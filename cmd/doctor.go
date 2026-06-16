package cmd

import (
	"fmt"
	"os"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var (
		sourceOrg     string
		destOrg       string
		githubToken   string
		destGitHubOrg string
	)

	cmd := &cobra.Command{
		Use:   "doctor [--source-org <slug>] [--dest-org <slug>]",
		Short: "Run migration preflight checks without migrating.",
		Long: `doctor runs the same preflight checks as 'migrate' but exits immediately
after printing the summary — it does not export or sync any data.

Use it to validate your tokens, org slugs, and configuration before running a
migration. It is safe to run as many times as needed; it is entirely read-only.

If --dest-org is omitted, only source-side checks are run (token present, source
org reachable, api-trigger flag state, and project discovery). If --source-org is
omitted, only destination-side checks are run (token present, destination org
reachable, cross-type warning, GitHub token for repo resolution).

Exit codes:
  0 — all checks passed (OK or warnings only)
  1 — one or more hard failures (missing required token or unreachable org)

Examples:
  # Check both source and destination:
  circleci-migrate doctor --source-org gh/acme --dest-org gh/acme-new

  # Source-side only (validate before export):
  circleci-migrate doctor --source-org gh/acme

  # Destination-side only (validate before sync):
  circleci-migrate doctor --dest-org gh/acme-new`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			cfg := configFromContext(ctx)

			// Resolve the GitHub token from the env after parsing so the flag
			// default never leaks $GITHUB_TOKEN into --help output.
			if githubToken == "" {
				githubToken = os.Getenv("GITHUB_TOKEN")
			}

			srcToken := cfg.SourceTokenOrDefault()
			dstToken := cfg.DestTokenOrDefault()

			// Decide which side(s) to check based on which org flags are present.
			hasSrc := sourceOrg != ""
			hasDst := destOrg != ""

			if !hasSrc && !hasDst {
				return fmt.Errorf(
					"at least one of --source-org or --dest-org is required\n" +
						"  source-only:  doctor --source-org gh/acme\n" +
						"  dest-only:    doctor --dest-org gh/acme-new\n" +
						"  both:         doctor --source-org gh/acme --dest-org gh/acme-new")
			}

			out := cmd.ErrOrStderr()

			if hasSrc && !hasDst {
				// Source-only check: token + reachability + api-trigger + discovery.
				var pfSrcOrgClient orgGetter
				var pfFlagClient featureFlagGetter
				var pfProjClient projectLister
				var pfOrgMgr capture.OrgFlagManager

				if srcToken != "" {
					if c, err := org.NewClient(cfg, srcToken); err == nil {
						pfSrcOrgClient = c
						pfFlagClient = c
						pfOrgMgr = c
					}
					if c, err := project.NewClient(cfg, srcToken); err == nil {
						pfProjClient = c
					}
				}

				deps := preflightDeps{
					cfg:       cfg,
					srcToken:  srcToken,
					sourceOrg: sourceOrg,
				}
				clients := preflightClients{
					srcOrg:      pfSrcOrgClient,
					srcFlags:    pfFlagClient,
					srcOrgMgr:   pfOrgMgr,
					srcProjects: pfProjClient,
				}
				return runExportPreflight(ctx, deps, clients, out)
			}

			if hasDst && !hasSrc {
				// Dest-only check: token + reachability + github-token.
				var pfDstOrgClient orgGetter

				if dstToken != "" {
					if c, err := org.NewClient(cfg, dstToken); err == nil {
						pfDstOrgClient = c
					}
				}

				deps := preflightDeps{
					cfg:           cfg,
					dstToken:      dstToken,
					destOrg:       destOrg,
					githubToken:   githubToken,
					destGitHubOrg: destGitHubOrg,
				}
				clients := preflightClients{
					dstOrg: pfDstOrgClient,
				}
				// Run sync-side preflight with no manifest source type (cross-type
				// check is skipped when the manifest type is unknown).
				return runSyncPreflight(ctx, deps, clients, "", out)
			}

			// Both source and destination: run the full migrate preflight.
			pfSrcOrgClient, pfErr := org.NewClient(cfg, srcToken)
			pfDstOrgClient, pfErr2 := org.NewClient(cfg, dstToken)
			pfProjClient, pfErr3 := project.NewClient(cfg, srcToken)

			var pfClients preflightClients
			if pfErr == nil {
				pfClients.srcOrg = pfSrcOrgClient
				pfClients.srcFlags = pfSrcOrgClient
				pfClients.srcOrgMgr = pfSrcOrgClient
			}
			if pfErr2 == nil {
				pfClients.dstOrg = pfDstOrgClient
			}
			if pfErr3 == nil {
				pfClients.srcProjects = pfProjClient
			}
			if pfErr != nil || pfErr2 != nil || pfErr3 != nil {
				fmt.Fprintf(out, "warning: preflight client init partial: src=%v dst=%v proj=%v\n",
					pfErr, pfErr2, pfErr3)
			}

			deps := preflightDeps{
				cfg:           cfg,
				srcToken:      srcToken,
				dstToken:      dstToken,
				sourceOrg:     sourceOrg,
				destOrg:       destOrg,
				githubToken:   githubToken,
				destGitHubOrg: destGitHubOrg,
			}
			return runMigratePreflight(ctx, deps, pfClients, out)
		},
	}

	f := cmd.Flags()
	f.StringVar(&sourceOrg, "source-org", "",
		"CircleCI organization slug for the source org, e.g. gh/my-org "+
			"(shown in CircleCI → Organization Settings → Overview). "+
			"This is the CircleCI org identifier, not a GitHub repository URL. "+
			"When provided, source-side checks are run (token, reachability, api-trigger flag, project discovery). "+
			"May be combined with --dest-org to run both sides.")
	f.StringVar(&destOrg, "dest-org", "",
		"CircleCI organization slug for the destination org, e.g. gh/my-new-org "+
			"(shown in CircleCI → Organization Settings → Overview). "+
			"This is the CircleCI org identifier, not a GitHub repository URL. "+
			"When provided, destination-side checks are run (token, reachability, cross-type warning, GitHub token). "+
			"May be combined with --source-org to run both sides.")
	f.StringVar(&githubToken, "github-token", "",
		"GitHub personal access token used to resolve repository IDs when creating pipeline definitions "+
			"in a GitHub App destination org. Falls back to $GITHUB_TOKEN.")
	f.StringVar(&destGitHubOrg, "dest-github-org", "",
		"Destination GitHub organization owner (e.g. 'acme-new'). Use when repos have moved to a new "+
			"GitHub org. Triggers the GitHub-token check.")

	return cmd
}
