// Internal (white-box) tests for applyArtifactRetentionControl in
// secrets_capture.go.  Uses package cmd (not cmd_test) to access unexported
// symbols.
package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/spf13/cobra"
)

// fakeStorageRetentionManager is a test double for storageRetentionManager.
type fakeStorageRetentionManager struct {
	getStorageRetention func(orgUUID string) (*org.StorageRetention, error)
	setStorageRetention func(orgUUID string, controls org.StorageRetentionControls) error

	setCallCount    int
	lastSetControls org.StorageRetentionControls
	lastSetOrgUUID  string
}

func (f *fakeStorageRetentionManager) GetStorageRetention(_ context.Context, orgUUID string) (*org.StorageRetention, error) {
	if f.getStorageRetention != nil {
		return f.getStorageRetention(orgUUID)
	}
	return &org.StorageRetention{
		Controls: org.StorageRetentionControls{
			CacheDays:     15,
			WorkspaceDays: 15,
			ArtifactDays:  30,
		},
	}, nil
}

func (f *fakeStorageRetentionManager) SetStorageRetention(_ context.Context, orgUUID string, controls org.StorageRetentionControls) error {
	f.setCallCount++
	f.lastSetControls = controls
	f.lastSetOrgUUID = orgUUID
	if f.setStorageRetention != nil {
		return f.setStorageRetention(orgUUID, controls)
	}
	return nil
}

// newTestCmdForRetention builds a cobra.Command with stderr captured.
func newTestCmdForRetention(stderrBuf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetErr(stderrBuf)
	return cmd
}

const testRetentionOrgUUID = "aaaaaaaa-bbbb-cccc-dddd-ffffffffffff"

// ─────────────────────────────────────────────────────────────────────────────
// applyArtifactRetentionControl happy-path
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyArtifactRetentionControl_SetsArtifactDaysOnly(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{}
	cmd := newTestCmdForRetention(&stderr)

	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 1)

	if mgr.setCallCount != 1 {
		t.Fatalf("expected 1 SetStorageRetention call, got %d", mgr.setCallCount)
	}
	if mgr.lastSetControls.ArtifactDays != 1 {
		t.Errorf("ArtifactDays: got %d want 1", mgr.lastSetControls.ArtifactDays)
	}
	// Cache and workspace must be preserved from the current values.
	if mgr.lastSetControls.CacheDays != 15 {
		t.Errorf("CacheDays should be preserved as 15, got %d", mgr.lastSetControls.CacheDays)
	}
	if mgr.lastSetControls.WorkspaceDays != 15 {
		t.Errorf("WorkspaceDays should be preserved as 15, got %d", mgr.lastSetControls.WorkspaceDays)
	}
}

func TestApplyArtifactRetentionControl_LogsPriorValue(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{}
	cmd := newTestCmdForRetention(&stderr)

	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 1)

	out := stderr.String()
	// stderr must contain the prior artifact-retention value (30).
	if !strings.Contains(out, "30") {
		t.Errorf("stderr should mention prior artifact_days (30), got: %q", out)
	}
	// stderr must mention how to restore.
	if !strings.Contains(out, "retention_days_artifact") {
		t.Errorf("stderr should mention retention_days_artifact for restore instructions, got: %q", out)
	}
	// stderr must mention "NOT auto-restored" to make the behaviour clear.
	if !strings.Contains(out, "NOT auto-restored") {
		t.Errorf("stderr should mention NOT auto-restored, got: %q", out)
	}
}

func TestApplyArtifactRetentionControl_OrgIDPassedThrough(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{}
	cmd := newTestCmdForRetention(&stderr)

	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 3)

	if mgr.lastSetOrgUUID != testRetentionOrgUUID {
		t.Errorf("orgUUID: got %q want %q", mgr.lastSetOrgUUID, testRetentionOrgUUID)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// applyArtifactRetentionControl error paths
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyArtifactRetentionControl_GetError_SkipsSet(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{
		getStorageRetention: func(orgUUID string) (*org.StorageRetention, error) {
			return nil, errors.New("permission denied")
		},
	}
	cmd := newTestCmdForRetention(&stderr)

	// Must not panic or return error.
	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 1)

	if mgr.setCallCount != 0 {
		t.Error("SetStorageRetention must not be called when GetStorageRetention fails")
	}
	if !strings.Contains(stderr.String(), "WARNING") {
		t.Error("stderr should contain a WARNING when GetStorageRetention fails")
	}
}

func TestApplyArtifactRetentionControl_SetError_PrintsWarning(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{
		setStorageRetention: func(orgUUID string, controls org.StorageRetentionControls) error {
			return errors.New("quota exceeded")
		},
	}
	cmd := newTestCmdForRetention(&stderr)

	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 1)

	if !strings.Contains(stderr.String(), "WARNING") {
		t.Errorf("stderr should contain WARNING on SetStorageRetention failure, got: %q", stderr.String())
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// applyArtifactRetentionControl: no-op when targetDays == 0
// (exercised via the command path; here we just confirm the helper skips)
// ─────────────────────────────────────────────────────────────────────────────

func TestApplyArtifactRetentionControl_TargetFive_SetsCorrectly(t *testing.T) {
	var stderr bytes.Buffer
	mgr := &fakeStorageRetentionManager{}
	cmd := newTestCmdForRetention(&stderr)

	applyArtifactRetentionControl(cmd, mgr, testRetentionOrgUUID, 5)

	if mgr.lastSetControls.ArtifactDays != 5 {
		t.Errorf("ArtifactDays: got %d want 5", mgr.lastSetControls.ArtifactDays)
	}
}
