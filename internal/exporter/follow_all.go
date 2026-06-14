package exporter

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
)

// GitHubRepoLister lists all repositories for a GitHub organisation.
// Implemented by github.ListOrgRepos in production; replaced by a fake in tests.
type GitHubRepoLister func(ctx context.Context, org, token, baseURL string) ([]github.Repo, error)

// ProjectFollower follows a project on CircleCI (POST /api/v1.1/project/gh/<org>/<repo>/follow).
// Implemented by project.Client.FollowProject in production; replaced by a fake in tests.
type ProjectFollower func(ctx context.Context, vcsType, org, repo string) error

// FollowAllOptions controls the follow-all behaviour.
type FollowAllOptions struct {
	// GitHubToken is the personal access token for the GitHub API.
	GitHubToken string
	// GitHubBaseURL overrides the GitHub API base URL (defaults to https://api.github.com).
	// Used in tests to point at a local httptest.Server.
	GitHubBaseURL string
	// OrgSlug is the source CircleCI org slug (e.g. "gh/acme").
	OrgSlug string
	// KnownSlugs is the set of project slugs already discovered by ListOrgProjects.
	// Repos whose CircleCI slug would be in this set are skipped.
	KnownSlugs map[string]struct{}
	// Out is the writer for progress messages (may be nil for silence).
	Out io.Writer
}

// webhookValidationMsg is the substring present in the CircleCI v1.1 API error
// body when a newly-created GitHub repository has not yet had any branch pushed,
// causing the webhook validation to fail. The follow-all loop treats this as a
// recoverable warning rather than a hard error.
const webhookValidationMsg = "Unable to create webhook due to a validation error"

// FollowAll follows every GitHub repository in the source org that is not yet
// a CircleCI project, then returns the set of newly-followed repo names so that
// the caller can re-run discovery to pick them up.
//
// Rules:
//   - Only applicable to GitHub OAuth orgs (slug prefix "gh/").
//   - Requires GitHubToken; callers must validate this before calling.
//   - Archived repos are skipped.
//   - Repos already in opts.KnownSlugs (as "gh/<org>/<repo>") are skipped.
//   - Webhook-validation errors are logged as warnings and do not abort the loop.
//
// Returns the number of repositories followed (including webhook-warned ones) and
// any hard error. A hard error stops the loop; individual webhook errors are not
// returned here (they are written to opts.Out).
func FollowAll(
	ctx context.Context,
	listRepos GitHubRepoLister,
	followProject ProjectFollower,
	opts FollowAllOptions,
) (followed int, err error) {
	parts := strings.SplitN(opts.OrgSlug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return 0, fmt.Errorf("follow-all: invalid org slug %q", opts.OrgSlug)
	}
	vcsPrefix := parts[0]
	orgName := parts[1]

	if vcsPrefix != "gh" {
		// Non-GitHub-OAuth org: skip with an informational note.
		logFollowAll(opts.Out,
			"Note: --follow-all is not applicable to circleci/ (App/standalone) orgs — "+
				"projects in those orgs are not 'followed' in the same way. Skipping.")
		return 0, nil
	}

	logFollowAll(opts.Out, "follow-all: listing GitHub repos for org %q...", orgName)
	repos, err := listRepos(ctx, orgName, opts.GitHubToken, opts.GitHubBaseURL)
	if err != nil {
		return 0, fmt.Errorf("follow-all: list GitHub repos: %w", err)
	}
	logFollowAll(opts.Out, "follow-all: %d GitHub repo(s) found", len(repos))

	for _, r := range repos {
		if r.Archived {
			continue
		}
		slug := opts.OrgSlug + "/" + r.Name
		if _, already := opts.KnownSlugs[slug]; already {
			continue
		}
		logFollowAll(opts.Out, "follow-all: following %q...", slug)
		ferr := followProject(ctx, "gh", orgName, r.Name)
		if ferr != nil {
			if strings.Contains(ferr.Error(), webhookValidationMsg) {
				// Brand-new repo with no branch — warn and continue.
				logFollowAll(opts.Out,
					"follow-all: WARNING: %q — webhook validation failed (new repo with no branch?); continuing.",
					r.Name)
				followed++
				continue
			}
			// Hard error: stop the loop so the caller can surface it.
			return followed, fmt.Errorf("follow-all: follow %q: %w", r.Name, ferr)
		}
		logFollowAll(opts.Out, "follow-all: followed %q", slug)
		followed++
	}
	return followed, nil
}

func logFollowAll(w io.Writer, format string, args ...any) {
	if w == nil {
		return
	}
	fmt.Fprintf(w, format+"\n", args...)
}
