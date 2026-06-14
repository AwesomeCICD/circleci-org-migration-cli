package exporter_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
)

// ---------------------------------------------------------------------------
// Fake helpers
// ---------------------------------------------------------------------------

// fakeRepoLister returns the given repos regardless of org/token/baseURL.
func fakeRepoLister(repos []github.Repo) exporter.GitHubRepoLister {
	return func(_ context.Context, _, _, _ string) ([]github.Repo, error) {
		return repos, nil
	}
}

// fakeRepoListerErr always returns an error.
func fakeRepoListerErr(err error) exporter.GitHubRepoLister {
	return func(_ context.Context, _, _, _ string) ([]github.Repo, error) {
		return nil, err
	}
}

// recordingFollower records which (vcsType, org, repo) triples were followed
// and optionally returns an error for specific repo names.
type recordingFollower struct {
	calls    []string // "<org>/<repo>"
	errRepos map[string]error
}

func (r *recordingFollower) follow(_ context.Context, vcsType, org, repo string) error {
	_ = vcsType
	r.calls = append(r.calls, org+"/"+repo)
	if r.errRepos != nil {
		if err, ok := r.errRepos[repo]; ok {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestFollowAll_GHOrg_TwoUnknownRepos(t *testing.T) {
	repos := []github.Repo{
		{Name: "web"},
		{Name: "api"},
	}
	follower := &recordingFollower{}
	var out bytes.Buffer

	n, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(repos),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  map[string]struct{}{},
			Out:         &out,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("followed: got %d, want 2", n)
	}
	if len(follower.calls) != 2 {
		t.Errorf("follower calls: got %v, want [acme/web acme/api]", follower.calls)
	}
}

func TestFollowAll_GHOrg_AlreadyOnboardedRepoSkipped(t *testing.T) {
	repos := []github.Repo{
		{Name: "web"},
		{Name: "api"},
	}
	follower := &recordingFollower{}

	known := map[string]struct{}{
		"gh/acme/web": {},
	}
	n, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(repos),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  known,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("followed: got %d, want 1", n)
	}
	if len(follower.calls) != 1 || follower.calls[0] != "acme/api" {
		t.Errorf("follower calls: got %v, want [acme/api]", follower.calls)
	}
}

func TestFollowAll_GHOrg_WebhookErrorWarned_OthersContinue(t *testing.T) {
	repos := []github.Repo{
		{Name: "new-repo"},  // will trigger webhook validation error
		{Name: "good-repo"}, // should still be followed
	}
	webhookErr := fmt.Errorf("FollowProject gh/acme/new-repo: Unable to create webhook due to a validation error (repo has no branch yet)")
	follower := &recordingFollower{
		errRepos: map[string]error{"new-repo": webhookErr},
	}
	var out bytes.Buffer

	n, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(repos),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  map[string]struct{}{},
			Out:         &out,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Both repos should be counted as "followed" (even though one warned).
	if n != 2 {
		t.Errorf("followed: got %d, want 2", n)
	}
	// The warning should be printed.
	if !containsStr(out.String(), "WARNING") {
		t.Errorf("expected WARNING in output, got: %s", out.String())
	}
	// good-repo should still be followed.
	if !containsStr(follower.calls[1], "good-repo") {
		t.Errorf("good-repo should be followed even after webhook error on new-repo; calls: %v", follower.calls)
	}
}

func TestFollowAll_CircleCIOrgSkipped(t *testing.T) {
	follower := &recordingFollower{}
	var out bytes.Buffer

	n, err := exporter.FollowAll(context.Background(),
		fakeRepoListerErr(errors.New("should not be called")),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "circleci/some-uuid",
			KnownSlugs:  map[string]struct{}{},
			Out:         &out,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("followed: got %d, want 0", n)
	}
	if len(follower.calls) != 0 {
		t.Errorf("follower should not be called for circleci/ orgs; calls: %v", follower.calls)
	}
	if !containsStr(out.String(), "not applicable") {
		t.Errorf("expected 'not applicable' note in output, got: %s", out.String())
	}
}

func TestFollowAll_ArchivedRepoSkipped(t *testing.T) {
	repos := []github.Repo{
		{Name: "active"},
		{Name: "archived", Archived: true},
	}
	follower := &recordingFollower{}

	n, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(repos),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  map[string]struct{}{},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("followed: got %d, want 1 (archived skipped)", n)
	}
	for _, c := range follower.calls {
		if containsStr(c, "archived") {
			t.Errorf("archived repo should not be followed; calls: %v", follower.calls)
		}
	}
}

func TestFollowAll_HardFollowError_Aborts(t *testing.T) {
	repos := []github.Repo{
		{Name: "bad-repo"},
		{Name: "good-repo"},
	}
	hardErr := errors.New("permission denied")
	follower := &recordingFollower{
		errRepos: map[string]error{"bad-repo": hardErr},
	}

	_, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(repos),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  map[string]struct{}{},
		},
	)
	if err == nil {
		t.Fatal("expected error for hard follow failure, got nil")
	}
	// good-repo should not be attempted after the hard error.
	for _, c := range follower.calls {
		if containsStr(c, "good-repo") {
			t.Errorf("good-repo should not be followed after hard error; calls: %v", follower.calls)
		}
	}
}

func TestFollowAll_ListReposError(t *testing.T) {
	follower := &recordingFollower{}
	_, err := exporter.FollowAll(context.Background(),
		fakeRepoListerErr(errors.New("rate limited")),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "gh/acme",
			KnownSlugs:  map[string]struct{}{},
		},
	)
	if err == nil {
		t.Fatal("expected error when ListOrgRepos fails, got nil")
	}
}

func TestFollowAll_InvalidOrgSlug(t *testing.T) {
	follower := &recordingFollower{}
	_, err := exporter.FollowAll(context.Background(),
		fakeRepoLister(nil),
		follower.follow,
		exporter.FollowAllOptions{
			GitHubToken: "token",
			OrgSlug:     "badslug",
			KnownSlugs:  map[string]struct{}{},
		},
	)
	if err == nil {
		t.Fatal("expected error for invalid org slug, got nil")
	}
}

// containsStr is a convenience wrapper around strings.Contains for test readability.
func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
