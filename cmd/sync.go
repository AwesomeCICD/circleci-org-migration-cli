package cmd

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/runner"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
	"github.com/spf13/cobra"
)

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
		githubToken         string
		destGitHubOrg       string
		destRunnerNamespace string
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
initial build). Pass --yes / -y to auto-confirm without a prompt, or run without
a TTY and later re-run with --apply --yes to enable builds.

When the manifest contains self-hosted runner resource classes, pass
--dest-runner-namespace to recreate them in the destination namespace. If the
flag is omitted, runner classes are flagged for manual recreation — the syncer
never guesses the destination namespace.

Examples:
  circleci-migrate sync --manifest manifest.json --secrets secrets.json
  circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply
  circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply
  circleci-migrate sync --manifest manifest.json --apply --yes
  circleci-migrate sync --manifest manifest.json --dest-runner-namespace acme-new --apply`,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			sy := &syncer.Syncer{
				Org:         orgClient,
				Contexts:    ctxClient,
				Projects:    projClient,
				OrgSettings: orgSettingsAdapter{orgClient},
				Groups:      orgGroupLister{orgClient},
				Out:         cmd.ErrOrStderr(),
			}
			opts := syncer.Options{
				Apply:               apply,
				MissingSecrets:      missing,
				GitHubToken:         githubToken,
				DestGitHubOrg:       destGitHubOrg,
				DestRunnerNamespace: destRunnerNamespace,
			}

			// Wire up the runner client when a destination namespace was provided
			// or the manifest has runner classes (so dry-run preview works).
			if destRunnerNamespace != "" || len(m.RunnerResourceClasses) > 0 {
				runnerClient, rerr := runner.NewClient(rootOptions, token)
				if rerr != nil {
					return fmt.Errorf("creating runner client: %w", rerr)
				}
				sy.Runner = runnerClient
			}

			if !skipOrgSettings {
				rep, err := sy.SyncOrgSettings(m, mapping, opts)
				if err != nil {
					return err
				}
				printSyncReport(cmd, "Org Settings", rep)
			}
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

				// Handle the enable-builds (follow) step for paused projects.
				if err := handleEnableBuilds(cmd, sy, rep, apply, yes); err != nil {
					return err
				}
			}

			// Runner resource classes (always attempted when present in manifest).
			if len(m.RunnerResourceClasses) > 0 || destRunnerNamespace != "" {
				rep, err := sy.SyncRunnerResourceClasses(m, opts)
				if err != nil {
					return err
				}
				printSyncReport(cmd, "Runner Resource Classes", rep)
			}

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVar(&manifestPath, "manifest", "", "Path to the export manifest (required)")
	f.StringVar(&secretsPath, "secrets", "secrets.json", "Path to the captured secret bundle (optional)")
	f.StringVar(&mappingPath, "mapping", "", "Path to a source->destination mapping file (optional)")
	f.BoolVar(&apply, "apply", false, "Write changes to the destination (default: dry run)")
	f.BoolVarP(&yes, "yes", "y", false, "Auto-confirm enabling builds after project creation (skip the interactive prompt)")
	f.StringVar(&missing, "missing-secrets", syncer.MissingSkip, "How to handle variables with no captured value: skip|placeholder")
	f.BoolVar(&skipContexts, "skip-contexts", false, "Skip syncing contexts")
	f.BoolVar(&skipProjects, "skip-projects", false, "Skip syncing projects")
	f.BoolVar(&skipOrgSettings, "skip-org-settings", false, "Skip syncing org-level settings (feature flags, OIDC, URL-orb allow list, config policies)")
	f.StringVar(&githubToken, "github-token", "",
		"GitHub personal access token used to resolve repository IDs when creating pipeline definitions in a GitHub App destination org. Falls back to $GITHUB_TOKEN. Required when repos have been moved to a new GitHub org (--dest-github-org or mapping github_org). When omitted, the captured external_id is reused (correct for same-org migrations).")
	f.StringVar(&destGitHubOrg, "dest-github-org", "",
		"Destination GitHub organization owner (e.g. 'acme-new'). Use when all repos have moved to a new GitHub org. Takes precedence over the source owner when resolving repo external IDs; overridden by an explicit github_org entry in the mapping file. Requires --github-token.")
	f.StringVar(&destRunnerNamespace, "dest-runner-namespace", "",
		"Destination runner namespace for recreating self-hosted runner resource classes (e.g. 'acme-new'). "+
			"Must be supplied explicitly — the syncer never guesses the destination namespace. "+
			"When omitted and the manifest contains runner classes, each is flagged for manual recreation.")

	return cmd
}

// handleEnableBuilds decides whether to follow (enable builds for) the
// paused projects collected in rep.PendingEnable, then executes that decision.
// It prints results to cmd's output/error streams.
func handleEnableBuilds(cmd *cobra.Command, sy *syncer.Syncer, rep *syncer.Report, apply, yes bool) error {
	pending := rep.PendingEnable
	if len(pending) == 0 {
		return nil
	}
	n := len(pending)

	if !apply {
		fmt.Fprintf(cmd.OutOrStdout(),
			"\n%d project(s) would be created paused; re-run with --apply, then confirm enabling builds (or pass --yes).\n", n)
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
		if !isTTY && !yes {
			fmt.Fprintf(cmd.OutOrStdout(),
				"\nSkipped enabling builds (no TTY). Re-run with --yes to follow the %d created project(s).\n", n)
		} else {
			fmt.Fprintf(cmd.OutOrStdout(), "\nSkipped enabling builds.\n")
		}
		return nil
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\nEnabling builds for %d project(s)...\n", n)
	for _, t := range pending {
		action, _ := sy.EnableBuilds(t, true)
		fmt.Fprintf(cmd.OutOrStdout(), "  [%s] %s — %s\n", action.Status, action.Target, action.Detail)
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

func (a orgSettingsAdapter) UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error {
	return a.c.UpdateFeatureFlags(vcsType, orgName, flags)
}
func (a orgSettingsAdapter) SetOIDCClaims(orgID string, audience []string, ttl string) error {
	return a.c.SetOIDCClaims(orgID, audience, ttl)
}
func (a orgSettingsAdapter) CreateURLOrbAllowEntry(slugOrID, name, prefix, auth string) error {
	return a.c.CreateURLOrbAllowEntry(slugOrID, name, prefix, auth)
}
func (a orgSettingsAdapter) PutPolicyBundle(ownerID string, policies map[string]string) error {
	return a.c.PutPolicyBundle(ownerID, policies)
}
func (a orgSettingsAdapter) SetPolicyEnforcement(ownerID string, enabled bool) error {
	return a.c.SetPolicyEnforcement(ownerID, enabled)
}
func (a orgSettingsAdapter) CreateOTelExporter(orgID, endpoint, protocol string, insecure bool, headers map[string]string) error {
	return a.c.CreateOTelExporter(orgID, endpoint, protocol, insecure, headers)
}
func (a orgSettingsAdapter) SetContacts(orgID string, primary, security []string) error {
	return a.c.SetContacts(orgID, primary, security)
}
func (a orgSettingsAdapter) SetStorageRetention(orgUUID string, controls syncer.StorageRetentionArgs) error {
	return a.c.SetStorageRetention(orgUUID, org.StorageRetentionControls{
		CacheDays:     controls.CacheDays,
		WorkspaceDays: controls.WorkspaceDays,
		ArtifactDays:  controls.ArtifactDays,
	})
}
func (a orgSettingsAdapter) SetBudget(orgUUID string, projectID *string, credits int) error {
	return a.c.SetBudget(orgUUID, projectID, credits)
}
func (a orgSettingsAdapter) SetBlockUnregisteredUsers(orgUUID string, enabled bool) error {
	return a.c.SetBlockUnregisteredUsers(orgUUID, enabled)
}
func (a orgSettingsAdapter) SetReleaseTrackerSettings(orgUUID string, ttl string) error {
	return a.c.SetReleaseTrackerSettings(orgUUID, org.ReleaseTrackerSettings{
		InconclusiveReleaseTTL: ttl,
	})
}

// orgGroupLister adapts the org client's ListGroups (returning []org.Group) to
// the syncer.GroupLister interface (returning []syncer.Group).
type orgGroupLister struct {
	c *org.Client
}

func (g orgGroupLister) ListGroups(orgID string) ([]syncer.Group, error) {
	groups, err := g.c.ListGroups(orgID)
	if err != nil {
		return nil, err
	}
	out := make([]syncer.Group, len(groups))
	for i, gr := range groups {
		out[i] = syncer.Group{ID: gr.ID, Name: gr.Name}
	}
	return out, nil
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
