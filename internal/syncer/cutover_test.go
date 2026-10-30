package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/rest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---- fakes -----------------------------------------------------------------

type fakeCutSource struct {
	flags     map[string]map[string]bool // slug -> kebab flag -> value
	defs      map[string][]project.PipelineDefinition
	triggers  map[string][]project.Trigger // by def id
	setErr    error
	toggleErr error

	flagSets []string // "slug:flag=value"
	toggles  []string // "triggerID=disabled"
}

func (f *fakeCutSource) GetProject(_ context.Context, slug string) (*project.Project, error) {
	return &project.Project{Slug: slug, ID: "id-of-" + slug}, nil
}
func (f *fakeCutSource) GetV11ProjectFeatureFlags(_ context.Context, slug string) (map[string]bool, error) {
	out := map[string]bool{}
	for k, v := range f.flags[slug] {
		out[k] = v
	}
	return out, nil
}
func (f *fakeCutSource) SetV11ProjectFeatureFlags(_ context.Context, slug string, flags map[string]bool) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.flags[slug] == nil {
		f.flags[slug] = map[string]bool{}
	}
	for k, v := range flags {
		f.flags[slug][strings.ReplaceAll(k, "_", "-")] = v
		f.flagSets = append(f.flagSets, slug+":"+k+"="+boolStr(v))
	}
	return nil
}
func (f *fakeCutSource) ListPipelineDefinitions(_ context.Context, projectID string) ([]project.PipelineDefinition, error) {
	return f.defs[projectID], nil
}
func (f *fakeCutSource) ListTriggers(_ context.Context, _, defID string) ([]project.Trigger, error) {
	return f.triggers[defID], nil
}
func (f *fakeCutSource) SetTriggerDisabled(_ context.Context, _, triggerID string, disabled bool) error {
	if f.toggleErr != nil {
		return f.toggleErr
	}
	f.toggles = append(f.toggles, triggerID+"="+boolStr(disabled))
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

type fakeOrgFlagsC struct {
	flags  map[string]bool
	sets   []string
	updErr error
}

func (f *fakeOrgFlagsC) GetFeatureFlags(context.Context, string, string) (map[string]bool, error) {
	return f.flags, nil
}
func (f *fakeOrgFlagsC) UpdateFeatureFlags(_ context.Context, vcs, name string, fl map[string]bool) error {
	if f.updErr != nil {
		return f.updErr
	}
	for k, v := range fl {
		f.flags[k] = v
		f.sets = append(f.sets, vcs+"/"+name+":"+k+"="+boolStr(v))
	}
	return nil
}

type fakeToggler struct {
	calls []string
	err   error
}

func (f *fakeToggler) SetTriggerDisabled(_ context.Context, _, triggerID string, disabled bool) error {
	if f.err != nil {
		return f.err
	}
	f.calls = append(f.calls, triggerID+"="+boolStr(disabled))
	return nil
}

// ---- fixtures --------------------------------------------------------------

func cutoverManifest() *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/acme"}},
		Projects: []manifest.Project{
			{Slug: "gh/acme/web", Name: "web", SourceID: "src-web"},
			{Slug: "gh/acme/api", Name: "api", SourceID: "src-api", Settings: &manifest.AdvancedSettings{}},
		},
	}
}

func cutoverMapping() *manifest.Mapping {
	return &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: "circleci/dest"}}
}

func newCutSource() *fakeCutSource {
	return &fakeCutSource{
		flags: map[string]map[string]bool{
			"gh/acme/web": {"drop-all-build-requests": false},
			"gh/acme/api": {"drop-all-build-requests": true}, // already paused by someone else
		},
		defs: map[string][]project.PipelineDefinition{"src-web": {{ID: "sdef", Name: "build"}}},
		triggers: map[string][]project.Trigger{"sdef": {
			{ID: "t-app", EventSource: project.TriggerEventSource{Provider: "github_app"}},
			{ID: "t-oauth", EventSource: project.TriggerEventSource{Provider: "github_oauth"}},
			{ID: "t-off", Disabled: true, EventSource: project.TriggerEventSource{Provider: "github_app"}},
		}},
	}
}

func statePath(t *testing.T) string { return filepath.Join(t.TempDir(), "cutover-state.json") }

func actionStatus(rep *Report, kind, targetSub string) string {
	for _, a := range rep.Actions {
		if a.Kind == kind && strings.Contains(a.Target, targetSub) {
			return a.Status
		}
	}
	return ""
}

func readState(t *testing.T, path string) CutoverState {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var st CutoverState
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// ---- pause source ----------------------------------------------------------

func TestCutover_PauseSource_DryRunChangesNothing(t *testing.T) {
	src := newCutSource()
	sp := statePath(t)
	rep, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{PauseSource: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.flagSets)+len(src.toggles) != 0 {
		t.Errorf("dry run wrote: %v %v", src.flagSets, src.toggles)
	}
	if _, err := os.Stat(sp); err == nil {
		t.Error("dry run must not create the state file")
	}
	if actionStatus(rep, kindCutoverSourceFlag, "gh/acme/web") != "set" {
		t.Errorf("web should be planned: %+v", rep.Actions)
	}
}

func TestCutover_PauseSource_ApplyRecordsStateAndIsIdempotent(t *testing.T) {
	src := newCutSource()
	sp := statePath(t)
	c := &Cutover{Source: src}
	rep, err := c.Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(src.flagSets, ","); got != "gh/acme/web:drop_all_build_requests=true" {
		t.Errorf("flag writes = %q (api was already paused and must not be rewritten)", got)
	}
	if got := strings.Join(src.toggles, ","); got != "t-app=true" {
		t.Errorf("only the enabled non-oauth trigger is disabled; got %q", got)
	}
	if actionStatus(rep, kindCutoverSourceFlag, "gh/acme/api") != "exists" {
		t.Errorf("already-paused project should be exists: %+v", rep.Actions)
	}

	st := readState(t, sp)
	if len(st.SourceFlags) != 2 || len(st.SourceTriggers) != 1 || st.SourceTriggers[0].TriggerID != "t-app" || st.SourceTriggers[0].ProjectID != "src-web" {
		t.Fatalf("state = %+v", st)
	}
	for _, r := range st.SourceFlags {
		wantPrior := r.Project == "gh/acme/api" // paused before us: undo must leave it paused
		if r.Prior != wantPrior {
			t.Errorf("prior for %s = %v, want %v", r.Project, r.Prior, wantPrior)
		}
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(sp); fi.Mode().Perm() != 0o600 {
			t.Errorf("state mode = %v", fi.Mode().Perm())
		}
	}

	// Re-run: the trigger is now disabled in the fake? simulate server state.
	src.triggers["sdef"][0].Disabled = true
	src.flagSets, src.toggles = nil, nil
	rep, err = c.Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.flagSets)+len(src.toggles) != 0 {
		t.Errorf("second run must change nothing: %v %v", src.flagSets, src.toggles)
	}
	if actionStatus(rep, kindCutoverSourceFlag, "gh/acme/web") != "exists" || actionStatus(rep, kindCutoverSourceTrigger, "t-app") != "exists" {
		t.Errorf("second run should report exists: %+v", rep.Actions)
	}
	if st2 := readState(t, sp); len(st2.SourceFlags) != 2 || len(st2.SourceTriggers) != 1 {
		t.Errorf("state must not grow on re-run: %+v", st2)
	}
}

// A failed mutation keeps its state record (a 5xx/timeout may have applied the
// change; undo is idempotent), except a definite 4xx rejection.
func TestCutover_PauseSource_FailureKeepsRecordUnlessRejected(t *testing.T) {
	for _, tc := range []struct {
		name     string
		err      error
		wantKept bool
	}{
		{"5xx may have landed", &rest.HTTPError{Code: 500, Message: "boom"}, true},
		{"plain/timeout error may have landed", errors.New("context deadline exceeded"), true},
		{"408 may have landed", &rest.HTTPError{Code: 408}, true},
		{"4xx definitely refused", &rest.HTTPError{Code: 422, Message: "nope"}, false},
		{"403 definitely refused", &rest.HTTPError{Code: 403}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := newCutSource()
			src.setErr, src.toggleErr = tc.err, tc.err
			sp := statePath(t)
			rep, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp})
			if err != nil {
				t.Fatal(err)
			}
			if rep.Counts()["error"] != 2 {
				t.Errorf("want 2 errors (flag + trigger), got %+v", rep.Actions)
			}
			st := readState(t, sp)
			flagKept := false
			for _, r := range st.SourceFlags {
				if r.Project == "gh/acme/web" {
					flagKept = true
				}
			}
			if flagKept != tc.wantKept {
				t.Errorf("flag record kept = %v, want %v: %+v", flagKept, tc.wantKept, st.SourceFlags)
			}
			if kept := len(st.SourceTriggers) == 1; kept != tc.wantKept {
				t.Errorf("trigger record kept = %v, want %v: %+v", kept, tc.wantKept, st.SourceTriggers)
			}
		})
	}
}

// A kept record is harmless: undo against a change that never landed is a no-op
// restore and clears the record.
func TestCutover_PauseSource_KeptRecordUndoesCleanly(t *testing.T) {
	src := newCutSource()
	src.setErr, src.toggleErr = &rest.HTTPError{Code: 502}, &rest.HTTPError{Code: 502}
	sp := statePath(t)
	c := &Cutover{Source: src}
	if _, err := c.Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp}); err != nil {
		t.Fatal(err)
	}
	src.setErr, src.toggleErr = nil, nil
	rep, err := c.Run(context.Background(), nil, nil, CutoverOptions{Undo: true, Apply: true, StatePath: sp})
	if err != nil || rep.Counts()["error"] != 0 {
		t.Fatalf("undo: %v %+v", err, rep.Actions)
	}
	if after := readState(t, sp); !after.Empty() {
		t.Errorf("state should be empty after undo: %+v", after)
	}
}

func TestCutover_PauseSource_AppSourceProjectUsesTriggersOnly(t *testing.T) {
	src := newCutSource()
	m := &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "circleci/src"}},
		Projects: []manifest.Project{{Slug: "circleci/src/p-uuid", Name: "p", SourceID: "src-web"}}}
	_, err := (&Cutover{Source: src}).Run(context.Background(), m, cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: statePath(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(src.flagSets) != 0 {
		t.Errorf("an App source project must not get the v1.1 flag: %v", src.flagSets)
	}
	if len(src.toggles) != 1 {
		t.Errorf("toggles = %v", src.toggles)
	}
}

func TestCutover_PauseOrg(t *testing.T) {
	of := &fakeOrgFlagsC{flags: map[string]bool{}}
	sp := statePath(t)
	c := &Cutover{Source: newCutSource(), SourceOrg: of}
	rep, err := c.Run(context.Background(), &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}}}, cutoverMapping(),
		CutoverOptions{Apply: true, PauseOrg: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if len(of.sets) != 1 || of.sets[0] != "github/acme:drop_all_build_requests=true" {
		t.Errorf("org writes = %v", of.sets)
	}
	if st := readState(t, sp); st.OrgFlag == nil || st.OrgFlag.Prior {
		t.Errorf("state = %+v", st)
	}
	if actionStatus(rep, kindCutoverOrgFlag, "gh/acme") != "set" {
		t.Errorf("actions = %+v", rep.Actions)
	}
	// Re-run: already paused.
	of.sets = nil
	if _, err := c.Run(context.Background(), &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}}}, cutoverMapping(),
		CutoverOptions{Apply: true, PauseOrg: true, StatePath: sp}); err != nil || len(of.sets) != 0 {
		t.Errorf("re-run wrote %v (err %v)", of.sets, err)
	}
}

func TestCutover_UnwritableStateStopsBeforeChanging(t *testing.T) {
	src := newCutSource()
	bad := filepath.Join(t.TempDir(), "no", "such", "dir", "state.json")
	rep, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: bad})
	if err == nil {
		t.Fatal("expected an error")
	}
	if len(src.flagSets)+len(src.toggles) != 0 {
		t.Errorf("nothing may change when the state cannot be saved: %v %v", src.flagSets, src.toggles)
	}
	if rep.Counts()["error"] == 0 {
		t.Error("the failure should be reported")
	}
}

// ---- enable destination ----------------------------------------------------

func destFixtures(destDisabled map[string]bool) (*Syncer, *fakeProjectWriter) {
	fp := &fakeProjectWriter{}
	fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
		return []project.OrgProject{{ID: "dest-web", Name: "web"}}, nil // "api" missing
	}
	fp.listPipelineDefinitions = func(string) ([]project.PipelineDefinition, error) {
		return []project.PipelineDefinition{{ID: "ddef", Name: "build"}}, nil
	}
	fp.listTriggers = func(string, string) ([]project.Trigger, error) {
		return []project.Trigger{
			{ID: "d-push", EventPreset: "all-pushes", Disabled: destDisabled["d-push"], EventSource: project.TriggerEventSource{Provider: "github_app"}},
			{ID: "d-pr", EventPreset: "only-pull-requests", Disabled: destDisabled["d-pr"], EventSource: project.TriggerEventSource{Provider: "github_app"}},
		}, nil
	}
	return &Syncer{Org: &fakeOrgResolver{}, Projects: fp}, fp
}

func enableManifest() *manifest.Manifest {
	trig := func(name, preset string, disabled bool) manifest.Trigger {
		return manifest.Trigger{Name: name, EventPreset: preset, Disabled: disabled, EventSource: manifest.TriggerEventSource{Provider: "github_app"}}
	}
	return &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}},
		Projects: []manifest.Project{
			{Slug: "circleci/acme/web-uuid", Name: "web", PipelineDefinitions: []manifest.PipelineDefinition{{
				Name:     "build",
				Triggers: []manifest.Trigger{trig("push", "all-pushes", false), trig("pr", "only-pull-requests", true)},
			}}},
			{Slug: "circleci/acme/api-uuid", Name: "api", PipelineDefinitions: []manifest.PipelineDefinition{{Name: "build", Triggers: []manifest.Trigger{trig("push", "all-pushes", false)}}}},
		},
	}
}

func TestCutover_EnableDest_EnablesOnlySourceEnabledTriggers(t *testing.T) {
	sy, fp := destFixtures(map[string]bool{"d-push": true, "d-pr": true})
	sp := statePath(t)
	rep, err := (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	enabled := fp.callsTo("EnableTrigger")
	if len(enabled) != 1 || enabled[0].args[1] != "d-push" {
		t.Fatalf("only the source-enabled trigger may be enabled: %+v", enabled)
	}
	if actionStatus(rep, kindCutoverDestTrigger, "trigger:pr") != "skipped" {
		t.Errorf("source-disabled trigger must be skipped: %+v", rep.Actions)
	}
	if actionStatus(rep, kindCutoverDestTrigger, "api") != "manual" {
		t.Errorf("project missing in the destination should be manual: %+v", rep.Actions)
	}
	if st := readState(t, sp); len(st.DestTriggers) != 1 || st.DestTriggers[0].TriggerID != "d-push" || st.DestTriggers[0].ProjectID != "dest-web" {
		t.Errorf("state = %+v", st)
	}

	// Idempotent: now enabled in the destination.
	sy2, fp2 := destFixtures(map[string]bool{"d-pr": true})
	rep, err = (&Cutover{Dest: sy2}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: sp})
	if err != nil || fp2.hasCalled("EnableTrigger") {
		t.Fatalf("re-run must not enable again (err %v)", err)
	}
	if actionStatus(rep, kindCutoverDestTrigger, "d-push") != "exists" {
		t.Errorf("want exists: %+v", rep.Actions)
	}
	if st := readState(t, sp); len(st.DestTriggers) != 1 {
		t.Errorf("state must keep the earlier record: %+v", st)
	}
}

func TestCutover_EnableDest_DryRunAndFailure(t *testing.T) {
	sy, fp := destFixtures(map[string]bool{"d-push": true})
	sp := statePath(t)
	rep, err := (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{EnableDest: true, StatePath: sp})
	if err != nil || fp.hasCalled("EnableTrigger") {
		t.Fatalf("dry run enabled something (err %v)", err)
	}
	if a := actionStatus(rep, kindCutoverDestTrigger, "d-push"); a != "set" {
		t.Errorf("dry run should plan it: %+v", rep.Actions)
	}
	if _, serr := os.Stat(sp); serr == nil {
		t.Error("dry run must not write state")
	}

	sy, fp = destFixtures(map[string]bool{"d-push": true})
	fp.enableTrigger = func(string, string) error { return errors.New("HTTP 500") }
	rep, _ = (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: sp})
	if rep.Counts()["error"] != 1 {
		t.Errorf("failed enable should be an error: %+v", rep.Actions)
	}
	if st := readState(t, sp); len(st.DestTriggers) != 1 {
		t.Errorf("a failed enable (5xx) may have landed, so its record stays: %+v", st)
	}

	// A definite 4xx rejection means it did not land: the record is dropped.
	sy, fp = destFixtures(map[string]bool{"d-push": true})
	fp.enableTrigger = func(string, string) error { return &rest.HTTPError{Code: 403} }
	sp2 := statePath(t)
	_, _ = (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: sp2})
	if st := readState(t, sp2); len(st.DestTriggers) != 0 {
		t.Errorf("a rejected enable must not stay recorded: %+v", st)
	}
}

func TestCutover_EnableDest_NeedsDestinationOrg(t *testing.T) {
	sy, _ := destFixtures(nil)
	_, err := (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), &manifest.Mapping{}, CutoverOptions{EnableDest: true, StatePath: statePath(t)})
	if err == nil || !strings.Contains(err.Error(), "org.to") {
		t.Errorf("err = %v", err)
	}
}

// ---- undo ------------------------------------------------------------------

func TestCutover_Undo_RestoresEverything(t *testing.T) {
	sp := statePath(t)
	st := CutoverState{Version: 1,
		SourceFlags: []CutoverFlagRecord{
			{Project: "gh/acme/web", Flag: cutoverFlag, Prior: false},
			{Project: "gh/acme/api", Flag: cutoverFlag, Prior: true},
		},
		OrgFlag:        &CutoverOrgFlagRecord{Org: "gh/acme", Flag: cutoverFlag, Prior: false},
		SourceTriggers: []CutoverTriggerRecord{{Project: "gh/acme/web", ProjectID: "src-web", Definition: "build", TriggerID: "t-app"}},
		DestTriggers:   []CutoverTriggerRecord{{Project: "web", ProjectID: "dest-web", Definition: "build", TriggerID: "d-push"}},
	}
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	src := &fakeCutSource{flags: map[string]map[string]bool{
		"gh/acme/web": {"drop-all-build-requests": true},
		"gh/acme/api": {"drop-all-build-requests": true},
	}}
	of := &fakeOrgFlagsC{flags: map[string]bool{cutoverFlag: true}}
	dt := &fakeToggler{}
	c := &Cutover{Source: src, SourceOrg: of, DestToggle: dt}

	// Dry run first: nothing changes, state untouched.
	if _, err := c.Run(context.Background(), nil, nil, CutoverOptions{Undo: true, StatePath: sp}); err != nil {
		t.Fatal(err)
	}
	if len(src.flagSets)+len(src.toggles)+len(of.sets)+len(dt.calls) != 0 {
		t.Fatal("dry-run undo changed something")
	}

	rep, err := c.Run(context.Background(), nil, nil, CutoverOptions{Undo: true, Apply: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(src.flagSets, ","); got != "gh/acme/web:drop_all_build_requests=false" {
		t.Errorf("only web differs from its prior value; writes = %q", got)
	}
	if got := strings.Join(src.toggles, ","); got != "t-app=false" {
		t.Errorf("source trigger must be re-enabled; got %q", got)
	}
	if got := strings.Join(dt.calls, ","); got != "d-push=true" {
		t.Errorf("destination trigger must be disabled; got %q", got)
	}
	if len(of.sets) != 1 || !strings.HasSuffix(of.sets[0], "=false") {
		t.Errorf("org flag writes = %v", of.sets)
	}
	if after := readState(t, sp); !after.Empty() {
		t.Errorf("state should be empty after a successful undo: %+v", after)
	}
	if rep.Counts()["error"] != 0 {
		t.Errorf("unexpected errors: %+v", rep.Actions)
	}

	// Second undo is a no-op.
	rep, err = c.Run(context.Background(), nil, nil, CutoverOptions{Undo: true, Apply: true, StatePath: sp})
	if err != nil || len(rep.Actions) != 1 || rep.Actions[0].Status != "exists" {
		t.Errorf("second undo: %+v err %v", rep.Actions, err)
	}
}

func TestCutover_Undo_GoneSourceTriggerIsManualAndFailuresStayRecorded(t *testing.T) {
	sp := statePath(t)
	st := CutoverState{Version: 1,
		SourceTriggers: []CutoverTriggerRecord{{Project: "p", ProjectID: "src", Definition: "d", TriggerID: "gone"}},
		DestTriggers:   []CutoverTriggerRecord{{Project: "p", ProjectID: "dst", Definition: "d", TriggerID: "stuck"}},
	}
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	src := &fakeCutSource{toggleErr: &rest.HTTPError{Code: 404, Message: "not found"}}
	dt := &fakeToggler{err: errors.New("HTTP 500")}
	rep, err := (&Cutover{Source: src, DestToggle: dt}).Run(context.Background(), nil, nil, CutoverOptions{Undo: true, Apply: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if actionStatus(rep, kindCutoverSourceTrigger, "gone") != "manual" {
		t.Errorf("404 on a source trigger should be manual: %+v", rep.Actions)
	}
	if actionStatus(rep, kindCutoverDestTrigger, "stuck") != "error" {
		t.Errorf("failed restore should be an error: %+v", rep.Actions)
	}
	got := readState(t, sp)
	if len(got.SourceTriggers) != 0 || len(got.DestTriggers) != 1 || got.DestTriggers[0].TriggerID != "stuck" {
		t.Errorf("only the failed item stays recorded: %+v", got)
	}
}

func TestCutover_PauseOrg_FailureKeepsRecordUnlessRejected(t *testing.T) {
	orgM := &manifest.Manifest{Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}}}
	for _, tc := range []struct {
		err      error
		wantKept bool
	}{{&rest.HTTPError{Code: 503}, true}, {&rest.HTTPError{Code: 400}, false}} {
		of := &fakeOrgFlagsC{flags: map[string]bool{}, updErr: tc.err}
		sp := statePath(t)
		rep, err := (&Cutover{Source: newCutSource(), SourceOrg: of}).Run(context.Background(), orgM, cutoverMapping(), CutoverOptions{Apply: true, PauseOrg: true, StatePath: sp})
		if err != nil || rep.Counts()["error"] != 1 {
			t.Fatalf("err=%v actions=%+v", err, rep.Actions)
		}
		if kept := readState(t, sp).OrgFlag != nil; kept != tc.wantKept {
			t.Errorf("err %v: org record kept = %v, want %v", tc.err, kept, tc.wantKept)
		}
	}
}

// ---- pause failure blocks enable, state guards, atomic writes ----------------

// A failed pause step must stop --enable-dest: otherwise builds could run in
// both orgs. The run returns an error (non-zero exit) and enables nothing.
func TestCutover_FailedPauseSkipsEnableDest(t *testing.T) {
	src := newCutSource()
	src.toggleErr = &rest.HTTPError{Code: 500}
	sy, fp := destFixtures(map[string]bool{"d-push": true})
	sp := statePath(t)
	rep, err := (&Cutover{Source: src, Dest: sy}).Run(context.Background(), enableManifestWithSource(), cutoverMapping(),
		CutoverOptions{Apply: true, PauseSource: true, EnableDest: true, StatePath: sp})
	if err == nil || !strings.Contains(err.Error(), "--enable-dest was NOT run") {
		t.Fatalf("err = %v, want the enable-dest-skipped error", err)
	}
	if fp.hasCalled("EnableTrigger") || fp.hasCalled("ListOrgProjects") {
		t.Error("nothing may be enabled (or even listed) after a failed pause")
	}
	if actionStatus(rep, kindCutoverDestTrigger, "(all destination triggers)") != "error" {
		t.Errorf("the skip must be reported as an error: %+v", rep.Actions)
	}

	// Dry run behaves the same (a read failure of the source counts too).
	_, err = (&Cutover{Source: src, Dest: sy}).Run(context.Background(), enableManifestWithSource(), cutoverMapping(),
		CutoverOptions{PauseSource: true, EnableDest: true, StatePath: statePath(t)})
	if err != nil {
		t.Fatalf("a dry run plans without touching the source, so nothing failed: %v", err)
	}
}

// With a healthy pause, --pause-source --enable-dest still enables.
func TestCutover_PauseThenEnableDest(t *testing.T) {
	src := newCutSource()
	sy, fp := destFixtures(map[string]bool{"d-push": true})
	_, err := (&Cutover{Source: src, Dest: sy}).Run(context.Background(), enableManifestWithSource(), cutoverMapping(),
		CutoverOptions{Apply: true, PauseSource: true, EnableDest: true, StatePath: statePath(t)})
	if err != nil {
		t.Fatal(err)
	}
	if len(fp.callsTo("EnableTrigger")) != 1 {
		t.Errorf("destination trigger should be enabled after a clean pause")
	}
}

// enableManifestWithSource is enableManifest with SourceIDs so pause-source also
// finds source triggers.
func enableManifestWithSource() *manifest.Manifest {
	m := enableManifest()
	m.Source.Org.Slug = "gh/acme"
	m.Projects[0].Slug = "gh/acme/web"
	m.Projects[0].SourceID = "src-web"
	m.Projects = m.Projects[:1]
	return m
}

func TestCutover_UndoRequiresExistingStateFile(t *testing.T) {
	sp := statePath(t)
	_, err := (&Cutover{Source: newCutSource()}).Run(context.Background(), nil, nil, CutoverOptions{Undo: true, Apply: true, StatePath: sp})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want a missing-state error", err)
	}
	if _, serr := os.Stat(sp); serr == nil {
		t.Error("a failed undo must not create the state file")
	}
}

func TestCutover_RefusesStateFromAnotherOrg(t *testing.T) {
	sp := statePath(t)
	st := CutoverState{Version: 1, SourceOrg: "gh/other", DestOrg: "circleci/dest",
		SourceTriggers: []CutoverTriggerRecord{{Project: "gh/other/x", ProjectID: "p", TriggerID: "t"}}}
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	src := newCutSource()
	_, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp})
	if err == nil || !strings.Contains(err.Error(), "gh/other") || !strings.Contains(err.Error(), "gh/acme") {
		t.Fatalf("err = %v, want a source-org mismatch error", err)
	}
	if len(src.flagSets)+len(src.toggles) != 0 {
		t.Error("nothing may change when the state belongs to another org")
	}

	// Matching source, different destination: refused for --enable-dest.
	st.SourceOrg = "gh/acme"
	st.DestOrg = "circleci/elsewhere"
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	sy, fp := destFixtures(map[string]bool{"d-push": true})
	_, err = (&Cutover{Dest: sy}).Run(context.Background(), enableManifest(), cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: sp})
	if err == nil || !strings.Contains(err.Error(), "destination org") {
		t.Fatalf("err = %v, want a destination-org mismatch error", err)
	}
	if fp.hasCalled("EnableTrigger") {
		t.Error("nothing may be enabled")
	}

	// --undo with a manifest checks it too; without one it proceeds.
	st.SourceOrg, st.DestOrg = "gh/other", ""
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), nil, CutoverOptions{Undo: true, StatePath: sp}); err == nil {
		t.Error("undo with a mismatching manifest must be refused")
	}
	if _, err := (&Cutover{Source: src}).Run(context.Background(), nil, nil, CutoverOptions{Undo: true, StatePath: sp}); err != nil {
		t.Errorf("undo without a manifest proceeds: %v", err)
	}
}

// An empty state of another org is not a conflict (nothing to misapply).
func TestCutover_EmptyStateOfAnotherOrgIsFine(t *testing.T) {
	sp := statePath(t)
	if err := (&CutoverState{Version: 1, SourceOrg: "gh/other"}).Save(sp); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Cutover{Source: newCutSource()}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{PauseSource: true, StatePath: sp}); err != nil {
		t.Fatal(err)
	}
}

// A pause-only run must not stamp the destination org (it would later block an
// --enable-dest run that supplies the real destination).
func TestCutover_PauseOnlyDoesNotRecordDestOrg(t *testing.T) {
	sp := statePath(t)
	if _, err := (&Cutover{Source: newCutSource()}).Run(context.Background(), cutoverManifest(), nil, CutoverOptions{Apply: true, PauseSource: true, StatePath: sp}); err != nil {
		t.Fatal(err)
	}
	if st := readState(t, sp); st.DestOrg != "" || st.SourceOrg != "gh/acme" {
		t.Errorf("state orgs = %q/%q", st.SourceOrg, st.DestOrg)
	}
}

func TestCutoverState_SaveIsAtomicAnd0600(t *testing.T) {
	dir := t.TempDir()
	sp := filepath.Join(dir, "state.json")
	if err := os.WriteFile(sp, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	st := &CutoverState{Version: 1, SourceOrg: "gh/acme"}
	if err := st.Save(sp); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(sp); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("no temp file may be left behind: %v", entries)
	}
	if got := readState(t, sp); got.SourceOrg != "gh/acme" {
		t.Errorf("state = %+v", got)
	}
}

// A failed write (unwritable directory) leaves the previous file intact.
func TestWriteSecretFile_FailureLeavesOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("directory permissions differ on windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f.json")
	if err := writeSecretFile(p, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chmod(dir, 0o700) }()
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if err := writeSecretFile(p, []byte("new")); err == nil {
		t.Fatal("expected an error")
	}
	if got, _ := os.ReadFile(p); string(got) != "old" {
		t.Errorf("old content must survive a failed write, got %q", got)
	}
}

// ---- enable-dest trigger matching and project-level pause --------------------

func TestMatchCutoverTriggers(t *testing.T) {
	gh := func(name, preset string, disabled bool) manifest.Trigger {
		return manifest.Trigger{Name: name, EventPreset: preset, Disabled: disabled, EventSource: manifest.TriggerEventSource{Provider: "github_app"}}
	}
	dt := func(id, name, preset string) project.Trigger {
		return project.Trigger{ID: id, Name: name, EventPreset: preset, EventSource: project.TriggerEventSource{Provider: "github_app"}}
	}
	prov := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = "github_app"
		}
		return out
	}

	t.Run("name wins over position when fields are identical", func(t *testing.T) {
		src := []manifest.Trigger{gh("deploy", "all-pushes", false), gh("nightly", "all-pushes", true)}
		dest := []project.Trigger{dt("d-nightly", "nightly", "all-pushes"), dt("d-deploy", "deploy", "all-pushes")}
		got := matchCutoverTriggers(src, prov(2), dest)
		if got[0] != 1 || got[1] != 0 {
			t.Errorf("match = %v, want [1 0] (by name)", got)
		}
	})
	t.Run("identical triggers with different enabled state and no name are ambiguous", func(t *testing.T) {
		src := []manifest.Trigger{gh("", "all-pushes", false), gh("", "all-pushes", true)}
		dest := []project.Trigger{dt("d1", "x", "all-pushes"), dt("d2", "y", "all-pushes")}
		got := matchCutoverTriggers(src, prov(2), dest)
		if got[0] != matchAmbiguous || got[1] != matchAmbiguous {
			t.Errorf("match = %v, want both ambiguous", got)
		}
	})
	t.Run("equally good candidates are fine when every source trigger is enabled", func(t *testing.T) {
		src := []manifest.Trigger{gh("", "all-pushes", false), gh("", "all-pushes", false)}
		dest := []project.Trigger{dt("d1", "x", "all-pushes"), dt("d2", "y", "all-pushes")}
		got := matchCutoverTriggers(src, prov(2), dest)
		if got[0] < 0 || got[1] < 0 || got[0] == got[1] {
			t.Errorf("match = %v, want two distinct matches", got)
		}
	})
	t.Run("distinct presets do not compete", func(t *testing.T) {
		src := []manifest.Trigger{gh("", "all-pushes", false), gh("", "only-pull-requests", true)}
		dest := []project.Trigger{dt("d1", "x", "only-pull-requests"), dt("d2", "y", "all-pushes")}
		got := matchCutoverTriggers(src, prov(2), dest)
		if got[0] != 1 || got[1] != 0 {
			t.Errorf("match = %v, want [1 0]", got)
		}
	})
	t.Run("no candidate", func(t *testing.T) {
		got := matchCutoverTriggers([]manifest.Trigger{gh("a", "all-pushes", false)}, prov(1), nil)
		if got[0] != matchNone {
			t.Errorf("match = %v", got)
		}
	})
}

func TestCutover_EnableDest_AmbiguousTriggerIsManualAndNotEnabled(t *testing.T) {
	trig := func(preset string, disabled bool) manifest.Trigger {
		return manifest.Trigger{EventPreset: preset, Disabled: disabled, EventSource: manifest.TriggerEventSource{Provider: "github_app"}}
	}
	m := &manifest.Manifest{
		Source: manifest.Source{Org: manifest.Org{Slug: "gh/acme"}},
		Projects: []manifest.Project{{Slug: "circleci/acme/web-uuid", Name: "web", PipelineDefinitions: []manifest.PipelineDefinition{{
			Name: "build", Triggers: []manifest.Trigger{trig("all-pushes", false), trig("all-pushes", true)},
		}}}},
	}
	sy, fp := destFixtures(map[string]bool{"d-push": true, "d-pr": true})
	fp.listTriggers = func(string, string) ([]project.Trigger, error) {
		return []project.Trigger{
			{ID: "d1", EventPreset: "all-pushes", Disabled: true, EventSource: project.TriggerEventSource{Provider: "github_app"}},
			{ID: "d2", EventPreset: "all-pushes", Disabled: true, EventSource: project.TriggerEventSource{Provider: "github_app"}},
		}, nil
	}
	rep, err := (&Cutover{Dest: sy}).Run(context.Background(), m, cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: statePath(t)})
	if err != nil {
		t.Fatal(err)
	}
	if fp.hasCalled("EnableTrigger") {
		t.Fatalf("an ambiguous match must not enable anything: %+v", fp.callsTo("EnableTrigger"))
	}
	if actionStatus(rep, kindCutoverDestTrigger, "def:build/trigger:") == "" {
		t.Fatalf("no action reported: %+v", rep.Actions)
	}
	found := false
	for _, a := range rep.Actions {
		if a.Status == "manual" && strings.Contains(a.Detail, "equally well") {
			found = true
		}
	}
	if !found {
		t.Errorf("want a manual ambiguity report: %+v", rep.Actions)
	}
}

func TestCutover_EnableDest_SkipsProjectsPausedInTheManifest(t *testing.T) {
	yes := true
	no := false
	m := enableManifest()
	m.Projects[0].Settings = &manifest.AdvancedSettings{DropAllBuildRequests: &yes}
	m.Projects[1].Followed = &no
	m.Projects[1].Name = "api"
	sy, fp := destFixtures(map[string]bool{"d-push": true})
	fp.listOrgProjects = func(string) ([]project.OrgProject, error) {
		return []project.OrgProject{{ID: "dest-web", Name: "web"}, {ID: "dest-api", Name: "api"}}, nil
	}
	rep, err := (&Cutover{Dest: sy}).Run(context.Background(), m, cutoverMapping(), CutoverOptions{Apply: true, EnableDest: true, StatePath: statePath(t)})
	if err != nil {
		t.Fatal(err)
	}
	if fp.hasCalled("EnableTrigger") {
		t.Fatalf("projects paused/not followed in the manifest must not be enabled: %+v", fp.callsTo("EnableTrigger"))
	}
	if actionStatus(rep, kindCutoverDestTrigger, "web") != "manual" || actionStatus(rep, kindCutoverDestTrigger, "api") != "manual" {
		t.Errorf("both projects should be reported manual: %+v", rep.Actions)
	}
	var web, api string
	for _, a := range rep.Actions {
		switch a.Target {
		case "web":
			web = a.Detail
		case "api":
			api = a.Detail
		}
	}
	if !strings.Contains(web, "drop_all_build_requests") || !strings.Contains(api, "not followed") {
		t.Errorf("reasons: web=%q api=%q", web, api)
	}
}
