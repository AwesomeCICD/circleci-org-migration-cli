package exporter_test

// storage_retention_export_test.go contains focused unit tests for the
// storage-retention capture code path in exportOrgSettings.

import (
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// storageRetentionExporter builds an Exporter whose GetStorageRetention fake
// is controlled by the caller. All other org API methods return safe defaults.
func storageRetentionExporter(
	getRetention func(orgUUID string) (*org.StorageRetention, error),
) *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:     func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getStorageRetention: getRetention,
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}
}

var retentionOpts = exporter.Options{OrgSlug: "gh/myorg"}

// TestStorageRetention_CapturedIntoManifest verifies that when the API returns
// storage-retention controls they are stored in OrgSettings.StorageRetention.
func TestStorageRetention_CapturedIntoManifest(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return &org.StorageRetention{
			Controls: org.StorageRetentionControls{
				CacheDays:     10,
				WorkspaceDays: 5,
				ArtifactDays:  1,
			},
		}, nil
	})

	m, err := ex.Export(retentionOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("OrgSettings is nil")
	}
	sr := m.Source.Org.Settings.StorageRetention
	if sr == nil {
		t.Fatal("StorageRetention is nil in manifest")
	}
	if sr.CacheDays != 10 {
		t.Errorf("CacheDays: got %d want 10", sr.CacheDays)
	}
	if sr.WorkspaceDays != 5 {
		t.Errorf("WorkspaceDays: got %d want 5", sr.WorkspaceDays)
	}
	if sr.ArtifactDays != 1 {
		t.Errorf("ArtifactDays: got %d want 1", sr.ArtifactDays)
	}
}

// TestStorageRetention_APIError_Warning verifies that a GetStorageRetention
// error results in a "retention_unreadable" warning and does NOT fail the export.
func TestStorageRetention_APIError_Warning(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return nil, errors.New("permission denied")
	})

	m, err := ex.Export(retentionOpts)
	if err != nil {
		t.Fatalf("export must not fail on retention API error, got: %v", err)
	}

	// StorageRetention must not be set.
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.StorageRetention != nil {
		t.Error("StorageRetention should be nil when API returns error")
	}

	// A warning with code "retention_unreadable" must be present.
	found := false
	for _, w := range m.Warnings {
		if w.Code == "retention_unreadable" && w.Scope == "org" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected warning with code 'retention_unreadable', got warnings: %+v", m.Warnings)
	}
}

// TestStorageRetention_NilResponse_NoField verifies that a nil response from
// GetStorageRetention leaves StorageRetention unset (nil pointer guard).
func TestStorageRetention_NilResponse_NoField(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return nil, nil // no retention configured
	})

	m, err := ex.Export(retentionOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Source.Org.Settings != nil && m.Source.Org.Settings.StorageRetention != nil {
		t.Error("expected StorageRetention to be nil for nil API response")
	}
}

// TestStorageRetention_WarningMentionsError verifies that the warning detail
// message includes the original error text.
func TestStorageRetention_WarningMentionsError(t *testing.T) {
	ex := storageRetentionExporter(func(orgUUID string) (*org.StorageRetention, error) {
		return nil, errors.New("storage API unavailable")
	})

	m, err := ex.Export(retentionOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, w := range m.Warnings {
		if w.Code == "retention_unreadable" {
			if !strings.Contains(w.Message, "storage API unavailable") {
				t.Errorf("warning message does not mention original error: %q", w.Message)
			}
			return
		}
	}
	t.Error("retention_unreadable warning not found")
}
