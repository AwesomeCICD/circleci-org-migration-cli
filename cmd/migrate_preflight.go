package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/preflight"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// preflightDeps groups the inputs for runMigratePreflight so that tests can
// supply fake values without wiring a full cobra command.
type preflightDeps struct {
	cfg           *settings.Config
	srcToken      string
	dstToken      string
	sourceOrg     string
	destOrg       string
	githubToken   string
	destGitHubOrg string
}

// orgGetter retrieves an org by slug or ID.
type orgGetter interface {
	GetOrganization(ctx context.Context, slugOrID string) (*org.Organization, error)
}

// featureFlagGetter retrieves org feature flags.
type featureFlagGetter interface {
	GetFeatureFlags(ctx context.Context, vcsType, orgName string) (map[string]bool, error)
}

// projectLister lists org projects via the private API.
type projectLister interface {
	ListOrgProjects(ctx context.Context, orgID string) ([]project.OrgProject, error)
}

// preflightClients holds the API clients used by preflight checks. All fields
// may be nil; each check handles nil gracefully (returning a WARN).
type preflightClients struct {
	srcOrg      orgGetter
	dstOrg      orgGetter
	srcFlags    featureFlagGetter
	srcProjects projectLister
}

// runMigratePreflight executes all preflight checks, prints the summary, and
// offers interactive fixes where applicable.
//
// Hard-failure rule: only return a non-nil error for:
//   - Missing tokens (StatusFail from checkTokens)
//   - Destination org unreachable (StatusFail from checkDestOrg)
//
// All other failures are downgraded to StatusWarn so the migration can proceed
// with operator acknowledgement.
//
// out is written to (typically cmd.ErrOrStderr()).  The prompter uses os.Stdin
// (overridable via preflightStdinReader for tests).
func runMigratePreflight(
	ctx context.Context,
	deps preflightDeps,
	clients preflightClients,
	out io.Writer,
) error {
	var results []preflight.Result

	// ---- Check 1: Tokens ------------------------------------------------
	tokResult := checkTokens(deps)
	results = append(results, tokResult)
	if tokResult.Status == preflight.StatusFail {
		preflight.PrintSummary(out, results)
		return fmt.Errorf("preflight: %s", tokResult.Detail)
	}

	// ---- Check 2: Destination org reachable -----------------------------
	destResult, destOrg := checkDestOrg(ctx, clients.dstOrg, deps.destOrg)
	results = append(results, destResult)
	if destResult.Status == preflight.StatusFail {
		preflight.PrintSummary(out, results)
		return fmt.Errorf("preflight: %s", destResult.Detail)
	}

	// ---- Check 3: Source org reachable (best-effort, WARN on failure) ---
	srcResult, srcOrg := checkSrcOrg(ctx, clients.srcOrg, deps.sourceOrg)
	results = append(results, srcResult)

	// ---- Check 4: Cross-type warning ------------------------------------
	crossType := false
	if srcOrg != nil && destOrg != nil {
		xtResult := checkCrossType(srcOrg, destOrg)
		results = append(results, xtResult)
		crossType = xtResult.Status == preflight.StatusWarn
	}

	// ---- Check 5: Source api-trigger flag (best-effort) -----------------
	if srcOrg != nil {
		results = append(results, checkAPITriggerFlag(ctx, clients.srcFlags, srcOrg))
	}

	// ---- Check 6: Project discovery preview (best-effort) ---------------
	if srcOrg != nil {
		results = append(results, checkProjectDiscovery(ctx, clients.srcProjects, srcOrg))
	}

	// ---- Check 7: GitHub token for repo resolution ----------------------
	if needsGitHubToken(deps, crossType) {
		results = append(results, checkGitHubToken(deps.githubToken))
	}

	// ---- Check 8: Recommended order reminder ----------------------------
	results = append(results, preflight.Result{
		Name:   "Recommended order",
		Status: preflight.StatusOK,
		Detail: "export → secrets capture → sync (dry-run) → sync --apply → enable builds → rotate tokens",
	})

	// ---- Print summary --------------------------------------------------
	_, warn, fail := preflight.PrintSummary(out, results)

	if fail > 0 {
		// Shouldn't reach here (hard failures returned early), but be safe.
		return fmt.Errorf("preflight failed with %d blocker(s); address them before retrying", fail)
	}

	// Interactive confirm on warnings.
	if warn > 0 && isInteractiveTTY() {
		p := NewPrompter(os.Stdin, out)
		cont, err := p.askBool(
			fmt.Sprintf("Preflight found %d warning(s). Continue?", warn),
			true,
		)
		if err != nil {
			return fmt.Errorf("preflight: reading confirmation: %w", err)
		}
		if !cont {
			return fmt.Errorf("migration cancelled at preflight")
		}
	}

	return nil
}

// ---- Individual checks -------------------------------------------------

// checkTokens validates that both source and dest tokens are resolved.
func checkTokens(deps preflightDeps) preflight.Result {
	var missing []string
	if deps.srcToken == "" {
		missing = append(missing, "source token (set CIRCLECI_SOURCE_TOKEN, --source-token, or CIRCLECI_CLI_TOKEN)")
	}
	if deps.dstToken == "" {
		missing = append(missing, "destination token (set CIRCLECI_DEST_TOKEN, --dest-token, or CIRCLECI_CLI_TOKEN)")
	}
	if len(missing) > 0 {
		return preflight.Result{
			Name:   "Tokens",
			Status: preflight.StatusFail,
			Detail: "Missing: " + strings.Join(missing, "; "),
		}
	}
	return preflight.Result{
		Name:   "Tokens",
		Status: preflight.StatusOK,
		Detail: "Source and destination tokens are set.",
	}
}

// checkDestOrg verifies the destination org is reachable via the API.
// Returns StatusFail if the org cannot be reached (hard blocker).
func checkDestOrg(ctx context.Context, client orgGetter, destSlug string) (preflight.Result, *org.Organization) {
	if client == nil {
		return preflight.Result{
			Name:   "Destination org reachable",
			Status: preflight.StatusWarn,
			Detail: "No destination org client available for preflight.",
		}, nil
	}
	o, err := client.GetOrganization(ctx, destSlug)
	if err != nil {
		return preflight.Result{
			Name:   "Destination org reachable",
			Status: preflight.StatusFail,
			Detail: fmt.Sprintf("Cannot reach destination org %q: %v — check --dest-org and your destination token.", destSlug, err),
		}, nil
	}
	return preflight.Result{
		Name:   "Destination org reachable",
		Status: preflight.StatusOK,
		Detail: fmt.Sprintf("Destination org %q (type: %s) is reachable.", o.Name, o.VCSType),
	}, o
}

// checkSrcOrg fetches the source org; on error the result is StatusWarn
// (not StatusFail) because the source being readable is not a hard blocker.
func checkSrcOrg(ctx context.Context, client orgGetter, srcSlug string) (preflight.Result, *org.Organization) {
	if client == nil {
		return preflight.Result{
			Name:   "Source org reachable",
			Status: preflight.StatusWarn,
			Detail: "No source org client available for preflight.",
		}, nil
	}
	o, err := client.GetOrganization(ctx, srcSlug)
	if err != nil {
		return preflight.Result{
			Name:   "Source org reachable",
			Status: preflight.StatusWarn,
			Detail: fmt.Sprintf("Could not read source org %q: %v — check --source-org and your source token.", srcSlug, err),
		}, nil
	}
	return preflight.Result{
		Name:   "Source org reachable",
		Status: preflight.StatusOK,
		Detail: fmt.Sprintf("Source org %q (type: %s) is reachable.", o.Name, o.VCSType),
	}, o
}

// checkCrossType warns when source and destination org types differ.
func checkCrossType(srcOrg, dstOrg *org.Organization) preflight.Result {
	srcType := normalizeOrgType(srcOrg.VCSType)
	dstType := normalizeOrgType(dstOrg.VCSType)
	if srcType == dstType {
		return preflight.Result{
			Name:   "Cross-type migration",
			Status: preflight.StatusOK,
			Detail: fmt.Sprintf("Both orgs are the same type (%s).", srcType),
		}
	}
	return preflight.Result{
		Name:   "Cross-type migration",
		Status: preflight.StatusWarn,
		Detail: fmt.Sprintf(
			"Source org type (%s) differs from destination (%s). "+
				"This is a cross-type migration: some features may not transfer (e.g. group context restrictions, "+
				"GitHub OAuth app triggers). See docs/playbooks/cross-type-oauth-to-app.md for details.",
			srcType, dstType,
		),
	}
}

// normalizeOrgType maps VCS type strings to short human-readable labels.
func normalizeOrgType(vcsType string) string {
	switch strings.ToLower(vcsType) {
	case "github", "gh", "github_oauth":
		return "GitHub OAuth"
	case "github_app":
		return "GitHub App (standalone)"
	case "circleci":
		return "CircleCI standalone"
	case "bitbucket":
		return "Bitbucket"
	default:
		if vcsType == "" {
			return "unknown"
		}
		return vcsType
	}
}

// checkAPITriggerFlag reads the source org's allow_api_trigger_with_config flag.
// If OFF, emits a non-blocking WARN advising the operator.
func checkAPITriggerFlag(ctx context.Context, client featureFlagGetter, srcOrg *org.Organization) preflight.Result {
	vcsType, orgName := splitForV11(srcOrg)
	if client == nil || vcsType == "" || orgName == "" {
		return preflight.Result{
			Name:   "Source api-trigger flag",
			Status: preflight.StatusOK,
			Detail: "Skipping api-trigger check (org not in v1.1 slug form or no client).",
		}
	}

	flags, err := client.GetFeatureFlags(ctx, vcsType, orgName)
	if err != nil {
		return preflight.Result{
			Name:   "Source api-trigger flag",
			Status: preflight.StatusWarn,
			Detail: fmt.Sprintf("Could not read source feature flags: %v. Proceeding.", err),
		}
	}

	if capture.OrgTriggerAlreadyEnabled(flags) {
		return preflight.Result{
			Name:   "Source api-trigger flag",
			Status: preflight.StatusOK,
			Detail: "allow_api_trigger_with_config is already enabled on the source org.",
		}
	}
	return preflight.Result{
		Name:   "Source api-trigger flag",
		Status: preflight.StatusWarn,
		Detail: "allow_api_trigger_with_config is OFF on the source org. " +
			"Secrets capture and transfer need it enabled. " +
			"The CLI enables it automatically during 'secrets capture' (or pass --enable-trigger). " +
			"You do NOT need to enable it now.",
		Fixable: false,
	}
}

// splitForV11 derives the (vcsType, orgName) pair that the v1.1 API
// GetFeatureFlags expects from an org struct.
func splitForV11(o *org.Organization) (vcsType, orgName string) {
	if o.Slug != "" {
		parts := strings.SplitN(o.Slug, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1]
		}
	}
	return o.VCSType, o.Name
}

// checkProjectDiscovery runs ListOrgProjects and reports count + discovery path.
func checkProjectDiscovery(ctx context.Context, client projectLister, srcOrg *org.Organization) preflight.Result {
	if client == nil || srcOrg.ID == "" {
		return preflight.Result{
			Name:   "Project discovery",
			Status: preflight.StatusWarn,
			Detail: "Cannot run project discovery without a source org UUID. Discovery will run during export.",
		}
	}

	projects, err := client.ListOrgProjects(ctx, srcOrg.ID)
	if err != nil {
		return preflight.Result{
			Name:   "Project discovery",
			Status: preflight.StatusWarn,
			Detail: fmt.Sprintf(
				"Private project-list API unavailable (%v). "+
					"Export will fall back to the followed-projects list (v1.1): repositories not followed by "+
					"your token's user may be missing — pass --project explicitly to be exhaustive. "+
					"Note: Insights-based tools miss idle projects entirely; the followed-list beats that.",
				err,
			),
		}
	}

	n := len(projects)
	if n == 0 {
		return preflight.Result{
			Name:   "Project discovery",
			Status: preflight.StatusWarn,
			Detail: "Private API returned 0 projects. The org may have no projects set up in CircleCI, " +
				"or your token may lack org-admin scope. Repositories never set up in CircleCI will not appear.",
		}
	}
	return preflight.Result{
		Name:   "Project discovery",
		Status: preflight.StatusOK,
		Detail: fmt.Sprintf(
			"%d project(s) found via private API (complete list — not limited to followed projects). "+
				"Repositories never set up in CircleCI will not appear (this is expected).",
			n,
		),
	}
}

// needsGitHubToken returns true when a GitHub personal token will be needed.
func needsGitHubToken(deps preflightDeps, crossType bool) bool {
	return crossType || deps.destGitHubOrg != ""
}

// checkGitHubToken warns if a GitHub token appears necessary but is absent.
func checkGitHubToken(githubToken string) preflight.Result {
	if githubToken != "" {
		return preflight.Result{
			Name:   "GitHub token for repo resolution",
			Status: preflight.StatusOK,
			Detail: "GitHub token is set.",
		}
	}
	return preflight.Result{
		Name:   "GitHub token for repo resolution",
		Status: preflight.StatusWarn,
		Detail: "Cross-type or repo-move migration detected but --github-token is absent. " +
			"It is required to resolve destination repository IDs when creating pipeline definitions on a GitHub App org. " +
			"Set --github-token or $GITHUB_TOKEN before running sync.",
	}
}
