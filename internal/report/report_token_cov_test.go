package report

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// The report's manual-steps must include a Project API tokens section, with the
// friendly project name, each token's label+scope, a settings URL, and the docs
// link, when any project has captured API tokens.
func TestMarkdown_ProjectAPITokens_ManualSection(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org:  manifest.Org{Slug: "gh/acme", ID: "org-uuid", Name: "acme", VCSType: "github"},
		},
		Projects: []manifest.Project{
			{
				Slug: "gh/acme/web", Name: "web",
				APITokens: []manifest.ProjectAPIToken{
					{Label: "deploy-bot", Scope: "all"},
					{Label: "badge", Scope: "status"},
				},
			},
			{Slug: "gh/acme/no-tokens", Name: "no-tokens"},
		},
	}
	out := Markdown(m)
	for _, want := range []string{
		"Project API tokens",
		"**web**",                            // friendly name
		"deploy-bot (all)", "badge (status)", // label (scope)
		"app.circleci.com/settings/project/gh/acme/web", // settings URL
		"docs/guides/toolkit/managing-api-tokens",       // docs link
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q\n---\n%s", want, out)
		}
	}
}

// When NO project has API tokens, the API-token manual section is absent.
func TestMarkdown_ProjectAPITokens_AbsentWhenNone(t *testing.T) {
	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Host: "https://circleci.com", Org: manifest.Org{Slug: "gh/acme", Name: "acme"}},
		Projects:      []manifest.Project{{Slug: "gh/acme/web", Name: "web"}},
	}
	if strings.Contains(Markdown(m), "Project API tokens") {
		t.Error("API-token section should be absent when no project has tokens")
	}
}
