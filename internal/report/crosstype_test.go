package report_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/report"
)

const crossTypeHeading = "Cross-type checklist: GitHub OAuth"

func crossTypeManifest(srcSlug string) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion:   manifest.SchemaVersion,
		OrbNamespace:    "bcbsm",
		RunnerNamespace: "bcbsm",
		RunnerResourceClasses: []manifest.RunnerResourceClass{
			{Name: "bcbsm/fast"},
		},
		Source: manifest.Source{Org: manifest.Org{Name: "bcbsm", Slug: srcSlug}},
	}
}

func TestMarkdownWith_CrossTypeChecklist_OnlyForOAuthToApp(t *testing.T) {
	cases := []struct {
		name string
		src  string
		dest string
		want bool
	}{
		{"gh to circleci", "gh/bcbsm", "circleci/abc123", true},
		{"gh to gh", "gh/bcbsm", "gh/bcbsm-new", false},
		{"circleci to circleci", "circleci/old", "circleci/abc123", false},
		{"gh with unknown destination", "gh/bcbsm", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			md := report.MarkdownWith(crossTypeManifest(tc.src), report.Options{DestOrgSlug: tc.dest})
			if got := strings.Contains(md, crossTypeHeading); got != tc.want {
				t.Errorf("checklist present = %v, want %v", got, tc.want)
			}
		})
	}

	// Markdown (no options) never includes it.
	if strings.Contains(report.Markdown(crossTypeManifest("gh/bcbsm")), crossTypeHeading) {
		t.Error("Markdown() without a destination must not include the cross-type checklist")
	}
}

func TestMarkdownWith_CrossTypeChecklist_Content(t *testing.T) {
	md := report.MarkdownWith(crossTypeManifest("gh/bcbsm"), report.Options{DestOrgSlug: "circleci/abc123"})
	start := strings.Index(md, crossTypeHeading)
	if start < 0 {
		t.Fatal("checklist missing")
	}
	section := md[start:]
	if end := strings.Index(section, "\n### 2."); end > 0 {
		section = section[:end]
	}

	// Ordered: export-first precedes the app move, which precedes transfer-plan,
	// which precedes the namespace move.
	order := []string{
		"Export and capture first",
		"Move the GitHub App connection",
		"Transfer the plan",
		"Move the namespace(s)",
		"Recreate runner resource classes",
		"Repoint cloud OIDC trust",
		"IP ranges entitlement",
		"SSO members default to Viewer",
		"Do not build in both orgs",
	}
	prev := -1
	for _, s := range order {
		i := strings.Index(section, s)
		if i < 0 {
			t.Errorf("checklist missing step %q", s)
			continue
		}
		if i < prev {
			t.Errorf("step %q is out of order", s)
		}
		prev = i
	}

	for _, want := range []string{
		"deletes that CircleCI org's GitHub App pipelines and triggers",
		"only ONE CircleCI org",
		"`transfer-plan \"gh/bcbsm\" :github \"abc123\" :circleci`",
		"**Free** plan",
		"plan-share rider",
		"`change-namespace-owner! \"bcbsm\" \"circleci/abc123\"`",
		"never `rename-namespace`",
		"https://oidc.circleci.com/org/<new-org-id>",
		"embeds project UUIDs",
		"Contributor",
		"doubles credit spend",
	} {
		if !strings.Contains(section, want) {
			t.Errorf("checklist missing %q", want)
		}
	}
	// Numbered list.
	if !strings.Contains(section, "\n1. ") || !strings.Contains(section, "\n9. ") {
		t.Error("checklist should be an ordered 1..9 list")
	}
}

func TestMarkdown_OIDCSubAndOrbsNamespaceHandoff(t *testing.T) {
	m := crossTypeManifest("gh/bcbsm")
	m.Source.Org.Settings = &manifest.OrgSettings{
		OrbNamespace: "bcbsm",
		Orbs:         []manifest.OrgOrb{{OrbName: "bcbsm/tool", LatestVersionNumber: "1.0.0"}},
	}
	md := report.Markdown(m)

	for _, want := range []string{
		// OIDC `sub` claim carries project UUIDs.
		"The `sub` claim embeds project UUIDs",
		"org/<org-id>/project/<project-id>/user/<user-id>",
		// Orbs: no longer claims they cannot be exported; documents both paths.
		"change-namespace-owner! \"bcbsm\" \"<dest-org-slug>\"",
		"`sync --dest-orb-namespace <new-ns>`",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report missing %q", want)
		}
	}
	if strings.Contains(md, "orb source YAML is not exportable") {
		t.Error("stale 'orb source YAML is not exportable' claim must be gone")
	}
}

func TestMarkdown_DeploysManualStep(t *testing.T) {
	m := crossTypeManifest("gh/bcbsm")
	if strings.Contains(report.Markdown(m), "Deploys (") {
		t.Error("no Deploys item expected without captured deploys")
	}
	m.Deploys = &manifest.Deploys{
		Environments: []manifest.DeployEnvironment{{Name: "production"}},
		Components:   []manifest.DeployComponent{{Name: "web"}},
	}
	md := report.Markdown(m)
	for _, want := range []string{
		"**Deploys (1 environment(s), 1 component(s), 0 project deploy setting(s))**",
		"the public Deploys API is read-only",
		"Deploys history does not transfer",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report missing %q", want)
		}
	}
}
