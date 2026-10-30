package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

const appDest = "circleci/org-uuid/proj-uuid"

func appFlagProject(flags map[string]bool) manifest.Project {
	return manifest.Project{
		Slug:     "gh/acme/web",
		Settings: &manifest.AdvancedSettings{V11FeatureFlags: flags},
	}
}

func flagActions(rep *Report) map[string]Action {
	out := map[string]Action{}
	for _, a := range rep.Actions {
		if a.Kind == "project-flag" {
			out[a.Target] = a
		}
	}
	return out
}

// An App destination must never receive forks-receive-secret-env-vars or oss
// (the monolith 403s the whole PUT) and must report them manual.
func TestSyncProjectV11Flags_App_DropsRejectedFlags(t *testing.T) {
	var sent map[string]bool
	fp := &fakeProjectWriter{setV11ProjectFeatureFlags: func(_ string, f map[string]bool) error { sent = f; return nil }}
	s := &Syncer{Projects: fp}
	rep := &Report{}
	p := appFlagProject(map[string]bool{
		"forks-receive-secret-env-vars": true,
		"oss":                           true,
		"build-prs-only":                true,
		"build-fork-prs":                false, // default: silent
		"set-github-status":             true,  // default: silent
		"disable-ssh":                   true,
		"api-trigger-with-config":       true,
		"setup-workflows":               true,
	})
	s.syncProjectV11Flags(context.Background(), rep, p, appDest, Options{Apply: true})

	for _, bad := range []string{"forks_receive_secret_env_vars", "oss", "build_prs_only", "build_fork_prs", "set_github_status"} {
		if _, ok := sent[bad]; ok {
			t.Errorf("flag %q must not be sent to an App project", bad)
		}
	}
	for _, good := range []string{"disable_ssh", "api_trigger_with_config", "setup_workflows"} {
		if !sent[good] {
			t.Errorf("flag %q should be sent; got %v", good, sent)
		}
	}
	acts := flagActions(rep)
	for _, k := range []string{"forks_receive_secret_env_vars", "oss", "build_prs_only"} {
		a, ok := acts[appDest+"/feature_flag:"+k]
		if !ok || a.Status != "manual" || a.Detail == "" {
			t.Errorf("expected manual action with reason for %s, got %+v", k, a)
		}
	}
	for _, k := range []string{"build_fork_prs", "set_github_status"} {
		if _, ok := acts[appDest+"/feature_flag:"+k]; ok {
			t.Errorf("flag %s at its default must not be reported", k)
		}
	}
	if !strings.Contains(acts[appDest+"/feature_flag:forks_receive_secret_env_vars"].Detail, "403") {
		t.Error("the reason for forks_receive_secret_env_vars should mention the 403")
	}
}

// drop_all_build_requests is sent to an App project only with --include-danger-flags.
func TestSyncProjectV11Flags_App_DangerFlag(t *testing.T) {
	var sent map[string]bool
	fp := &fakeProjectWriter{setV11ProjectFeatureFlags: func(_ string, f map[string]bool) error { sent = f; return nil }}
	s := &Syncer{Projects: fp}
	p := appFlagProject(map[string]bool{"drop-all-build-requests": true})

	s.syncProjectV11Flags(context.Background(), &Report{}, p, appDest, Options{Apply: true})
	if sent != nil {
		t.Fatalf("danger flag sent without --include-danger-flags: %v", sent)
	}
	s.syncProjectV11Flags(context.Background(), &Report{}, p, appDest, Options{Apply: true, IncludeDangerFlags: true})
	if !sent["drop_all_build_requests"] {
		t.Fatalf("danger flag should be sent with IncludeDangerFlags; got %v", sent)
	}
}

// Flags that already match the destination are not re-sent.
func TestSyncProjectV11Flags_SkipsFlagsAlreadyAtSourceValue(t *testing.T) {
	var sent map[string]bool
	fp := &fakeProjectWriter{
		getV11ProjectFeatureFlags: func(string) (map[string]bool, error) {
			return map[string]bool{"disable-ssh": true, "autocancel-builds": false}, nil
		},
		setV11ProjectFeatureFlags: func(_ string, f map[string]bool) error { sent = f; return nil },
	}
	s := &Syncer{Projects: fp}
	rep := &Report{}
	p := appFlagProject(map[string]bool{"disable-ssh": true, "autocancel-builds": true})
	s.syncProjectV11Flags(context.Background(), rep, p, appDest, Options{Apply: true})

	if _, ok := sent["disable_ssh"]; ok {
		t.Error("disable_ssh already matches and must not be sent")
	}
	if !sent["autocancel_builds"] {
		t.Errorf("autocancel_builds differs and must be sent; got %v", sent)
	}
	found := false
	for _, a := range rep.Actions {
		if a.Status == "exists" && strings.Contains(a.Detail, "disable_ssh") {
			found = true
		}
	}
	if !found {
		t.Error("expected an 'exists' action naming disable_ssh")
	}
}

// When the combined PUT fails, each flag is retried alone and the offender named.
func TestSyncProjectV11Flags_RetriesPerFlagToIsolateOffender(t *testing.T) {
	calls := 0
	fp := &fakeProjectWriter{setV11ProjectFeatureFlags: func(_ string, f map[string]bool) error {
		calls++
		if len(f) > 1 || f["autocancel_builds"] {
			return errors.New("HTTP 403 Forbidden with an empty response body")
		}
		return nil
	}}
	s := &Syncer{Projects: fp}
	rep := &Report{}
	p := appFlagProject(map[string]bool{"disable-ssh": true, "autocancel-builds": true})
	s.syncProjectV11Flags(context.Background(), rep, p, "gh/acme/web", Options{Apply: true})

	acts := flagActions(rep)
	bad := acts["gh/acme/web/feature_flag:autocancel_builds"]
	if bad.Status != "error" || !strings.Contains(bad.Detail, "autocancel_builds") || !strings.Contains(bad.Detail, "403") {
		t.Errorf("offender not isolated: %+v", bad)
	}
	if good := acts["gh/acme/web/feature_flag:disable_ssh"]; good.Status != "set" {
		t.Errorf("disable_ssh should still be applied: %+v", good)
	}
	if calls != 3 {
		t.Errorf("expected 1 combined + 2 individual PUTs, got %d", calls)
	}
}

// Dry run on a not-yet-created App project reads nothing and shows only the sendable flags.
func TestSyncProjectV11Flags_App_DryRunNewProject(t *testing.T) {
	fp := &fakeProjectWriter{}
	s := &Syncer{Projects: fp}
	rep := &Report{}
	p := appFlagProject(map[string]bool{"disable-ssh": true, "forks-receive-secret-env-vars": true})
	s.syncProjectV11Flags(context.Background(), rep, p, "circleci/org-uuid/<new>", Options{})
	if fp.hasCalled("GetV11ProjectFeatureFlags") || fp.hasCalled("SetV11ProjectFeatureFlags") {
		t.Error("a dry run on a project that does not exist must not call the API")
	}
	acts := flagActions(rep)
	if acts["circleci/org-uuid/<new>/feature_flag:disable_ssh"].Status != "set" ||
		acts["circleci/org-uuid/<new>/feature_flag:forks_receive_secret_env_vars"].Status != "manual" {
		t.Errorf("unexpected dry-run actions: %+v", acts)
	}
}
