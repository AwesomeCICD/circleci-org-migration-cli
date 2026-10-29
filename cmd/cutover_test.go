package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// --undo on an empty/missing state is a harmless no-op (and needs no manifest).
func TestCutover_UndoWithNothingRecorded(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "tok")
	state := filepath.Join(t.TempDir(), "state.json")
	stdout, _, err := runCmd(t, "cutover", "--undo", "--state", state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(stdout, "nothing to undo") {
		t.Errorf("stdout = %q", stdout)
	}
	if _, err := os.Stat(state); err == nil {
		t.Error("a dry-run undo must not create the state file")
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
