// Internal (white-box) tests for the dependency-injected follow-all core in
// export.go (runFollowAllWithDeps). These exercise the orchestration without
// any real network access by injecting a fake CircleCI client and a fake
// GitHub repo lister.
package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
)

// fakeFollowAllClient implements followAllProjectClient for tests.
type fakeFollowAllClient struct {
	orgProjects   []project.OrgProject
	listErr       error
	followErr     error
	followedRepos []string
	listOrgCalls  int
	followCalls   int
}

func (f *fakeFollowAllClient) ListOrgProjects(_ context.Context, _ string) ([]project.OrgProject, error) {
	f.listOrgCalls++
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.orgProjects, nil
}

func (f *fakeFollowAllClient) FollowProject(_ context.Context, vcsType, org, repo string) (*project.FollowResult, error) {
	f.followCalls++
	if f.followErr != nil {
		return nil, f.followErr
	}
	f.followedRepos = append(f.followedRepos, vcsType+"/"+org+"/"+repo)
	return &project.FollowResult{Followed: true}, nil
}

// fakeRepoLister returns a fixed repo list (or error), ignoring its args.
func fakeRepoLister(repos []github.Repo, err error) func(ctx context.Context, org, token, baseURL string) ([]github.Repo, error) {
	return func(_ context.Context, _, _, _ string) ([]github.Repo, error) {
		return repos, err
	}
}

func TestRunFollowAllWithDeps_GHOrg_FollowsUnknownReposOnly(t *testing.T) {
	fc := &fakeFollowAllClient{}
	lister := fakeRepoLister([]github.Repo{
		{Name: "web"},
		{Name: "api"},
		{Name: "old", Archived: true}, // archived → skipped
	}, nil)

	var out strings.Builder
	err := runFollowAllWithDeps(context.Background(), "gh/acme", "tok", fc, lister, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// gh/ orgs skip the ListOrgProjects pre-check.
	if fc.listOrgCalls != 0 {
		t.Errorf("expected ListOrgProjects not called for gh/ org, got %d calls", fc.listOrgCalls)
	}
	// web + api followed; archived "old" skipped.
	if fc.followCalls != 2 {
		t.Errorf("expected 2 follow calls, got %d (%v)", fc.followCalls, fc.followedRepos)
	}
	if !strings.Contains(out.String(), "2 repo(s) followed") {
		t.Errorf("expected summary line in output, got:\n%s", out.String())
	}
}

func TestRunFollowAllWithDeps_CircleCIOrg_BuildsKnownAndSkipsFollow(t *testing.T) {
	fc := &fakeFollowAllClient{
		orgProjects: []project.OrgProject{{Slug: "circleci/abc/web"}},
	}
	lister := fakeRepoLister([]github.Repo{{Name: "web"}}, nil)

	var out strings.Builder
	err := runFollowAllWithDeps(context.Background(), "circleci/abc", "tok", fc, lister, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// circleci/ slug → ListOrgProjects is consulted to build the known set.
	if fc.listOrgCalls != 1 {
		t.Errorf("expected ListOrgProjects called once, got %d", fc.listOrgCalls)
	}
	// FollowAll short-circuits for non-gh orgs, so nothing is followed.
	if fc.followCalls != 0 {
		t.Errorf("expected 0 follow calls for circleci/ org, got %d", fc.followCalls)
	}
}

func TestRunFollowAllWithDeps_ListReposError_Wrapped(t *testing.T) {
	fc := &fakeFollowAllClient{}
	lister := fakeRepoLister(nil, errors.New("boom"))

	var out strings.Builder
	err := runFollowAllWithDeps(context.Background(), "gh/acme", "tok", fc, lister, &out)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "follow-all") {
		t.Errorf("expected wrapped follow-all error, got: %v", err)
	}
}

func TestRunFollowAllWithDeps_FollowError_Propagates(t *testing.T) {
	fc := &fakeFollowAllClient{followErr: errors.New("denied")}
	lister := fakeRepoLister([]github.Repo{{Name: "web"}}, nil)

	var out strings.Builder
	err := runFollowAllWithDeps(context.Background(), "gh/acme", "tok", fc, lister, &out)
	if err == nil {
		t.Fatal("expected error from FollowProject, got nil")
	}
}
