package cciurl_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/cciurl"
)

// ---------------------------------------------------------------------------
// AppHost
// ---------------------------------------------------------------------------

func TestAppHost_CircleCICloud(t *testing.T) {
	for _, h := range []string{"https://circleci.com", "http://circleci.com", "https://circleci.com/"} {
		got := cciurl.AppHost(h)
		if got != "https://app.circleci.com" {
			t.Errorf("AppHost(%q) = %q, want https://app.circleci.com", h, got)
		}
	}
}

func TestAppHost_Empty(t *testing.T) {
	got := cciurl.AppHost("")
	if got != "https://app.circleci.com" {
		t.Errorf("AppHost(\"\") = %q, want https://app.circleci.com", got)
	}
}

func TestAppHost_ServerDeployment(t *testing.T) {
	got := cciurl.AppHost("https://circleci.example.com")
	if got != "https://circleci.example.com" {
		t.Errorf("AppHost(server) = %q, want https://circleci.example.com", got)
	}
}

// ---------------------------------------------------------------------------
// ProjectSettingsURL
// ---------------------------------------------------------------------------

func TestProjectSettingsURL_OAuth_GH(t *testing.T) {
	got := cciurl.ProjectSettingsURL("https://circleci.com", "gh/acme/web", "ssh")
	want := "https://app.circleci.com/settings/project/gh/acme/web/ssh"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectSettingsURL_OAuth_BB(t *testing.T) {
	got := cciurl.ProjectSettingsURL("https://circleci.com", "bb/acme/web", "env-vars")
	want := "https://app.circleci.com/settings/project/bb/acme/web/env-vars"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectSettingsURL_Standalone_NoTab(t *testing.T) {
	slug := "circleci/org-uuid-123/proj-uuid-456"
	got := cciurl.ProjectSettingsURL("https://circleci.com", slug, "")
	want := "https://app.circleci.com/settings/project/circleci/org-uuid-123/proj-uuid-456"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestProjectSettingsURL_MalformedSlug(t *testing.T) {
	got := cciurl.ProjectSettingsURL("https://circleci.com", "bad-slug", "")
	if !strings.Contains(got, "/projects") {
		t.Errorf("malformed slug should fall back to /projects, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// OrgSettingsURL
// ---------------------------------------------------------------------------

func TestOrgSettingsURL_GH(t *testing.T) {
	got := cciurl.OrgSettingsURL("https://circleci.com", "gh/acme", "contexts")
	want := "https://app.circleci.com/settings/organization/gh/acme/contexts"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrgSettingsURL_Standalone(t *testing.T) {
	got := cciurl.OrgSettingsURL("https://circleci.com", "circleci/org-uuid-123", "")
	want := "https://app.circleci.com/settings/organization/circleci/org-uuid-123"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestOrgSettingsURL_MalformedSlug(t *testing.T) {
	got := cciurl.OrgSettingsURL("https://circleci.com", "invalid", "")
	if !strings.Contains(got, "/settings/organization") {
		t.Errorf("malformed org slug should fall back gracefully, got %q", got)
	}
}
