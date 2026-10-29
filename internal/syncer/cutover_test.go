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
	flags map[string]bool
	sets  []string
}

func (f *fakeOrgFlagsC) GetFeatureFlags(context.Context, string, string) (map[string]bool, error) {
	return f.flags, nil
}
func (f *fakeOrgFlagsC) UpdateFeatureFlags(_ context.Context, vcs, name string, fl map[string]bool) error {
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

func TestCutover_PauseSource_FailureIsErrorAndNotRecorded(t *testing.T) {
	src := newCutSource()
	src.setErr = errors.New("HTTP 500")
	src.toggleErr = errors.New("HTTP 500")
	sp := statePath(t)
	rep, err := (&Cutover{Source: src}).Run(context.Background(), cutoverManifest(), cutoverMapping(), CutoverOptions{Apply: true, PauseSource: true, StatePath: sp})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Counts()["error"] != 2 {
		t.Errorf("want 2 errors (flag + trigger), got %+v", rep.Actions)
	}
	st := readState(t, sp)
	for _, r := range st.SourceFlags {
		if r.Project == "gh/acme/web" {
			t.Errorf("a failed change must not stay recorded: %+v", st)
		}
	}
	if len(st.SourceTriggers) != 0 {
		t.Errorf("failed trigger disable must not stay recorded: %+v", st.SourceTriggers)
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
	if st := readState(t, sp); len(st.DestTriggers) != 0 {
		t.Errorf("a failed enable must not stay recorded: %+v", st)
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
