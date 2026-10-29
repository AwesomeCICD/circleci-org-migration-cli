package cmd_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
)

const (
	migSrcOrgUUID = "11111111-1111-1111-1111-111111111111"
	migDstOrgUUID = "22222222-2222-2222-2222-222222222222"
)

// migrateAppServer fakes both orgs of an App → App migration on one host: the
// source holds project web (src-proj) with one definition and trigger; the
// destination already has project web (dst-proj).
//
//	listDefsStatus  — status of GET dst-proj/pipeline-definitions (200 → empty list)
//	enableStatus    — status of PATCH …/triggers/<id> (the enable-builds step)
func migrateAppServer(t *testing.T, listDefsStatus, enableStatus int) *httptest.Server {
	t.Helper()
	const defJSON = `{"id":"d1","name":"default",` +
		`"config_source":{"provider":"github_app","repo":{"full_name":"acme/web","external_id":"1"},"file_path":".circleci/config.yml"},` +
		`"checkout_source":{"provider":"github_app","repo":{"full_name":"acme/web","external_id":"1"}}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, "/api/v2/organization/"):
			_, _ = w.Write([]byte(`{"id":"` + migSrcOrgUUID + `","name":"org","slug":"circleci/org","vcs_type":"circleci"}`))
		case p == "/api/private/project":
			if r.URL.Query().Get("organization-id") == migSrcOrgUUID {
				_, _ = w.Write([]byte(`{"items":[{"id":"src-proj","slug":"circleci/src/web","name":"web"}],"next_page_token":""}`))
			} else {
				_, _ = w.Write([]byte(`{"items":[{"id":"dst-proj","slug":"circleci/dst/dst-proj","name":"web"}],"next_page_token":""}`))
			}
		case p == "/api/v2/project/circleci/src/web":
			_, _ = w.Write([]byte(`{"id":"src-proj","slug":"circleci/src/web","name":"web"}`))
		case p == "/api/v2/projects/src-proj/pipeline-definitions":
			_, _ = w.Write([]byte(`{"items":[` + defJSON + `],"next_page_token":""}`))
		case p == "/api/v2/projects/src-proj/pipeline-definitions/d1/triggers":
			_, _ = w.Write([]byte(`{"items":[{"id":"st1","name":"push","event_preset":"all-pushes","disabled":false,` +
				`"event_source":{"provider":"github_app","repo":{"full_name":"acme/web","external_id":"1"}}}],"next_page_token":""}`))
		case p == "/api/v2/projects/dst-proj/pipeline-definitions" && r.Method == http.MethodGet:
			w.WriteHeader(listDefsStatus)
			if listDefsStatus == http.StatusOK {
				_, _ = w.Write([]byte(`{"items":[],"next_page_token":""}`))
			} else {
				_, _ = w.Write([]byte(`{"message":"definitions unavailable"}`))
			}
		case p == "/api/v2/projects/dst-proj/pipeline-definitions" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"def-new"}`))
		case p == "/api/v2/projects/dst-proj/pipeline-definitions/def-new/triggers" && r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"id":"trig-new"}`))
		case p == "/api/v2/projects/dst-proj/triggers/trig-new" && r.Method == http.MethodPatch:
			w.WriteHeader(enableStatus)
			_, _ = w.Write([]byte(`{"message":"enable refused"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"not found"}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func runMigrateApp(t *testing.T, srv *httptest.Server, extra ...string) (stdout, stderr string, err error) {
	t.Helper()
	root := cmd.MakeCommands()
	var o, e strings.Builder
	root.SetOut(&o)
	root.SetErr(&e)
	args := []string{"migrate",
		"--source-org", "circleci/" + migSrcOrgUUID, "--dest-org", "circleci/" + migDstOrgUUID,
		"--host", srv.URL, "--token", "tok", "--no-input", "--apply", "--yes", "--skip-preflight",
		"--skip-contexts", "--skip-org-settings", "--skip-runner", "--skip-orb", "--skip-ciam"}
	root.SetArgs(append(args, extra...))
	err = root.Execute()
	return o.String(), e.String(), err
}

// TestMigrateCmd_EnableFailure_RunsRemainingStepsThenFails: an enable-builds
// failure no longer returns immediately — the end summary and the post-apply
// validation still run, and the non-zero exit is returned last.
func TestMigrateCmd_EnableFailure_RunsRemainingStepsThenFails(t *testing.T) {
	srv := migrateAppServer(t, http.StatusOK, http.StatusBadRequest)

	stdout, stderr, err := runMigrateApp(t, srv)
	if err == nil || !strings.Contains(err.Error(), "enabling builds failed") {
		t.Fatalf("want the enable failure returned, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
	if !strings.Contains(stdout, "Migration summary") {
		t.Errorf("the end summary must still be printed:\n%s", stdout)
	}
	if !strings.Contains(stderr, "Post-migration validation") {
		t.Errorf("post-apply validation must still run:\n%s", stderr)
	}
	if !strings.Contains(stderr, "trigger trig-new") || !strings.Contains(stderr, `definition "default"`) {
		t.Errorf("each enable must print the trigger id and definition name:\n%s", stderr)
	}
}

// TestMigrateCmd_EnableFailure_JSONStillEmitted: with --json the summary document
// is emitted before the non-zero exit.
func TestMigrateCmd_EnableFailure_JSONStillEmitted(t *testing.T) {
	srv := migrateAppServer(t, http.StatusOK, http.StatusBadRequest)

	stdout, _, err := runMigrateApp(t, srv, "--json")
	if err == nil {
		t.Fatal("an enable failure must exit non-zero")
	}
	if !strings.Contains(stdout, `"sync"`) {
		t.Errorf("--json output must be emitted before the failure is returned:\n%s", stdout)
	}
}

// TestMigrateCmd_PipelineDefError_FailsAfterSummary: a per-item pipeline-definition
// error exits non-zero after the summary and the remaining steps.
func TestMigrateCmd_PipelineDefError_FailsAfterSummary(t *testing.T) {
	srv := migrateAppServer(t, http.StatusBadRequest, http.StatusOK)

	stdout, stderr, err := runMigrateApp(t, srv)
	if err == nil || !strings.Contains(err.Error(), "pipeline definition/trigger") {
		t.Fatalf("want a pipeline-definition failure, got %v", err)
	}
	if !strings.Contains(stdout, "Migration summary") || !strings.Contains(stderr, "Post-migration validation") {
		t.Errorf("summary and validation must still run\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

// TestMigrateCmd_Healthy_ExitsZero: the same flow with a healthy API succeeds.
func TestMigrateCmd_Healthy_ExitsZero(t *testing.T) {
	srv := migrateAppServer(t, http.StatusOK, http.StatusOK)
	if stdout, stderr, err := runMigrateApp(t, srv); err != nil {
		t.Fatalf("healthy migrate must exit 0, got %v\nstdout:\n%s\nstderr:\n%s", err, stdout, stderr)
	}
}
