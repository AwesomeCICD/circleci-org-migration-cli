package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/cciurl"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/spf13/cobra"
)

// SyncJSONSummary is the machine-readable result of a sync command when --json
// is set. Only names and counts are included — no secret values.
type SyncJSONSummary struct {
	// DryRun is true when --apply was not set (no changes were written).
	DryRun bool `json:"dry_run"`
	// DestOrgSlug is the destination organization slug.
	DestOrgSlug string `json:"dest_org_slug,omitempty"`
	// Sections holds per-resource-type sync results.
	Sections []SyncSectionSummary `json:"sections"`
	// Warnings lists items that need manual attention (status "manual" or
	// "error"), without any secret values.
	Warnings []syncWarning `json:"warnings,omitempty"`
}

// SyncSectionSummary holds the counts for one resource type (e.g. "Contexts").
type SyncSectionSummary struct {
	// Section is the resource type name (e.g. "Contexts", "Projects",
	// "Org Settings", "Runner Resource Classes").
	Section string `json:"section"`
	// Created is the number of resources created (or that would be created in
	// dry-run mode).
	Created int `json:"created"`
	// Exists is the number of resources that already existed and were reused.
	Exists int `json:"exists"`
	// Set is the number of resources that were updated or set.
	Set int `json:"set"`
	// Skipped is the number of resources that were skipped.
	Skipped int `json:"skipped"`
	// Manual is the number of resources flagged for manual recreation.
	Manual int `json:"manual"`
	// Error is the number of resources that encountered an error.
	Error int `json:"error"`
}

// syncWarning is a safe, secret-free item requiring manual attention.
type syncWarning struct {
	Section string `json:"section"`
	Status  string `json:"status"` // "manual" | "error"
	Target  string `json:"target"`
	Detail  string `json:"detail"`
}

// buildSyncSummary accumulates per-section reports into a SyncJSONSummary.
// repsBySection maps section name → its Report. apply mirrors the --apply flag.
func buildSyncSummary(apply bool, repsBySection map[string]*syncer.Report) SyncJSONSummary {
	summary := SyncJSONSummary{
		DryRun:   !apply,
		Sections: make([]SyncSectionSummary, 0, len(repsBySection)),
	}

	// Use the dest slug from the first non-empty report.
	for _, rep := range repsBySection {
		if rep != nil && rep.DestOrgSlug != "" {
			summary.DestOrgSlug = rep.DestOrgSlug
			break
		}
	}

	// Stable section order matches the sync execution order.
	sectionOrder := []string{"Org Settings", "Contexts", "Projects", "Runner Resource Classes"}
	for _, section := range sectionOrder {
		rep, ok := repsBySection[section]
		if !ok || rep == nil {
			continue
		}
		counts := rep.Counts()
		sec := SyncSectionSummary{
			Section: section,
			Created: counts["created"],
			Exists:  counts["exists"],
			Set:     counts["set"],
			Skipped: counts["skipped"],
			Manual:  counts["manual"],
			Error:   counts["error"],
		}
		summary.Sections = append(summary.Sections, sec)

		for _, a := range rep.Actions {
			if a.Status == "manual" || a.Status == "error" {
				summary.Warnings = append(summary.Warnings, syncWarning{
					Section: section,
					Status:  a.Status,
					Target:  a.Target,
					Detail:  a.Detail,
				})
			}
		}
	}
	return summary
}

// stripProjectExtras clears the "safety net" project metadata (checkout keys,
// additional SSH keys, webhooks, and schedules) from every project in the
// manifest. It is used by sync's --skip-extras flag to mirror the export and
// migrate --skip-extras behaviour at sync time: with these slices empty the
// syncer's webhook/schedule/SSH-key steps no-op.
func stripProjectExtras(m *manifest.Manifest) {
	for i := range m.Projects {
		m.Projects[i].CheckoutKeys = nil
		m.Projects[i].SSHKeys = nil
		m.Projects[i].Webhooks = nil
		m.Projects[i].Schedules = nil
	}
}

func newSyncCommand() *cobra.Command {
	var (
		manifestPath        string
		secretsPath         string
		mappingPath         string
		apply               bool
		yes                 bool
		missing             string
		skipContexts        bool
		skipProjects        bool
		skipOrgSettings     bool
		skipRunner          bool
		skipCIAM            bool
		skipExtras          bool
		githubToken         string
		destGitHubOrg       string
		destRunnerNamespace string
		jsonOutput          bool
		createProjectTokens bool
	)

	cmd := &cobra.Command{
		Use:   "sync --manifest <file> [--secrets <file>] [--apply]",
		Short: "Apply a manifest to the destination org (contexts, projects, and org settings).",
		Long: `sync recreates exported data in the destination CircleCI organization.

It reads the manifest (structure + variable names), an optional secret bundle
(the plaintext values captured by the in-pipeline 'secrets' step), and an
optional mapping file (source->destination org/project mapping; defaults to the
same names). It is idempotent: existing resources are reused by name where
possible.

The destination org defaults to the SOURCE org from the manifest. To target a
DIFFERENT org you MUST pass --mapping with org.to set — otherwise sync runs
against your own source org (a prominent warning is printed in that case).

--mapping file schema (JSON):
  {
    "org": { "from": "gh/acme", "to": "gh/acme-new" },
    "projects": { "gh/acme/web": "gh/acme-new/web" },
    "github_org": { "from": "acme", "to": "acme-new" }
  }
Only "org.to" is required to retarget the destination org. "projects" remaps
individual project slugs (needed for GitHub App destinations whose slug is
"circleci/<org-id>/<project-id>"); "github_org" rewrites repo owners when repos
moved to a new GitHub org.

Secrets: env-var VALUES come from the captured secret bundle (--secrets). With
--apply but NO bundle, contexts/projects are created with EMPTY env-var values
that you must fill in manually — run 'circleci-migrate secrets capture' first to
capture the plaintext values, then pass --secrets <bundle>.

Resources synced (in order):
  • Org settings — feature flags, OIDC claims, URL-orb allow list, config
    policies, OTel exporter, contacts, storage retention, budgets, release
    tracker, and block-unregistered-users.
  • Contexts — with environment variable values from the secret bundle.
  • Projects — OAuth projects are recreated in a paused state; App projects
    are created with a pipeline definition and trigger.
  • Self-hosted runner resource classes — only when --dest-runner-namespace
    is provided or the manifest contains runner classes.

By default sync performs a DRY RUN and writes nothing — review the plan, then
re-run with --apply. Group and project-type context restrictions are flagged
for manual recreation (group writes are not GA; project restriction values are
source-org IDs).

When OAuth projects are missing in the destination, --apply creates them in a
paused state (no webhook, no builds). After creation you are prompted to enable
builds (follow the project, which installs the webhook and may trigger an
initial build). --yes / -y only matters together with --apply: it auto-confirms
enabling builds without the interactive prompt (it has no effect in a dry run).
Without a TTY, builds are not enabled unless --yes is passed.

When the manifest contains self-hosted runner resource classes, pass
--dest-runner-namespace to recreate them in the destination namespace. If the
flag is omitted, runner classes are flagged for manual recreation — the syncer
never guesses the destination namespace.

Examples:
  circleci-migrate sync --manifest manifest.json --secrets secrets.json
  circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --secrets secrets.json --apply --yes
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --dest-token $DST_TOKEN --apply
  circleci-migrate sync --manifest manifest.json --dest-runner-namespace acme-new --apply`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			// Resolve the GitHub token from the env after parsing so the flag
			// default never leaks $GITHUB_TOKEN into --help output.
			if githubToken == "" {
				githubToken = os.Getenv("GITHUB_TOKEN")
			}
			if manifestPath == "" {
				return fmt.Errorf("--manifest is required")
			}
			if missing != syncer.MissingSkip && missing != syncer.MissingPlaceholder {
				return fmt.Errorf("--missing-secrets must be %q or %q", syncer.MissingSkip, syncer.MissingPlaceholder)
			}
			token := rootOptions.DestTokenOrDefault()
			if token == "" {
				return fmt.Errorf("no destination API token: set --dest-token, --token, CIRCLECI_DEST_TOKEN, or CIRCLECI_CLI_TOKEN")
			}

			m, err := manifest.Load(manifestPath)
			if err != nil {
				return err
			}
			// --skip-extras mirrors export/migrate: drop the "safety net" project
			// metadata (checkout keys, additional SSH keys, webhooks, schedules)
			// so the syncer never re-creates it. Stripping it from the in-memory
			// manifest is sufficient because the syncer's webhook/schedule/SSH-key
			// steps no-op on empty slices.
			if skipExtras {
				stripProjectExtras(m)
			}
			secretsExplicit := cmd.Flags().Changed("secrets")
			bundle, err := loadBundleWithFeedback(secretsPath, !secretsExplicit, cmd.ErrOrStderr())
			if err != nil {
				return err
			}
			var mapping *manifest.Mapping
			if mappingPath != "" {
				if mapping, err = manifest.LoadMapping(mappingPath); err != nil {
					return err
				}
			}

			// Resolve the destination org up front so it is shown clearly at the
			// START of the run (not only mid-output), and warn loudly when it is
			// the SAME org as the source (the default when no --mapping is given).
			sourceSlug := m.Source.Org.Slug
			destSlug := sourceSlug
			if mapping != nil && mapping.Org.To != "" {
				destSlug = mapping.Org.To
			}
			errW := cmd.ErrOrStderr()
			runMode := "DRY RUN"
			if apply {
				runMode = "APPLY"
			}
			fmt.Fprintf(errW, "Destination org: %s (%s)\n", destSlug, runMode)
			sameOrg := slugsEqual(destSlug, sourceSlug) ||
				(m.Source.Org.ID != "" && slugsEqual(destSlug, m.Source.Org.ID))
			if sameOrg {
				fmt.Fprintf(errW,
					"⚠ Destination is the SAME org as the source (%s). "+
						"Pass --mapping with org.to=<dest-slug> to retarget.\n",
					destSlug)
				// Under --apply with a TTY, require explicit confirmation before
				// writing to the source org. Without a TTY, warn loudly only.
				if apply {
					fi, _ := os.Stdin.Stat()
					isTTY := fi != nil && fi.Mode()&os.ModeCharDevice != 0
					if isTTY && !yes {
						fmt.Fprintf(errW, "Apply changes to the SOURCE org %s anyway? [y/N]: ", destSlug)
						scanner := bufio.NewScanner(os.Stdin)
						confirmed := false
						if scanner.Scan() {
							ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
							confirmed = ans == "y" || ans == "yes"
						}
						if !confirmed {
							fmt.Fprintf(errW, "Aborted (destination equals source).\n")
							return nil
						}
					}
				}
			}

			orgClient, err := org.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating org client: %w", err)
			}
			ctxClient, err := cctx.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating context client: %w", err)
			}
			projClient, err := project.NewClient(rootOptions, token)
			if err != nil {
				return fmt.Errorf("creating project client: %w", err)
			}

			sy := &syncer.Syncer{
				Org:         orgClient,
				Contexts:    ctxClient,
				Projects:    projClient,
				OrgSettings: orgSettingsAdapter{orgClient},
				Groups:      orgGroupLister{orgClient},
				CIAM:        ciamWriterAdapter{orgClient},
				Out:         cmd.ErrOrStderr(),
			}
			opts := syncer.Options{
				Apply:               apply,
				MissingSecrets:      missing,
				GitHubToken:         githubToken,
				DestGitHubOrg:       destGitHubOrg,
				DestRunnerNamespace: destRunnerNamespace,
				CreateProjectTokens: createProjectTokens,
			}

			// Wire up the runner client when a destination namespace was provided
			// or the manifest has runner classes (so dry-run preview works).
			// Skip when --skip-runner is set.
			if !skipRunner && (destRunnerNamespace != "" || len(m.RunnerResourceClasses) > 0) {
				runnerClient, rerr := runner.NewClient(rootOptions, token)
				if rerr != nil {
					return fmt.Errorf("creating runner client: %w", rerr)
				}
				sy.Runner = runnerClient
			}

			// Accumulate section reports for --json output.
			repsBySection := make(map[string]*syncer.Report)

			if !skipOrgSettings {
				rep, err := sy.SyncOrgSettings(ctx, m, mapping, opts)
				if err != nil {
					return err
				}
				repsBySection["Org Settings"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Org Settings", rep, m)
				}
			}
			if !skipContexts {
				rep, err := sy.SyncContexts(ctx, m, bundle, mapping, opts)
				if err != nil {
					return err
				}
				repsBySection["Contexts"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Contexts", rep, m)
				}
			}
			if !skipProjects {
				rep, err := sy.SyncProjects(ctx, m, bundle, mapping, opts)
				if err != nil {
					return err
				}
				repsBySection["Projects"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Projects", rep, m)
				}

				// Handle the enable-builds (follow) step for paused projects.
				if err := handleEnableBuilds(cmd, sy, rep, apply, yes, jsonOutput); err != nil {
					return err
				}
			}

			// Runner resource classes (attempted when present in manifest, unless skipped).
			if !skipRunner && (len(m.RunnerResourceClasses) > 0 || destRunnerNamespace != "") {
				rep, err := sy.SyncRunnerResourceClasses(ctx, m, opts)
				if err != nil {
					return err
				}
				repsBySection["Runner Resource Classes"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "Runner Resource Classes", rep, m)
				}
			}

			// CIAM roles and groups (standalone circleci-type orgs only; SyncCIAM
			// self-gates on the destination org type and on the manifest having
			// CIAM data, so it is safe to always attempt unless --skip-ciam).
			if !skipCIAM && m.CIAM != nil {
				rep, err := sy.SyncCIAM(ctx, m, mapping, opts)
				if err != nil {
					return err
				}
				repsBySection["CIAM"] = rep
				if !jsonOutput {
					printSyncReport(cmd, "CIAM", rep, m)
				}
			}

			// Emit a warning when no bundle was loaded but the manifest contains
			// context or project env vars that needed values.
			if bundle == nil {
				totalEnvVars := 0
				for _, mc := range m.Contexts {
					totalEnvVars += len(mc.EnvVars)
				}
				for _, proj := range m.Projects {
					totalEnvVars += len(proj.EnvVars)
				}
				if totalEnvVars > 0 {
					fmt.Fprintf(cmd.ErrOrStderr(),
						"WARNING: no secrets bundle loaded; %d env var value(s) were not synced and require manual entry\n",
						totalEnvVars)
				}
			}

			if jsonOutput {
				summary := buildSyncSummary(apply, repsBySection)
				return marshalJSON(cmd.OutOrStdout(), summary)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&secretsPath, "secrets", "secrets.json", "Path to the captured secret bundle holding plaintext env-var values (optional). Without it, --apply creates resources with EMPTY env-var values; run 'secrets capture' first to populate them.")
	f.StringVar(&mappingPath, "mapping", "",
		`Path to a source->destination mapping file (JSON). REQUIRED to change the destination org name; without it sync targets the SOURCE org. Schema: `+
			`{ "org": {"from":"gh/acme","to":"gh/acme-new"}, "projects": {"gh/acme/web":"gh/acme-new/web"}, "github_org": {"from":"acme","to":"acme-new"} }. `+
			`Only org.to is required to retarget; projects/github_org are optional.`)
	f.BoolVar(&apply, "apply", false, "Write changes to the destination (default: dry run)")
	f.BoolVarP(&yes, "yes", "y", false, "Only with --apply: auto-confirm enabling builds after project creation (skip the interactive prompt). No effect in a dry run.")
	f.StringVar(&missing, "missing-secrets", syncer.MissingSkip,
		"How to handle variables with no captured value: 'skip' omits the variable entirely; "+
			"'placeholder' creates the variable with a placeholder value. Use 'placeholder' for "+
			"restricted contexts whose values cannot be captured, so the variable name exists and "+
			"can be filled in manually later.")
	f.BoolVar(&skipContexts, "skip-contexts", false, "Skip syncing contexts")
	f.BoolVar(&skipProjects, "skip-projects", false, "Skip syncing projects")
	f.BoolVar(&skipOrgSettings, "skip-org-settings", false, "Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)")
	f.BoolVar(&skipRunner, "skip-runner", false, "Skip syncing self-hosted runner resource classes")
	f.BoolVar(&skipCIAM, "skip-ciam", false, "Skip syncing CIAM roles and groups (standalone circleci-type orgs only)")
	f.BoolVar(&skipExtras, "skip-extras", false, "Skip syncing project checkout keys, additional SSH keys, webhooks, and schedules")
	f.BoolVar(&jsonOutput, "json", false,
		"Print a machine-readable JSON summary to stdout instead of the human-readable per-section reports")
	f.StringVar(&githubToken, "github-token", "",
		"GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org). When omitted, the captured external_id is reused (correct for same-org migrations).")
	f.StringVar(&destGitHubOrg, "dest-github-org", "",
		"Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.")
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). "+
			"Must be supplied explicitly — the syncer never guesses the destination namespace. "+
			"When omitted and the manifest contains runner classes, each is flagged for manual recreation.")
	f.BoolVar(&createProjectTokens, "create-project-tokens", false,
		"When set AND --apply, recreate each captured project API token on the destination project. "+
			"CAUTION: each recreated token mints a NEW one-time secret — every consumer of the old token "+
			"must be repointed to the new value. New plaintext values are printed to stderr once and cannot "+
			"be retrieved again. Default false: emit manual steps only.")

	return cmd
}

// handleEnableBuilds decides whether to follow (enable builds for) the
// paused projects collected in rep.PendingEnable, then executes that decision.
// Progress and result lines go to stderr so they never pollute the JSON stream.
// When jsonOutput is true all writes to stdout are suppressed; progress lines
// are also suppressed (the caller folds results into the JSON summary).
func handleEnableBuilds(cmd *cobra.Command, sy *syncer.Syncer, rep *syncer.Report, apply, yes, jsonOutput bool) error {
	pending := rep.PendingEnable
	if len(pending) == 0 {
		return nil
	}
	n := len(pending)

	if !apply {
		// Dry-run plan message goes to stderr (not data, not JSON).
		if !jsonOutput {
			fmt.Fprintf(cmd.ErrOrStderr(),
				"\n%d project(s) would be created paused; re-run with --apply, then confirm enabling builds (or pass --yes).\n", n)
		}
		return nil
	}

	// Detect TTY on stdin.
	fi, _ := os.Stdin.Stat()
	isTTY := fi != nil && fi.Mode()&os.ModeCharDevice != 0

	// confirm reads one line from stdin and returns true if it is "y" or "yes".
	confirm := func() bool {
		fmt.Fprintf(cmd.ErrOrStderr(), "Enable builds for %d project(s) now? [y/N]: ", n)
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			ans := strings.TrimSpace(strings.ToLower(scanner.Text()))
			return ans == "y" || ans == "yes"
		}
		return false
	}

	if !decideEnable(apply, yes, isTTY, confirm) {
		if !jsonOutput {
			if !isTTY && !yes {
				fmt.Fprintf(cmd.ErrOrStderr(),
					"\nSkipped enabling builds (no TTY). Re-run with --yes to follow the %d created project(s).\n", n)
			} else {
				fmt.Fprintf(cmd.ErrOrStderr(), "\nSkipped enabling builds.\n")
			}
		}
		return nil
	}

	if !jsonOutput {
		fmt.Fprintf(cmd.ErrOrStderr(), "\nEnabling builds for %d project(s)...\n", n)
	}
	for _, t := range pending {
		action, _ := sy.EnableBuilds(cmd.Context(), t, true)
		if !jsonOutput {
			fmt.Fprintf(cmd.ErrOrStderr(), "  [%s] %s — %s\n", action.Status, action.Target, action.Detail)
		}
	}
	return nil
}

// decideEnable returns true when builds should be enabled right now.
//
// Rules:
//   - dry run (!apply): never enable.
//   - apply + yes: always enable (non-interactive auto-confirm).
//   - apply + TTY + !yes: call confirm() and return its result.
//   - apply + no TTY + !yes: never enable (caller prints a how-to message).
func decideEnable(apply, yes, isTTY bool, confirm func() bool) bool {
	if !apply {
		return false
	}
	if yes {
		return true
	}
	if isTTY {
		return confirm()
	}
	return false
}

// orgSettingsAdapter wraps *org.Client and adapts it to syncer.OrgSettingsWriter.
// It translates syncer.StorageRetentionArgs → org.StorageRetentionControls and
// forwards all other methods directly to the underlying org client.
type orgSettingsAdapter struct {
	c *org.Client
}

func (a orgSettingsAdapter) UpdateFeatureFlags(ctx context.Context, vcsType, orgName string, flags map[string]bool) error {
	return a.c.UpdateFeatureFlags(ctx, vcsType, orgName, flags)
}
func (a orgSettingsAdapter) SetOIDCClaims(ctx context.Context, orgID string, audience []string, ttl string) error {
	return a.c.SetOIDCClaims(ctx, orgID, audience, ttl)
}
func (a orgSettingsAdapter) CreateURLOrbAllowEntry(ctx context.Context, slugOrID, name, prefix, auth string) error {
	return a.c.CreateURLOrbAllowEntry(ctx, slugOrID, name, prefix, auth)
}
func (a orgSettingsAdapter) PutPolicyBundle(ctx context.Context, ownerID string, policies map[string]string) error {
	return a.c.PutPolicyBundle(ctx, ownerID, policies)
}
func (a orgSettingsAdapter) SetPolicyEnforcement(ctx context.Context, ownerID string, enabled bool) error {
	return a.c.SetPolicyEnforcement(ctx, ownerID, enabled)
}
func (a orgSettingsAdapter) CreateOTelExporter(ctx context.Context, orgID, endpoint, protocol string, insecure bool, headers map[string]string) error {
	return a.c.CreateOTelExporter(ctx, orgID, endpoint, protocol, insecure, headers)
}

// GetURLOrbAllowList + GetOTelExporters satisfy the syncer's optional
// *Getter interfaces so re-running `sync --apply` is idempotent (skips
// already-present URL-orb entries / OTel exporters instead of duplicating).
func (a orgSettingsAdapter) GetURLOrbAllowList(ctx context.Context, slugOrID string) ([]syncer.URLOrbAllowEntry, error) {
	entries, err := a.c.GetURLOrbAllowList(ctx, slugOrID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.URLOrbAllowEntry, len(entries))
	for i, e := range entries {
		out[i] = syncer.URLOrbAllowEntry{ID: e.ID, Name: e.Name, Prefix: e.Prefix, Auth: e.Auth}
	}
	return out, nil
}

func (a orgSettingsAdapter) GetOTelExporters(ctx context.Context, orgID string) ([]syncer.OTelExporter, error) {
	exporters, err := a.c.GetOTelExporters(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.OTelExporter, len(exporters))
	for i, e := range exporters {
		out[i] = syncer.OTelExporter{ID: e.ID, Endpoint: e.Endpoint, Protocol: e.Protocol, Insecure: e.Insecure, Headers: e.Headers}
	}
	return out, nil
}

// Compile-time assertions that the adapter provides the optional idempotency
// getters the syncer type-asserts for.
var (
	_ syncer.URLOrbAllowListGetter = orgSettingsAdapter{}
	_ syncer.OTelExporterGetter    = orgSettingsAdapter{}
)

func (a orgSettingsAdapter) SetContacts(ctx context.Context, orgID string, primary, security []string) error {
	return a.c.SetContacts(ctx, orgID, primary, security)
}
func (a orgSettingsAdapter) SetStorageRetention(ctx context.Context, orgUUID string, controls syncer.StorageRetentionArgs) error {
	return a.c.SetStorageRetention(ctx, orgUUID, org.StorageRetentionControls{
		CacheDays:     controls.CacheDays,
		WorkspaceDays: controls.WorkspaceDays,
		ArtifactDays:  controls.ArtifactDays,
	})
}
func (a orgSettingsAdapter) SetBudget(ctx context.Context, orgUUID string, projectID *string, credits int) error {
	return a.c.SetBudget(ctx, orgUUID, projectID, credits)
}
func (a orgSettingsAdapter) SetBlockUnregisteredUsers(ctx context.Context, orgUUID string, enabled bool) error {
	return a.c.SetBlockUnregisteredUsers(ctx, orgUUID, enabled)
}
func (a orgSettingsAdapter) SetReleaseTrackerSettings(ctx context.Context, orgUUID string, ttl string) error {
	return a.c.SetReleaseTrackerSettings(ctx, orgUUID, org.ReleaseTrackerSettings{
		InconclusiveReleaseTTL: ttl,
	})
}

// orgGroupLister adapts the org client's ListGroups (returning []org.Group) to
// the syncer.GroupLister interface (returning []syncer.Group).
type orgGroupLister struct {
	c *org.Client
}

func (g orgGroupLister) ListGroups(ctx context.Context, orgID string) ([]syncer.Group, error) {
	groups, err := g.c.ListGroups(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.Group, len(groups))
	for i, gr := range groups {
		out[i] = syncer.Group{ID: gr.ID, Name: gr.Name}
	}
	return out, nil
}

// ciamWriterAdapter wraps *org.Client and adapts it to syncer.CIAMWriter.
// Two methods need shape conversion (ListOrgRoleGrants returns []org.OrgRoleGrant
// → []syncer.CIAMRoleGrant; CreateGroup returns *org.Group → its ID); the rest
// forward directly. Wiring this enables CIAM apply (#176); SyncCIAM self-gates on
// the destination being a circleci-type org and on the manifest having CIAM data.
type ciamWriterAdapter struct {
	c *org.Client
}

func (a ciamWriterAdapter) ListOrgRoleGrants(ctx context.Context, orgID string) ([]syncer.CIAMRoleGrant, error) {
	grants, err := a.c.ListOrgRoleGrants(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.CIAMRoleGrant, len(grants))
	for i, g := range grants {
		out[i] = syncer.CIAMRoleGrant{UserID: g.UserID, Email: g.Email, Username: g.Username}
	}
	return out, nil
}
func (a ciamWriterAdapter) SetOrgUserRole(ctx context.Context, orgID, userID, role string) error {
	return a.c.SetOrgUserRole(ctx, orgID, userID, role)
}
func (a ciamWriterAdapter) ListGroups(ctx context.Context, orgID string) ([]syncer.CIAMGroupInfo, error) {
	groups, err := a.c.ListGroups(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.CIAMGroupInfo, len(groups))
	for i, g := range groups {
		out[i] = syncer.CIAMGroupInfo{ID: g.ID, Name: g.Name}
	}
	return out, nil
}
func (a ciamWriterAdapter) CreateGroup(ctx context.Context, orgID, name, description string) (string, error) {
	g, err := a.c.CreateGroup(ctx, orgID, name, description)
	if err != nil {
		return "", err
	}
	return g.ID, nil
}
func (a ciamWriterAdapter) AddUsersToGroup(ctx context.Context, orgID, groupID string, userIDs []string) error {
	return a.c.AddUsersToGroup(ctx, orgID, groupID, userIDs)
}
func (a ciamWriterAdapter) SetProjectUserRole(ctx context.Context, orgID, projectID, userID, role string) error {
	return a.c.SetProjectUserRole(ctx, orgID, projectID, userID, role)
}
func (a ciamWriterAdapter) AddProjectGroupRole(ctx context.Context, orgID, projectID string, groupIDs []string, role string) error {
	return a.c.AddProjectGroupRole(ctx, orgID, projectID, groupIDs, role)
}

// Compile-time assertion that the adapter satisfies the syncer interface.
var _ syncer.CIAMWriter = ciamWriterAdapter{}

// loadBundleIfPresent loads the secret bundle at path if it exists; a missing
// file is not an error (sync then reports all values as needing manual entry).
func loadBundleIfPresent(path string) (*manifest.SecretBundle, error) {
	if path == "" {
		return nil, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return manifest.LoadSecretBundle(path)
}

// loadBundleWithFeedback is like loadBundleIfPresent but also prints a status
// line to stderr so the operator knows whether the bundle was loaded or absent.
//
//   - Present:  "Loaded secrets bundle from <path> (N values)."
//   - Absent (default path "secrets.json"):
//     "Note: secrets.json not found — env-var values will be missing unless captured."
//   - Absent (explicit non-default path): FATAL error "secrets bundle not found: <path>".
//
// isDefault should be true when path came from the flag default (i.e. the user
// did not explicitly supply --secrets).
func loadBundleWithFeedback(path string, isDefault bool, errW io.Writer) (*manifest.SecretBundle, error) {
	bndl, err := loadBundleIfPresent(path)
	if err != nil {
		return nil, err
	}
	if bndl == nil {
		// Bundle absent.
		if isDefault || path == "" {
			// Default path missing (or user explicitly opted out with "")
			// — informational note only.
			if isDefault && path != "" {
				fmt.Fprintf(errW, "Note: %s not found — env-var values will be missing unless captured.\n", path)
			}
			return nil, nil
		}
		// Explicit --secrets <path> that does not exist is a fatal configuration error.
		return nil, fmt.Errorf("secrets bundle not found: %s", path)
	}
	// Bundle loaded — count total secret values.
	n := 0
	for _, vals := range bndl.ContextSecrets {
		n += len(vals)
	}
	for _, vals := range bndl.ProjectSecrets {
		n += len(vals)
	}
	fmt.Fprintf(errW, "Loaded secrets bundle from %s (%d values).\n", path, n)
	return bndl, nil
}

func printSyncReport(cmd *cobra.Command, section string, rep *syncer.Report, m *manifest.Manifest) {
	out := cmd.OutOrStdout()
	mode := "DRY RUN (no changes written; re-run with --apply to apply)"
	if rep.Applied {
		mode = "APPLIED"
	}
	fmt.Fprintf(out, "== %s sync — %s ==\n", section, mode)
	fmt.Fprintf(out, "  Destination: %s\n\n", rep.DestOrgSlug)

	counts := rep.Counts()
	for _, status := range []string{"created", "exists", "set", "manual", "skipped", "error"} {
		if n := counts[status]; n > 0 {
			fmt.Fprintf(out, "  %-8s %d\n", status+":", n)
		}
	}

	// Surface the items needing attention (manual + error) explicitly.
	var attention []syncer.Action
	for _, a := range rep.Actions {
		if a.Status == "manual" || a.Status == "error" {
			attention = append(attention, a)
		}
	}
	if len(attention) > 0 {
		fmt.Fprintf(out, "\n  Needs attention:\n")
		sort.Slice(attention, func(i, j int) bool { return attention[i].Target < attention[j].Target })
		for _, a := range attention {
			line := syncActionLine(a, rep.DestOrgSlug, m)
			fmt.Fprintf(out, "    [%s] %s — %s\n", a.Status, line, a.Detail)
		}
	}
}

// syncActionLine enriches an action's Target with a friendly project/context
// name (from the manifest) and a destination settings URL when constructible.
// The returned string replaces the raw target slug in the "Needs attention"
// output so operators can act without hunting for UUIDs.
//
// The destination org slug (rep.DestOrgSlug) is used for URL construction so
// the link points at the correct destination org, not the source.
func syncActionLine(a syncer.Action, destOrgSlug string, m *manifest.Manifest) string {
	target := a.Target

	// Derive a dest host: use the manifest source host (URL scheme is the same
	// for cloud, different for server). For circleci.com sources this resolves to
	// app.circleci.com; for server installs it uses the server host.
	destHost := m.Source.Host

	// Compute the destination project slug from the target by replacing the
	// project-UUID portion with the destination org slug where possible.
	// Target formats we recognise (set by the syncer):
	//   "circleci/<srcOrgUUID>/<srcProjUUID>/..."  — standalone/App projects
	//   "gh/<org>/<repo>/..."                       — OAuth projects
	//   "<name>"                                    — App projects during dry-run / App creation path

	// Build helper lookups once.
	projBySourceSlug := buildProjBySourceSlug(m)
	projByName := buildProjByName(m)
	ctxByName := buildCtxByName(m)

	// Identify what this action's target refers to.
	friendlyName, settingsURL := resolveTargetMeta(target, destOrgSlug, destHost, projBySourceSlug, projByName, ctxByName)

	if friendlyName == "" && settingsURL == "" {
		return target
	}
	if friendlyName != "" && settingsURL != "" {
		return fmt.Sprintf("%s (%s) → %s", target, friendlyName, settingsURL)
	}
	if friendlyName != "" {
		return fmt.Sprintf("%s (%s)", target, friendlyName)
	}
	return fmt.Sprintf("%s → %s", target, settingsURL)
}

// resolveTargetMeta extracts the friendly name and destination settings URL for
// an action target. Returns ("", "") when neither can be determined.
func resolveTargetMeta(
	target, destOrgSlug, destHost string,
	projBySourceSlug map[string]manifest.Project,
	projByName map[string]manifest.Project,
	ctxByName map[string]manifest.Context,
) (friendlyName, settingsURL string) {
	// --- project-scoped targets ---
	// Patterns: "circleci/<orgUUID>/<projUUID>/<kind>:<detail>"
	//           "gh/<org>/<repo>/<kind>:<detail>"
	//           "<projectName>/<kind>:<detail>"    (App dry-run path)
	//
	// First try to extract a project slug (3-part prefix).
	if projSlug, rest := splitProjectSlug(target); projSlug != "" {
		// Look up the source project by slug.
		p, ok := projBySourceSlug[projSlug]
		if !ok {
			// Not a known source slug — may be a dest slug already; try name lookup
			// via the last path component.
			name := slugLastComponent(projSlug)
			if pp, found := projByName[name]; found {
				p = pp
				ok = true
			}
		}
		if ok {
			friendlyName = syncProjectDisplayName(p)
			tab := tabFromKind(rest)
			// Build destination project slug by replacing the source org part.
			destProjSlug := rebaseProjectSlug(projSlug, destOrgSlug)
			settingsURL = cciurl.ProjectSettingsURL(destHost, destProjSlug, tab)
			return
		}
	}

	// --- context-scoped targets ---
	// Patterns: "<contextName>/<kind>:<detail>"  or  "<contextName>"
	if ctxName, _ := splitContextTarget(target); ctxName != "" {
		if _, ok := ctxByName[ctxName]; ok {
			friendlyName = ctxName
			settingsURL = cciurl.OrgSettingsURL(destHost, destOrgSlug, "contexts")
			return
		}
	}

	// No match — return zero values.
	return "", ""
}

// splitProjectSlug attempts to parse the leading three-component project slug
// from an action target ("vcs/org/repo" or "circleci/uuid/uuid"). It returns
// the slug and the remainder of the target string (everything after the third
// component), or ("", target) when the target does not start with a
// recognisable project slug.
func splitProjectSlug(target string) (slug, rest string) {
	parts := strings.SplitN(target, "/", 4)
	if len(parts) < 3 {
		return "", target
	}
	// Require a known VCS prefix or "circleci" as the first component.
	prefix := strings.ToLower(parts[0])
	switch prefix {
	case "gh", "bb", "github", "bitbucket", "circleci":
		slug = strings.Join(parts[:3], "/")
		if len(parts) == 4 {
			rest = parts[3]
		}
		return slug, rest
	}
	return "", target
}

// splitContextTarget attempts to extract a context name from an action target.
// Context targets have the form "<name>" or "<name>/<rest>". Returns the
// context name and remainder, or ("", target) when the target contains a
// recognised VCS/project prefix (which is a project target, not a context).
func splitContextTarget(target string) (ctxName, rest string) {
	parts := strings.SplitN(target, "/", 2)
	if len(parts) == 0 {
		return "", target
	}
	prefix := strings.ToLower(parts[0])
	switch prefix {
	case "gh", "bb", "github", "bitbucket", "circleci",
		"sso", "feature_flag", "oidc", "url_orb", "policy",
		"audit_log", "otel", "contacts", "storage_retention",
		"budget", "block_unregistered", "release_tracker",
		"orb", "environment_hierarchy":
		return "", target
	}
	ctxName = parts[0]
	if len(parts) > 1 {
		rest = parts[1]
	}
	return ctxName, rest
}

// tabFromKind maps an action-target "kind:detail" fragment to a settings tab
// name suitable for ProjectSettingsURL.
func tabFromKind(rest string) string {
	if rest == "" {
		return ""
	}
	// rest is typically "kind:detail" e.g. "ssh-key:aa:bb:cc"
	kind := rest
	if idx := strings.Index(rest, ":"); idx >= 0 {
		kind = rest[:idx]
	}
	switch kind {
	case "ssh-key":
		return "ssh"
	case "webhook":
		return "webhooks"
	case "env-var", "project-var":
		return "env-vars"
	case "feature_flag":
		return "advanced"
	case "oidc_claims":
		return ""
	case "schedule":
		return ""
	default:
		return ""
	}
}

// rebaseProjectSlug replaces the source-org portion of a project slug with the
// destination org slug. For example:
//
//	"gh/acme/web" with destOrgSlug "gh/acme-new" → "gh/acme-new/web"
//	"circleci/src-uuid/proj-uuid" with "circleci/dst-uuid" → "circleci/dst-uuid/proj-uuid"
//
// If destOrgSlug is empty or the slug is malformed, the original slug is returned.
func rebaseProjectSlug(projSlug, destOrgSlug string) string {
	if destOrgSlug == "" {
		return projSlug
	}
	parts := strings.SplitN(projSlug, "/", 3)
	if len(parts) != 3 {
		return projSlug
	}
	destParts := strings.SplitN(destOrgSlug, "/", 2)
	if len(destParts) == 2 {
		return destParts[0] + "/" + destParts[1] + "/" + parts[2]
	}
	return projSlug
}

// slugLastComponent returns the last "/" component of a slug.
func slugLastComponent(slug string) string {
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		return slug[idx+1:]
	}
	return slug
}

// buildProjBySourceSlug builds a map from source project slug to manifest.Project.
func buildProjBySourceSlug(m *manifest.Manifest) map[string]manifest.Project {
	out := make(map[string]manifest.Project, len(m.Projects))
	for _, p := range m.Projects {
		out[p.Slug] = p
	}
	return out
}

// buildProjByName builds a map from project name to manifest.Project.
func buildProjByName(m *manifest.Manifest) map[string]manifest.Project {
	out := make(map[string]manifest.Project, len(m.Projects))
	for _, p := range m.Projects {
		if p.Name != "" {
			out[p.Name] = p
		}
	}
	return out
}

// buildCtxByName builds a map from context name to manifest.Context.
func buildCtxByName(m *manifest.Manifest) map[string]manifest.Context {
	out := make(map[string]manifest.Context, len(m.Contexts))
	for _, c := range m.Contexts {
		out[c.Name] = c
	}
	return out
}

// slugsEqual reports whether two org slugs/ids refer to the same org, ignoring
// case and surrounding whitespace. Empty strings never match.
func slugsEqual(a, b string) bool {
	a = strings.TrimSpace(strings.ToLower(a))
	b = strings.TrimSpace(strings.ToLower(b))
	return a != "" && a == b
}

// syncProjectDisplayName returns the human-readable name for a project in sync
// output. It prefers the Name field; falls back to the slug's last component.
func syncProjectDisplayName(p manifest.Project) string {
	if p.Name != "" {
		return p.Name
	}
	return slugLastComponent(p.Slug)
}
