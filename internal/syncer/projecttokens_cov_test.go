package syncer

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// Covers the create-error path AND the "SAVE THESE" stderr printing block:
// --create-project-tokens + --apply, with one token creating successfully
// (printed to s.Out) and one returning an error (recorded as "error").
func TestSyncProjectAPITokens_Apply_CreateErrorAndStderrPrint(t *testing.T) {
	var out bytes.Buffer
	fp := &fakeProjectWriter{
		getProject: func(slug string) (*project.Project, error) {
			return &project.Project{Slug: slug, ID: "proj-dst-uuid", Name: "web"}, nil
		},
		listProjectTokens: func(slug string) ([]project.ProjectAPIToken, error) { return nil, nil },
		createProjectToken: func(slug, scope, label string) (string, error) {
			if label == "boom" {
				return "", &fakeErr{"create failed"}
			}
			return "ccipat_PLACEHOLDER_created_value", nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Projects: fp, Out: &out}

	m := projectManifestWithTokens(
		manifest.ProjectAPIToken{Label: "deploy-bot", Scope: "all"},
		manifest.ProjectAPIToken{Label: "boom", Scope: "status"},
	)

	rep, err := sy.SyncProjects(context.Background(), m, nil, mappingTo("gh/dst"), Options{Apply: true, CreateProjectTokens: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var created, errored int
	for _, a := range rep.Actions {
		if a.Kind != "project-api-token" {
			continue
		}
		switch a.Status {
		case "created":
			created++
		case "error":
			errored++
		}
	}
	if created != 1 || errored != 1 {
		t.Fatalf("want 1 created + 1 error, got created=%d errored=%d", created, errored)
	}

	got := out.String()
	if !strings.Contains(got, "SAVE THESE PROJECT API TOKEN VALUES NOW") {
		t.Errorf("stderr should contain the save-now header; got:\n%s", got)
	}
	if !strings.Contains(got, "ccipat_PLACEHOLDER_created_value") {
		t.Errorf("stderr should contain the created token value; got:\n%s", got)
	}
	// The errored token's value must NOT appear.
	if strings.Contains(got, "label: boom") {
		t.Errorf("errored token must not be printed as created; got:\n%s", got)
	}
}

type fakeErr struct{ s string }

func (e *fakeErr) Error() string { return e.s }
