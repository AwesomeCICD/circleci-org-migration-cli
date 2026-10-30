package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
)

func TestCutover_FlagValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"nothing to do", []string{"cutover"}, "nothing to do"},
		{"undo conflicts", []string{"cutover", "--undo", "--pause-source"}, "cannot be combined"},
		{"pause-org alone", []string{"cutover", "--pause-org", "--manifest", "m.json"}, "--pause-source"},
		{"manifest required", []string{"cutover", "--pause-source"}, "--manifest is required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := runCmd(t, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to contain %q", err, tc.want)
			}
		})
	}
}

// --undo with a state file that does not exist must FAIL (a mistyped --state
// path would otherwise look like a successful undo while the source stays paused).
func TestCutover_UndoMissingStateFileFails(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "tok")
	state := filepath.Join(t.TempDir(), "state.json")
	_, _, err := runCmd(t, "cutover", "--undo", "--state", state)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("err = %v, want a missing-state-file error", err)
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("a failed undo must not create the state file")
	}
}

// An existing state with nothing recorded is a harmless no-op (no manifest needed).
func TestCutover_UndoWithNothingRecorded(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "tok")
	state := filepath.Join(t.TempDir(), "state.json")
	if err := (&syncer.CutoverState{Version: 1}).Save(state); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runCmd(t, "cutover", "--undo", "--state", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "nothing to undo") {
		t.Errorf("stdout = %q", stdout)
	}
}

// --undo builds a client only for the side(s) with records: a state with only
// destination records needs no source token, and vice versa.
func TestCutover_UndoBuildsOnlyTheClientsItNeeds(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	dir := t.TempDir()

	destOnly := filepath.Join(dir, "dest.json")
	if err := (&syncer.CutoverState{Version: 1, DestTriggers: []syncer.CutoverTriggerRecord{{Project: "p", ProjectID: "pid", TriggerID: "t1"}}}).Save(destOnly); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CIRCLECI_DEST_TOKEN", "dest-tok")
	stdout, _, err := runCmd(t, "cutover", "--undo", "--state", destOnly)
	if err != nil {
		t.Fatalf("dest-only state must not need a source token: %v", err)
	}
	if !strings.Contains(stdout, "would disable trigger") {
		t.Errorf("stdout = %q", stdout)
	}

	srcOnly := filepath.Join(dir, "src.json")
	if err := (&syncer.CutoverState{Version: 1, SourceTriggers: []syncer.CutoverTriggerRecord{{Project: "gh/a/b", ProjectID: "pid", TriggerID: "t9"}}}).Save(srcOnly); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	if _, _, err := runCmd(t, "cutover", "--undo", "--state", srcOnly); err == nil || !strings.Contains(err.Error(), "no source API token") {
		t.Fatalf("source-only state with no source token: err = %v, want a no-source-token error", err)
	}
}

func TestCutover_HelpMentionsAppUninstallCaveat(t *testing.T) {
	stdout, _, err := runCmd(t, "cutover", "--help")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"UNINSTALLED", "drop-all-build-requests", "--undo", "0600"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("help should mention %q", want)
		}
	}
}
