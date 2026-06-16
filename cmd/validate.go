package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	apiOrb "github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
	"github.com/spf13/cobra"
)

// ValidateJSONOutput is the machine-readable result of a validate command when
// --json is set. Only names and statuses are included — no secret values.
type ValidateJSONOutput struct {
	// SourceOrg is the slug of the source organization.
	SourceOrg string `json:"source_org"`
	// DestOrg is the slug of the destination organization.
	DestOrg string `json:"dest_org"`
	// HasGaps is true when any item is missing on the destination (exit code > 0).
	HasGaps bool `json:"has_gaps"`
	// Sections contains the per-section comparison results.
	Sections []ValidateJSONSection `json:"sections"`
	// Totals is the overall counts.
	Totals ValidateJSONTotals `json:"totals"`
}

// ValidateJSONSection is the per-section data for --json output.
type ValidateJSONSection struct {
	Name       string             `json:"name"`
	Skipped    bool               `json:"skipped,omitempty"`
	SkipReason string             `json:"skip_reason,omitempty"`
	Items      []ValidateJSONItem `json:"items,omitempty"`
	Counts     ValidateJSONCounts `json:"counts"`
}

// ValidateJSONItem is one comparison result item for --json output.
type ValidateJSONItem struct {
	Status string `json:"status"` // "matched" | "missing" | "manual"
	Name   string `json:"name"`
	Detail string `json:"detail"`
}

// ValidateJSONCounts holds the item counts for one section.
type ValidateJSONCounts struct {
	Matched int `json:"matched"`
	Missing int `json:"missing"`
	Manual  int `json:"manual"`
}

// ValidateJSONTotals holds overall counts across all sections.
type ValidateJSONTotals struct {
	Matched int `json:"matched"`
	Missing int `json:"missing"`
	Manual  int `json:"manual"`
}

// buildValidateJSONOutput converts a validate.Result to the JSON output shape.
func buildValidateJSONOutput(r validate.Result) ValidateJSONOutput {
	out := ValidateJSONOutput{
		SourceOrg: r.SourceOrg,
		DestOrg:   r.DestOrg,
		HasGaps:   r.HasMissing(),
	}
	for _, s := range r.Sections {
		matched, missing, manual := s.Counts()
		js := ValidateJSONSection{
			Name:       s.Name,
			Skipped:    s.Skipped,
			SkipReason: s.SkipReason,
			Counts:     ValidateJSONCounts{Matched: matched, Missing: missing, Manual: manual},
		}
		for _, it := range s.Items {
			js.Items = append(js.Items, ValidateJSONItem{
				Status: it.Status,
				Name:   it.Name,
				Detail: it.Detail,
			})
		}
		out.Sections = append(out.Sections, js)
		out.Totals.Matched += matched
		out.Totals.Missing += missing
		out.Totals.Manual += manual
	}
	return out
}

// printValidateReport writes a human-readable parity report to the given builder.
// It groups items by section, aligns columns, and ends with a summary of
// actionable items and an overall verdict.
func printValidateReport(r validate.Result, out *strings.Builder) {
	fmt.Fprintf(out, "CircleCI migration parity report\n")
	fmt.Fprintf(out, "  Source : %s\n", r.SourceOrg)
	fmt.Fprintf(out, "  Dest   : %s\n", r.DestOrg)
	fmt.Fprintf(out, "\n")

	// Per-section summary.
	for _, s := range r.Sections {
		if s.Skipped {
			fmt.Fprintf(out, "── %s\n", s.Name)
			fmt.Fprintf(out, "   ⊘  skipped — %s\n\n", s.SkipReason)
			continue
		}
		matched, missing, manual := s.Counts()
		fmt.Fprintf(out, "── %s\n", s.Name)
		if len(s.Items) == 0 {
			fmt.Fprintf(out, "   (nothing to compare)\n\n")
			continue
		}
		fmt.Fprintf(out, "   ✓ %d matched   ✗ %d missing   ⚠ %d manual\n", matched, missing, manual)
		// Show matched items briefly.
		for _, it := range s.Items {
			if it.Status == validate.StatusMatched {
				fmt.Fprintf(out, "   ✓  %-48s  %s\n", truncate(it.Name, 48), it.Detail)
			}
		}
		fmt.Fprintf(out, "\n")
	}

	// Actionable items: all missing + manual, sorted by section then name.
	type actionItem struct {
		section string
		item    validate.Item
	}
	var actions []actionItem
	for _, s := range r.Sections {
		for _, it := range s.Items {
			if it.Status == validate.StatusMissing || it.Status == validate.StatusManual {
				actions = append(actions, actionItem{section: s.Name, item: it})
			}
		}
	}

	if len(actions) > 0 {
		fmt.Fprintf(out, "════════════════════════════════════════════════════\n")
		fmt.Fprintf(out, "NEEDS ATTENTION\n")
		fmt.Fprintf(out, "════════════════════════════════════════════════════\n\n")

		// Group by section, sorted.
		sort.Slice(actions, func(i, j int) bool {
			if actions[i].section != actions[j].section {
				return actions[i].section < actions[j].section
			}
			return actions[i].item.Name < actions[j].item.Name
		})
		currentSection := ""
		for _, a := range actions {
			if a.section != currentSection {
				fmt.Fprintf(out, "%s:\n", a.section)
				currentSection = a.section
			}
			marker := "✗"
			if a.item.Status == validate.StatusManual {
				marker = "⚠"
			}
			fmt.Fprintf(out, "  %s  %s\n", marker, a.item.Detail)
		}
		fmt.Fprintf(out, "\n")
	}

	// Overall verdict.
	fmt.Fprintf(out, "════════════════════════════════════════════════════\n")
	fmt.Fprintf(out, "TOTALS: %s\n", r.TotalsLine())
	if r.HasMissing() {
		fmt.Fprintf(out, "VERDICT: ✗ GAPS FOUND — destination is missing items (exit code 1)\n")
		fmt.Fprintf(out, "         ⚠ Manual items above require operator action regardless of exit code.\n")
	} else {
		_, _, manual := validateTotals(r)
		if manual > 0 {
			fmt.Fprintf(out, "VERDICT: ✓ No missing items  ⚠ %d manual item(s) require operator attention.\n", manual)
		} else {
			fmt.Fprintf(out, "VERDICT: ✓ Migration appears complete — no gaps detected.\n")
		}
	}
	fmt.Fprintf(out, "════════════════════════════════════════════════════\n")
}

// validateTotals returns the overall matched, missing, manual counts across all sections.
func validateTotals(r validate.Result) (matched, missing, manual int) {
	for _, s := range r.Sections {
		m, ms, mn := s.Counts()
		matched += m
		missing += ms
		manual += mn
	}
	return
}

// truncate shortens s to at most maxLen runes, appending "…" if truncated.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}

// validateCountMissing returns the total number of StatusMissing items across
// all sections of a validate.Result.
func validateCountMissing(r validate.Result) int {
	n := 0
	for _, s := range r.Sections {
		_, missing, _ := s.Counts()
		n += missing
	}
	return n
}

func newValidateCommand() *cobra.Command {
	var (
		sourceOrg           string
		destOrg             string
		mappingPath         string
		destRunnerNamespace string
		destOrbNamespace    string
		jsonOutput          bool
		noInput             bool
	)

	cmd := &cobra.Command{
		Use:   "validate --source-org <slug> --dest-org <slug> [flags]",
		Short: "Compare source and destination orgs and report migration parity.",
		Long: `validate exports BOTH the source and destination orgs (read-only), then
diffs them by name/structure and prints a per-section parity report.

It reports what matched, what is missing on the destination, and what needs
manual attention. Secret VALUES are never compared — they are masked by the
CircleCI API and intentionally absent from the comparison.

Sections checked:
  • Contexts        — each source context exists on destination; every env-var
                       NAME is present; restrictions are compared by type.
  • Projects        — each source project exists on destination (by mapped slug);
                       every env-var NAME is present; key advanced settings match;
                       additional SSH-key fingerprints and checkout keys are present.
  • Org Settings    — feature flags, OIDC claims, URL-orb allow list, config
                       policies, storage retention, release tracker, contacts,
                       OTel exporters. SSO is always reported as manual (DNS
                       verification + IdP setup required).
  • Runner Classes  — resource classes present in destination namespace. Requires
                       --dest-runner-namespace; skipped with a note when absent.
  • Orbs            — orbs and versions present in destination namespace. Requires
                       --dest-orb-namespace; skipped with a note when absent.
  • CIAM            — if source has CIAM data, a manual verification note is
                       emitted (role bindings must be confirmed by email).

EXIT CODE:
  0 — no missing items (✓ matched and ⚠ manual items only; manual items still
      require operator attention and are listed prominently).
  1 — one or more items are ✗ missing on the destination.

Manual items (⚠) indicate steps that require operator action but do not by
themselves cause a non-zero exit code. They are always listed in the
"NEEDS ATTENTION" block regardless of the exit code.

TOKEN SOURCES (same as migrate/sync):
  --source-token flag or CIRCLECI_SOURCE_TOKEN env var
  --dest-token flag or CIRCLECI_DEST_TOKEN env var
  --token flag or CIRCLECI_CLI_TOKEN env var (fallback for both)

MAPPING:
Pass --mapping to apply the same slug translations as 'sync'. Without a mapping,
source project slugs are compared against the destination using the org-level
slug derivation (gh/old/web ↔ gh/new/web when --source-org gh/old and
--dest-org gh/new).

Examples:
  # Compare two orgs non-interactively:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --source-token $SRC_TOKEN --dest-token $DST_TOKEN

  # With a mapping file (same mapping used for sync):
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --mapping mapping.json

  # Include runner and orb comparison:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --dest-runner-namespace acme-new \
    --dest-orb-namespace acme-new

  # Machine-readable JSON output:
  circleci-migrate validate \
    --source-org gh/acme --dest-org gh/acme-new \
    --json`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)

			// Validate required flags.
			if sourceOrg == "" {
				if !noInput && isInteractiveTTY() {
					return fmt.Errorf("--source-org is required (e.g. --source-org gh/acme)")
				}
				return fmt.Errorf(
					"--source-org is required (e.g. --source-org gh/acme); " +
						"in non-TTY mode pass both --source-org and --dest-org explicitly")
			}
			if destOrg == "" {
				if !noInput && isInteractiveTTY() {
					return fmt.Errorf("--dest-org is required (e.g. --dest-org gh/acme-new)")
				}
				return fmt.Errorf(
					"--dest-org is required (e.g. --dest-org gh/acme-new); " +
						"in non-TTY mode pass both --source-org and --dest-org explicitly")
			}

			srcToken := cfg.SourceTokenOrDefault()
			if srcToken == "" {
				return noSourceTokenError()
			}
			dstToken := cfg.DestTokenOrDefault()
			if dstToken == "" {
				return noDestTokenError()
			}

			errW := cmd.ErrOrStderr()

			// ── Step 1: export source org ────────────────────────────────────
			fmt.Fprintf(errW, "Exporting source org %s...\n", sourceOrg)
			srcOrgClient, err := org.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source org client: %w", err)
			}
			srcCtxClient, err := cctx.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source context client: %w", err)
			}
			srcProjClient, err := project.NewClient(cfg, srcToken)
			if err != nil {
				return fmt.Errorf("creating source project client: %w", err)
			}

			srcEx := &exporter.Exporter{
				Org:      srcOrgClient,
				Contexts: srcCtxClient,
				Projects: srcProjClient,
				Out:      errW,
			}

			// Wire source runner client when dest-runner-namespace is set.
			if destRunnerNamespace != "" {
				srcRunnerClient, rerr := runner.NewClient(cfg, srcToken)
				if rerr != nil {
					fmt.Fprintf(errW, "Warning: could not create source runner client: %v\n", rerr)
				} else {
					srcEx.Runner = srcRunnerClient
				}
			}

			// Wire source orb client when dest-orb-namespace is set.
			if destOrbNamespace != "" {
				srcOrbClient, oerr := apiOrb.NewClient(cfg, srcToken)
				if oerr != nil {
					fmt.Fprintf(errW, "Warning: could not create source orb client: %v\n", oerr)
				} else {
					srcEx.Orb = srcOrbClient
				}
			}

			// Derive best-effort source namespaces from the source org slug
			// (e.g. "acme" from "gh/acme") when dest namespaces are set.
			srcRunnerNS := validateSourceNS(sourceOrg, destRunnerNamespace)
			srcOrbNS := validateSourceNS(sourceOrg, destOrbNamespace)

			srcManifest, err := srcEx.Export(ctx, exporter.Options{
				Host:            cfg.Host,
				OrgSlug:         sourceOrg,
				IncludeContexts: true,
				IncludeProjects: true,
				IncludeExtras:   false, // validate does not need checkout-key blobs
				RunnerNamespace: srcRunnerNS,
				OrbNamespace:    srcOrbNS,
			})
			if err != nil {
				return fmt.Errorf("exporting source org %q: %w", sourceOrg, err)
			}
			srcManifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			fmt.Fprintf(errW, "Source export complete: %d context(s), %d project(s)\n",
				len(srcManifest.Contexts), len(srcManifest.Projects))

			// ── Step 2: export destination org ───────────────────────────────
			fmt.Fprintf(errW, "Exporting destination org %s...\n", destOrg)
			dstOrgClient, err := org.NewClient(cfg, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination org client: %w", err)
			}
			dstCtxClient, err := cctx.NewClient(cfg, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination context client: %w", err)
			}
			dstProjClient, err := project.NewClient(cfg, dstToken)
			if err != nil {
				return fmt.Errorf("creating destination project client: %w", err)
			}

			dstEx := &exporter.Exporter{
				Org:      dstOrgClient,
				Contexts: dstCtxClient,
				Projects: dstProjClient,
				Out:      errW,
			}

			if destRunnerNamespace != "" {
				dstRunnerClient, rerr := runner.NewClient(cfg, dstToken)
				if rerr != nil {
					fmt.Fprintf(errW, "Warning: could not create destination runner client: %v\n", rerr)
				} else {
					dstEx.Runner = dstRunnerClient
				}
			}
			if destOrbNamespace != "" {
				dstOrbClient, oerr := apiOrb.NewClient(cfg, dstToken)
				if oerr != nil {
					fmt.Fprintf(errW, "Warning: could not create destination orb client: %v\n", oerr)
				} else {
					dstEx.Orb = dstOrbClient
				}
			}

			dstManifest, err := dstEx.Export(ctx, exporter.Options{
				Host:            cfg.Host,
				OrgSlug:         destOrg,
				IncludeContexts: true,
				IncludeProjects: true,
				IncludeExtras:   false,
				RunnerNamespace: destRunnerNamespace,
				OrbNamespace:    destOrbNamespace,
			})
			if err != nil {
				return fmt.Errorf("exporting destination org %q: %w", destOrg, err)
			}
			dstManifest.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
			fmt.Fprintf(errW, "Destination export complete: %d context(s), %d project(s)\n",
				len(dstManifest.Contexts), len(dstManifest.Projects))

			// ── Step 3: load mapping ─────────────────────────────────────────
			var mapping *manifest.Mapping
			if mappingPath != "" {
				mapping, err = manifest.LoadMapping(mappingPath)
				if err != nil {
					return fmt.Errorf("loading mapping: %w", err)
				}
			} else {
				// Build a minimal org-level mapping from the org slugs so that
				// project slug derivation works correctly (gh/old/web ↔ gh/new/web).
				mapping = &manifest.Mapping{
					Org: manifest.OrgMapping{From: sourceOrg, To: destOrg},
				}
			}

			// ── Step 4: compare ──────────────────────────────────────────────
			result := validate.Compare(srcManifest, dstManifest, mapping, validate.Options{
				DestRunnerNamespace: destRunnerNamespace,
				DestOrbNamespace:    destOrbNamespace,
			})

			// ── Step 5: output ───────────────────────────────────────────────
			stdout := cmd.OutOrStdout()
			if jsonOutput {
				jout := buildValidateJSONOutput(result)
				if err := marshalJSON(stdout, jout); err != nil {
					return err
				}
			} else {
				var b strings.Builder
				printValidateReport(result, &b)
				fmt.Fprint(stdout, b.String())
			}

			// Non-zero exit when there are missing items.
			if result.HasMissing() {
				return fmt.Errorf("validate: %d missing item(s) on destination", validateCountMissing(result))
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&sourceOrg, "source-org", "",
		"CircleCI organization slug for the source org, e.g. gh/acme "+
			"(shown in CircleCI → Organization Settings → Overview). (required)")
	f.StringVar(&destOrg, "dest-org", "",
		"CircleCI organization slug for the destination org, e.g. gh/acme-new "+
			"(shown in CircleCI → Organization Settings → Overview). (required)")
	f.StringVar(&mappingPath, "mapping", "",
		"Path to a source→destination mapping file (JSON, optional). "+
			"Reuses the same format as 'sync --mapping' so you can use the same file for both commands.")
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace to compare runner resource classes against (e.g. 'acme-new'). "+
			"When omitted the runner section is skipped with an explanatory note.")
	f.StringVar(&destOrbNamespace, "dest-orb-namespace", "",
		"Destination orb namespace to compare orbs against (e.g. 'acme-new'). "+
			"When omitted the orb section is skipped with an explanatory note.")
	f.BoolVar(&jsonOutput, "json", false,
		"Print a machine-readable JSON result to stdout instead of the human-readable report. "+
			"The JSON contains sections → items → status for tooling consumption.")
	f.BoolVar(&noInput, "no-input", false,
		"Disable all interactive prompts; error immediately if a required flag is missing. "+
			"Implied when stdin is not a TTY (e.g. CI pipelines).")

	return cmd
}

// validateSourceNS derives a best-effort source namespace (runner or orb) from
// the source org slug when the operator has set the corresponding destination
// namespace flag. The derivation uses the org short name (e.g. "acme" from
// "gh/acme"), which is the conventional namespace name. Returns "" when no
// destination namespace is set (disabling capture for that section).
func validateSourceNS(sourceOrgSlug, destNamespace string) string {
	if destNamespace == "" {
		return ""
	}
	// Use the segment after the first "/" as the org short name.
	if idx := strings.Index(sourceOrgSlug, "/"); idx >= 0 {
		return sourceOrgSlug[idx+1:]
	}
	return ""
}
