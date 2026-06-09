package cmd

import (
	"fmt"
	"os"
	"sort"

	cctx "github.com/CircleCI-Public/circleci-org-migration-cli/api/context"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/api/project"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/syncer"
	"github.com/spf13/cobra"
)

func newSyncCommand() *cobra.Command {
	var (
		manifestPath string
		secretsPath  string
		mappingPath  string
		apply        bool
		missing      string
		skipContexts bool
		skipProjects bool
	)

	cmd := &cobra.Command{
		Use:   "sync --manifest <file> [--secrets <file>] [--apply]",
		Short: "Apply a manifest to the destination org (contexts).",
		Long: `sync recreates exported data in the destination CircleCI organization.

It reads the manifest (structure + variable names), an optional secret bundle
(the plaintext values captured by the in-pipeline 'secrets' step), and an
optional mapping file (source->destination org/project mapping; defaults to the
same names). It is idempotent: existing contexts are reused by name.

By default sync performs a DRY RUN and writes nothing — review the plan, then
re-run with --apply. Group and project-type context restrictions are flagged
for manual recreation (group writes are not GA; project restriction values are
source-org IDs).

This milestone syncs contexts (env-var values + expression restrictions).
Project settings/variables and project creation are handled separately.

Examples:
  circleci-migrate sync --manifest manifest.json --secrets secrets.json
  circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			bundle, err := loadBundleIfPresent(secretsPath)
			if err != nil {
				return err
			}
			var mapping *manifest.Mapping
			if mappingPath != "" {
				if mapping, err = manifest.LoadMapping(mappingPath); err != nil {
					return err
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

			sy := &syncer.Syncer{Org: orgClient, Contexts: ctxClient, Projects: projClient, Out: cmd.ErrOrStderr()}
			opts := syncer.Options{Apply: apply, MissingSecrets: missing}

			if !skipContexts {
				rep, err := sy.SyncContexts(m, bundle, mapping, opts)
				if err != nil {
					return err
				}
				printSyncReport(cmd, "Contexts", rep)
			}
			if !skipProjects {
				rep, err := sy.SyncProjects(m, bundle, mapping, opts)
				if err != nil {
					return err
				}
				printSyncReport(cmd, "Projects", rep)
			}
			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&secretsPath, "secrets", "secrets.json", "Path to the captured secret bundle (optional)")
	f.StringVar(&mappingPath, "mapping", "", "Path to a source->destination mapping file (optional)")
	f.BoolVar(&apply, "apply", false, "Write changes to the destination (default: dry run)")
	f.StringVar(&missing, "missing-secrets", syncer.MissingSkip, "How to handle variables with no captured value: skip|placeholder")
	f.BoolVar(&skipContexts, "skip-contexts", false, "Skip syncing contexts")
	f.BoolVar(&skipProjects, "skip-projects", false, "Skip syncing projects")

	return cmd
}

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

func printSyncReport(cmd *cobra.Command, section string, rep *syncer.Report) {
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
			fmt.Fprintf(out, "    [%s] %s — %s\n", a.Status, a.Target, a.Detail)
		}
	}
}
