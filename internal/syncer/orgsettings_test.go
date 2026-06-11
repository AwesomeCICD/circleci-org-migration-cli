package syncer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ─────────────────────────────────────────────────────────────────────────────
// splitDestSlug
// ─────────────────────────────────────────────────────────────────────────────

func TestSplitDestSlug(t *testing.T) {
	cases := []struct {
		input       string
		wantVCS     string
		wantOrgName string
	}{
		{"gh/acme", "github", "acme"},
		{"bb/myorg", "bitbucket", "myorg"},
		{"github/acme", "github", "acme"},
		{"circleci/some-uuid", "", ""},
		{"circleci/", "", ""},
		{"", "", ""},
		{"noslash", "", ""},
	}
	for _, tc := range cases {
		vcs, org := splitDestSlug(tc.input)
		if vcs != tc.wantVCS || org != tc.wantOrgName {
			t.Errorf("splitDestSlug(%q) = (%q, %q), want (%q, %q)",
				tc.input, vcs, org, tc.wantVCS, tc.wantOrgName)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fake OrgSettingsWriter
// ─────────────────────────────────────────────────────────────────────────────

// orgSettingsCall records one method invocation on fakeOrgSettingsWriter.
type orgSettingsCall struct {
	method string
	args   []string
}

// fakeOrgSettingsWriter records calls for assertion in tests.
// It optionally implements URLOrbAllowListGetter and OTelExporterGetter when
// the corresponding func fields are set; this lets tests exercise the
// idempotency pre-flight code paths without requiring a new interface.
type fakeOrgSettingsWriter struct {
	updateFeatureFlags        func(vcsType, orgName string, flags map[string]bool) error
	setOIDCClaims             func(orgID string, audience []string, ttl string) error
	getURLOrbAllowList        func(slugOrID string) ([]URLOrbAllowEntry, error)
	createURLOrbAllowEntry    func(slugOrID, name, prefix, auth string) error
	putPolicyBundle           func(ownerID string, policies map[string]string) error
	setPolicyEnforcement      func(ownerID string, enabled bool) error
	getOTelExporters          func(orgID string) ([]OTelExporter, error)
	createOTelExporter        func(orgID, endpoint, protocol string, insecure bool, headers map[string]string) error
	setContacts               func(orgID string, primary, security []string) error
	setStorageRetention       func(orgUUID string, controls StorageRetentionArgs) error
	setBudget                 func(orgUUID string, projectID *string, credits int) error
	setBlockUnregisteredUsers func(orgUUID string, enabled bool) error
	setReleaseTrackerSettings func(orgUUID string, ttl string) error

	calls                      []orgSettingsCall
	flagsWritten               []map[string]bool // each call to UpdateFeatureFlags
	oidcCalls                  int
	urlOrbCalls                int
	urlOrbGetCalls             int
	policyPuts                 int
	enforcementSets            int
	otelCalls                  int
	otelGetCalls               int
	contactsCalls              int
	storageRetentionSets       int
	budgetSets                 int
	blockUnregisteredUsersSets int
	releaseTrackerSets         int
}

func (f *fakeOrgSettingsWriter) UpdateFeatureFlags(_ context.Context, vcsType, orgName string, flags map[string]bool) error {
	f.calls = append(f.calls, orgSettingsCall{"UpdateFeatureFlags", []string{vcsType, orgName}})
	f.flagsWritten = append(f.flagsWritten, flags)
	if f.updateFeatureFlags != nil {
		return f.updateFeatureFlags(vcsType, orgName, flags)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetOIDCClaims(_ context.Context, orgID string, audience []string, ttl string) error {
	f.calls = append(f.calls, orgSettingsCall{"SetOIDCClaims", []string{orgID, ttl}})
	f.oidcCalls++
	if f.setOIDCClaims != nil {
		return f.setOIDCClaims(orgID, audience, ttl)
	}
	return nil
}

// GetURLOrbAllowList implements URLOrbAllowListGetter (optional capability).
// Only active when f.getURLOrbAllowList is non-nil; otherwise the method is not
// exposed (type assertion returns false).
func (f *fakeOrgSettingsWriter) GetURLOrbAllowList(_ context.Context, slugOrID string) ([]URLOrbAllowEntry, error) {
	f.calls = append(f.calls, orgSettingsCall{"GetURLOrbAllowList", []string{slugOrID}})
	f.urlOrbGetCalls++
	if f.getURLOrbAllowList != nil {
		return f.getURLOrbAllowList(slugOrID)
	}
	return nil, nil
}

func (f *fakeOrgSettingsWriter) CreateURLOrbAllowEntry(_ context.Context, slugOrID, name, prefix, auth string) error {
	f.calls = append(f.calls, orgSettingsCall{"CreateURLOrbAllowEntry", []string{slugOrID, name, prefix, auth}})
	f.urlOrbCalls++
	if f.createURLOrbAllowEntry != nil {
		return f.createURLOrbAllowEntry(slugOrID, name, prefix, auth)
	}
	return nil
}

// GetOTelExporters implements OTelExporterGetter (optional capability).
// Only active when f.getOTelExporters is non-nil; otherwise the method is not
// exposed (type assertion returns false).
func (f *fakeOrgSettingsWriter) GetOTelExporters(_ context.Context, orgID string) ([]OTelExporter, error) {
	f.calls = append(f.calls, orgSettingsCall{"GetOTelExporters", []string{orgID}})
	f.otelGetCalls++
	if f.getOTelExporters != nil {
		return f.getOTelExporters(orgID)
	}
	return nil, nil
}

func (f *fakeOrgSettingsWriter) PutPolicyBundle(_ context.Context, ownerID string, policies map[string]string) error {
	f.calls = append(f.calls, orgSettingsCall{"PutPolicyBundle", []string{ownerID}})
	f.policyPuts++
	if f.putPolicyBundle != nil {
		return f.putPolicyBundle(ownerID, policies)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetPolicyEnforcement(_ context.Context, ownerID string, enabled bool) error {
	v := "false"
	if enabled {
		v = "true"
	}
	f.calls = append(f.calls, orgSettingsCall{"SetPolicyEnforcement", []string{ownerID, v}})
	f.enforcementSets++
	if f.setPolicyEnforcement != nil {
		return f.setPolicyEnforcement(ownerID, enabled)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) CreateOTelExporter(_ context.Context, orgID, endpoint, protocol string, insecure bool, headers map[string]string) error {
	ins := "false"
	if insecure {
		ins = "true"
	}
	f.calls = append(f.calls, orgSettingsCall{"CreateOTelExporter", []string{orgID, endpoint, protocol, ins}})
	f.otelCalls++
	if f.createOTelExporter != nil {
		return f.createOTelExporter(orgID, endpoint, protocol, insecure, headers)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetContacts(_ context.Context, orgID string, primary, security []string) error {
	f.calls = append(f.calls, orgSettingsCall{"SetContacts", []string{orgID}})
	f.contactsCalls++
	if f.setContacts != nil {
		return f.setContacts(orgID, primary, security)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetStorageRetention(_ context.Context, orgUUID string, controls StorageRetentionArgs) error {
	f.calls = append(f.calls, orgSettingsCall{"SetStorageRetention", []string{orgUUID}})
	f.storageRetentionSets++
	if f.setStorageRetention != nil {
		return f.setStorageRetention(orgUUID, controls)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetBudget(_ context.Context, orgUUID string, projectID *string, credits int) error {
	pid := "<nil>"
	if projectID != nil {
		pid = *projectID
	}
	f.calls = append(f.calls, orgSettingsCall{"SetBudget", []string{orgUUID, pid}})
	f.budgetSets++
	if f.setBudget != nil {
		return f.setBudget(orgUUID, projectID, credits)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetBlockUnregisteredUsers(_ context.Context, orgUUID string, enabled bool) error {
	v := "false"
	if enabled {
		v = "true"
	}
	f.calls = append(f.calls, orgSettingsCall{"SetBlockUnregisteredUsers", []string{orgUUID, v}})
	f.blockUnregisteredUsersSets++
	if f.setBlockUnregisteredUsers != nil {
		return f.setBlockUnregisteredUsers(orgUUID, enabled)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) SetReleaseTrackerSettings(_ context.Context, orgUUID string, ttl string) error {
	f.calls = append(f.calls, orgSettingsCall{"SetReleaseTrackerSettings", []string{orgUUID, ttl}})
	f.releaseTrackerSets++
	if f.setReleaseTrackerSettings != nil {
		return f.setReleaseTrackerSettings(orgUUID, ttl)
	}
	return nil
}

func (f *fakeOrgSettingsWriter) hasCalled(method string) bool {
	for _, c := range f.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func boolPtr(b bool) *bool { return &b }

// orgSettingsManifest builds a manifest with the given OrgSettings.
func orgSettingsManifest(settings *manifest.OrgSettings) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{
				Slug:     "gh/src",
				ID:       "src-org-id",
				Name:     "src",
				VCSType:  "github",
				Settings: settings,
			},
		},
	}
}

// newOrgSettingsSyncer builds a Syncer wired for org-settings tests.
func newOrgSettingsSyncer(fw *fakeOrgSettingsWriter) *Syncer {
	return &Syncer{
		Org:         &fakeOrgResolver{},
		OrgSettings: fw,
	}
}

// actionsOfStatus returns all actions with the given status from the report.
func actionsOfStatus(rep *Report, status string) []Action {
	var out []Action
	for _, a := range rep.Actions {
		if a.Status == status {
			out = append(out, a)
		}
	}
	return out
}

// ─────────────────────────────────────────────────────────────────────────────
// SyncOrgSettings: audit-log configs (manual report, never auto-applied)
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_AuditLogConfigs_ManualNoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		AuditLogConfigs: []manifest.AuditLogConfig{
			{
				ID:         "cfg-1",
				Purpose:    "security",
				TargetType: "s3",
				Config: manifest.AuditLogTarget{
					ARN:          "arn:aws:iam::123:role/audit",
					Region:       "us-east-1",
					BucketName:   "acme-audit",
					BucketPrefix: "logs/",
					Endpoint:     "https://s3.amazonaws.com",
				},
			},
		},
	})

	// Even with Apply=true, audit-log configs must never trigger a write.
	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manual := actionsOfStatus(rep, "manual")
	var found *Action
	for i := range manual {
		if manual[i].Target == "audit_log_config:security" {
			found = &manual[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a manual action for audit_log_config:security, got %+v", rep.Actions)
		return
	}
	for _, want := range []string{"security", "us-east-1", "acme-audit", "arn:aws:iam::123:role/audit", "environment-specific"} {
		if !strings.Contains(found.Detail, want) {
			t.Errorf("detail %q missing %q", found.Detail, want)
		}
	}
	// No org-settings writer method should have been called for audit-log configs.
	if len(fw.calls) != 0 {
		t.Errorf("expected no writer calls for audit-log configs, got %v", fw.calls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SyncOrgSettings: SSO (manual report, never auto-applied)
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_SSO_ManualNoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		SSO: &manifest.SSOSettings{
			Enforced:   true,
			Realm:      "acme-saml",
			Connection: map[string]any{"realm": "acme-saml"},
		},
	})

	// Even with Apply=true, SSO must never trigger a write.
	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manual := actionsOfStatus(rep, "manual")
	var found *Action
	for i := range manual {
		if manual[i].Target == "sso" {
			found = &manual[i]
		}
	}
	if found == nil {
		t.Fatalf("expected a manual action for sso, got %+v", rep.Actions)
		return
	}
	for _, want := range []string{"acme-saml", "DNS TXT", "IdP", "cannot be auto-synced"} {
		if !strings.Contains(found.Detail, want) {
			t.Errorf("detail %q missing %q", found.Detail, want)
		}
	}
	if len(fw.calls) != 0 {
		t.Errorf("expected no writer calls for SSO, got %v", fw.calls)
	}
}

func TestSyncOrgSettings_SSO_NoneWhenNil(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	// Settings present but SSO nil → no SSO action.
	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, a := range rep.Actions {
		if a.Target == "sso" {
			t.Errorf("did not expect an sso action when SSO is nil, got %+v", a)
		}
	}
	if len(fw.calls) != 0 {
		t.Errorf("expected no writer calls, got %v", fw.calls)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SyncOrgSettings: basic
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_NilSettings_NoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(nil) // no settings
	rep, err := sy.SyncOrgSettings(context.Background(), m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("expected no actions, got %v", rep.Actions)
	}
	if fw.hasCalled("UpdateFeatureFlags") {
		t.Error("UpdateFeatureFlags must not be called when settings is nil")
	}
}

func TestSyncOrgSettings_NilWriter_NoError(t *testing.T) {
	sy := &Syncer{
		Org:         &fakeOrgResolver{},
		OrgSettings: nil, // no writer injected
	}

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{"allow_private_orbs": true},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rep.Actions) != 0 {
		t.Errorf("expected no actions when writer is nil, got %v", rep.Actions)
	}
}

func TestSyncOrgSettings_ResolveOrgIDError(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			return "", errors.New("resolve failed")
		},
	}
	sy := &Syncer{Org: fr, OrgSettings: fw}

	m := orgSettingsManifest(&manifest.OrgSettings{})
	_, err := sy.SyncOrgSettings(context.Background(), m, nil, Options{Apply: true})
	if err == nil {
		t.Fatal("expected error from ResolveOrgID failure, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Feature flags
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_FeatureFlags_DangerFlagsSkipped(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{
			"allow_private_orbs":                true,  // safe — should be written
			"drop_all_build_requests":           false, // DANGER — skip
			"require_context_group_restriction": true,  // DANGER — skip
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the safe flag should trigger an UpdateFeatureFlags call.
	writtenFlags := fw.flagsWritten
	for _, flags := range writtenFlags {
		if _, found := flags["drop_all_build_requests"]; found {
			t.Error("drop_all_build_requests must never be written")
		}
		if _, found := flags["require_context_group_restriction"]; found {
			t.Error("require_context_group_restriction must never be written")
		}
	}

	// Danger flags should produce "manual" actions.
	manual := actionsOfStatus(rep, "manual")
	if len(manual) != 2 {
		t.Errorf("expected 2 manual actions for danger flags, got %d: %v", len(manual), manual)
	}
	for _, a := range manual {
		if !strings.Contains(a.Target, "drop_all_build_requests") &&
			!strings.Contains(a.Target, "require_context_group_restriction") {
			t.Errorf("unexpected manual action target: %q", a.Target)
		}
	}
}

func TestSyncOrgSettings_FeatureFlags_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{
			"allow_private_orbs":          true,
			"allow_certified_public_orbs": false,
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("UpdateFeatureFlags") {
		t.Error("UpdateFeatureFlags must NOT be called in dry-run mode")
	}

	// Safe flags should have "set" actions (would-write).
	setActions := actionsOfStatus(rep, "set")
	if len(setActions) != 2 {
		t.Errorf("expected 2 set actions in dry-run, got %d", len(setActions))
	}
}

func TestSyncOrgSettings_FeatureFlags_ApplyTrue_WritesEachFlagSeparately(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{
			"allow_private_orbs":            true,
			"allow_certified_public_orbs":   true,
			"allow_uncertified_public_orbs": false,
		},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Each safe flag gets its own UpdateFeatureFlags call.
	if len(fw.flagsWritten) != 3 {
		t.Errorf("expected 3 UpdateFeatureFlags calls, got %d", len(fw.flagsWritten))
	}
}

func TestSyncOrgSettings_FeatureFlags_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		updateFeatureFlags: func(vcsType, orgName string, flags map[string]bool) error {
			return errors.New("write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{"allow_private_orbs": true},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an 'error' action when UpdateFeatureFlags fails")
	}
}

func TestSyncOrgSettings_FeatureFlags_CircleCIDestSlug_Manual(t *testing.T) {
	// Destination is a circleci/-type org — feature flags cannot be written via v1.1.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{"allow_private_orbs": true},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("circleci/dest-uuid"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("UpdateFeatureFlags") {
		t.Error("UpdateFeatureFlags must NOT be called for circleci/-type dest slug")
	}
	manual := actionsOfStatus(rep, "manual")
	if len(manual) != 1 {
		t.Errorf("expected 1 manual action for circleci/ slug, got %d: %v", len(manual), manual)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OIDC
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_OIDC_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OIDCAudience: []string{"https://example.com"},
		OIDCTTL:      "1h",
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetOIDCClaims") {
		t.Error("SetOIDCClaims must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for OIDC in dry-run")
	}
}

func TestSyncOrgSettings_OIDC_ApplyTrue(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OIDCAudience: []string{"https://example.com"},
		OIDCTTL:      "2h",
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.oidcCalls != 1 {
		t.Errorf("expected 1 SetOIDCClaims call, got %d", fw.oidcCalls)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a 'set' action for OIDC")
	}
}

func TestSyncOrgSettings_OIDC_Empty_NoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		// No OIDC audience or TTL
		FeatureFlags: map[string]bool{},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetOIDCClaims") {
		t.Error("SetOIDCClaims must NOT be called when audience and TTL are empty")
	}
}

func TestSyncOrgSettings_OIDC_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setOIDCClaims: func(orgID string, audience []string, ttl string) error {
			return errors.New("oidc write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OIDCAudience: []string{"aud"},
		OIDCTTL:      "1h",
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("OIDC error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an 'error' action when SetOIDCClaims fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// URL-orb allow list
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_URLOrbAllowList_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/", Auth: "none"},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("CreateURLOrbAllowEntry") {
		t.Error("CreateURLOrbAllowEntry must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for URL-orb allow list in dry-run")
	}
}

func TestSyncOrgSettings_URLOrbAllowList_ApplyTrue(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "entry1", Prefix: "https://a.example.com/", Auth: "none"},
			{Name: "entry2", Prefix: "https://b.example.com/", Auth: "aws"},
		},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.urlOrbCalls != 2 {
		t.Errorf("expected 2 CreateURLOrbAllowEntry calls, got %d", fw.urlOrbCalls)
	}
}

func TestSyncOrgSettings_URLOrbAllowList_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		createURLOrbAllowEntry: func(slugOrID, name, prefix, auth string) error {
			return errors.New("url orb write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "bad", Prefix: "https://bad.example.com/", Auth: "none"},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("URL-orb error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an 'error' action when CreateURLOrbAllowEntry fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Config policies
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_Policies_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ConfigPolicies:           map[string]string{"my_policy": "package org\ndefault allow = false"},
		PolicyEnforcementEnabled: boolPtr(true),
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("PutPolicyBundle") {
		t.Error("PutPolicyBundle must NOT be called in dry-run mode")
	}
	if fw.hasCalled("SetPolicyEnforcement") {
		t.Error("SetPolicyEnforcement must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) < 2 {
		t.Errorf("expected at least 2 set actions in dry-run for policies+enforcement, got %d", len(setActions))
	}
}

func TestSyncOrgSettings_Policies_ApplyTrue(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ConfigPolicies:           map[string]string{"p1": "rego1", "p2": "rego2"},
		PolicyEnforcementEnabled: boolPtr(true),
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.policyPuts != 1 {
		t.Errorf("expected 1 PutPolicyBundle call, got %d", fw.policyPuts)
	}
	if fw.enforcementSets != 1 {
		t.Errorf("expected 1 SetPolicyEnforcement call, got %d", fw.enforcementSets)
	}
}

func TestSyncOrgSettings_Policies_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		putPolicyBundle: func(ownerID string, policies map[string]string) error {
			return errors.New("not on Scale plan")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ConfigPolicies: map[string]string{"p": "rego"},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("policy write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an 'error' action when PutPolicyBundle fails")
	}
}

func TestSyncOrgSettings_PolicyEnforcement_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setPolicyEnforcement: func(ownerID string, enabled bool) error {
			return errors.New("enforcement write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		PolicyEnforcementEnabled: boolPtr(false),
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("enforcement write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an 'error' action when SetPolicyEnforcement fails")
	}
}

func TestSyncOrgSettings_NoPolicies_NoPolicyEnforcement_NoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	// Settings present but no policies and nil enforcement.
	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("PutPolicyBundle") {
		t.Error("PutPolicyBundle must NOT be called when ConfigPolicies is empty")
	}
	if fw.hasCalled("SetPolicyEnforcement") {
		t.Error("SetPolicyEnforcement must NOT be called when PolicyEnforcementEnabled is nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Mapping / Report fields
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_MappingUsedForDestSlug(t *testing.T) {
	var resolvedSlug string
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			resolvedSlug = slug
			return "dest-id", nil
		},
	}
	fw := &fakeOrgSettingsWriter{}
	sy := &Syncer{Org: fr, OrgSettings: fw}

	m := orgSettingsManifest(nil)
	mapping := mappingTo("gh/dest-org")

	_, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedSlug != "gh/dest-org" {
		t.Errorf("ResolveOrgID slug: got %q want %q", resolvedSlug, "gh/dest-org")
	}
}

func TestSyncOrgSettings_Report_DestOrgSlugAndID(t *testing.T) {
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			return "resolved-dest-id", nil
		},
	}
	fw := &fakeOrgSettingsWriter{}
	sy := &Syncer{Org: fr, OrgSettings: fw}

	m := orgSettingsManifest(nil)
	mapping := mappingTo("gh/dest")

	rep, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.DestOrgSlug != "gh/dest" {
		t.Errorf("DestOrgSlug: got %q want %q", rep.DestOrgSlug, "gh/dest")
	}
	if rep.DestOrgID != "resolved-dest-id" {
		t.Errorf("DestOrgID: got %q want %q", rep.DestOrgID, "resolved-dest-id")
	}
}

func TestSyncOrgSettings_AppliedField(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)
	m := orgSettingsManifest(nil)

	repDry, _ := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if repDry.Applied {
		t.Error("Applied should be false when Apply=false")
	}

	repApply, _ := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if !repApply.Applied {
		t.Error("Applied should be true when Apply=true")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// OTel exporters
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_OTel_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf", Insecure: false},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("CreateOTelExporter") {
		t.Error("CreateOTelExporter must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for OTel exporter in dry-run")
	}
}

func TestSyncOrgSettings_OTel_ApplyTrue_Created(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf", Insecure: false},
			{Endpoint: "grpc.example.com:4317", Protocol: "grpc", Insecure: true},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.otelCalls != 2 {
		t.Errorf("expected 2 CreateOTelExporter calls, got %d", fw.otelCalls)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected set actions for OTel exporters")
	}
}

func TestSyncOrgSettings_OTel_HeaderKeys_ManualAction(t *testing.T) {
	// When an exporter had headers, a manual action listing the key names must be emitted.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{
				Endpoint: "https://otel.example.com:4318",
				Protocol: "http/protobuf",
				Insecure: false,
				Headers:  map[string]string{"Authorization": "xxxx", "X-Trace-Id": "xxxx"},
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manual := actionsOfStatus(rep, "manual")
	if len(manual) == 0 {
		t.Fatal("expected a manual action for redacted header keys, got none")
	}

	found := false
	for _, a := range manual {
		if strings.Contains(a.Detail, "Authorization") && strings.Contains(a.Detail, "X-Trace-Id") {
			found = true
		}
	}
	if !found {
		t.Errorf("manual action should mention header keys Authorization and X-Trace-Id; got %+v", manual)
	}
}

func TestSyncOrgSettings_OTel_NoHeaders_NoManualAction(t *testing.T) {
	// When an exporter had no headers, no manual action should be emitted for headers.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf", Insecure: false},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Status == "manual" && strings.Contains(a.Target, "otel") {
			t.Errorf("unexpected manual action for OTel exporter without headers: %+v", a)
		}
	}
}

func TestSyncOrgSettings_OTel_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		createOTelExporter: func(orgID, endpoint, protocol string, insecure bool, headers map[string]string) error {
			return errors.New("otel create failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf", Insecure: false},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("OTel write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when CreateOTelExporter fails")
	}
}

func TestSyncOrgSettings_OTel_Empty_NoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{}, // empty
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("CreateOTelExporter") {
		t.Error("CreateOTelExporter must NOT be called when OTelExporters is empty")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Contacts
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_Contacts_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary:  []string{"alice@example.com"},
			Security: []string{"sec@example.com"},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetContacts") {
		t.Error("SetContacts must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for contacts in dry-run")
	}
}

func TestSyncOrgSettings_Contacts_ApplyTrue(t *testing.T) {
	var gotPrimary, gotSecurity []string
	fw := &fakeOrgSettingsWriter{
		setContacts: func(orgID string, primary, security []string) error {
			gotPrimary = primary
			gotSecurity = security
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary:  []string{"alice@example.com", "bob@example.com"},
			Security: []string{"sec@example.com"},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.contactsCalls != 1 {
		t.Errorf("expected 1 SetContacts call, got %d", fw.contactsCalls)
	}

	if len(gotPrimary) != 2 || gotPrimary[0] != "alice@example.com" {
		t.Errorf("SetContacts primary: got %v", gotPrimary)
	}
	if len(gotSecurity) != 1 || gotSecurity[0] != "sec@example.com" {
		t.Errorf("SetContacts security: got %v", gotSecurity)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for contacts")
	}
}

func TestSyncOrgSettings_Contacts_NilContacts_Skip(t *testing.T) {
	// Nil Contacts → SetContacts must never be called.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
		Contacts:     nil,
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetContacts") {
		t.Error("SetContacts must NOT be called when Contacts is nil")
	}
}

func TestSyncOrgSettings_Contacts_BothEmpty_Skip(t *testing.T) {
	// Contacts present but both lists empty → SetContacts must not be called.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{Primary: []string{}, Security: []string{}},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetContacts") {
		t.Error("SetContacts must NOT be called when both lists are empty")
	}
}

func TestSyncOrgSettings_Contacts_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setContacts: func(orgID string, primary, security []string) error {
			return errors.New("contacts write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{Primary: []string{"alice@example.com"}},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("contacts write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when SetContacts fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Storage retention
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_StorageRetention_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{
			CacheDays:     10,
			WorkspaceDays: 7,
			ArtifactDays:  1,
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetStorageRetention") {
		t.Error("SetStorageRetention must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for storage retention in dry-run")
	}
}

func TestSyncOrgSettings_StorageRetention_ApplyTrue(t *testing.T) {
	var gotOrgUUID string
	var gotControls StorageRetentionArgs

	fw := &fakeOrgSettingsWriter{
		setStorageRetention: func(orgUUID string, controls StorageRetentionArgs) error {
			gotOrgUUID = orgUUID
			gotControls = controls
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{
			CacheDays:     15,
			WorkspaceDays: 10,
			ArtifactDays:  1,
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.storageRetentionSets != 1 {
		t.Errorf("expected 1 SetStorageRetention call, got %d", fw.storageRetentionSets)
	}
	if gotOrgUUID == "" {
		t.Error("orgUUID must be non-empty")
	}
	if gotControls.ArtifactDays != 1 {
		t.Errorf("ArtifactDays: got %d want 1", gotControls.ArtifactDays)
	}
	if gotControls.CacheDays != 15 {
		t.Errorf("CacheDays: got %d want 15", gotControls.CacheDays)
	}
	if gotControls.WorkspaceDays != 10 {
		t.Errorf("WorkspaceDays: got %d want 10", gotControls.WorkspaceDays)
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action for storage retention")
	}
}

func TestSyncOrgSettings_StorageRetention_Nil_NoWrite(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		FeatureFlags: map[string]bool{},
		// No StorageRetention.
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetStorageRetention") {
		t.Error("SetStorageRetention must NOT be called when StorageRetention is nil")
	}
}

func TestSyncOrgSettings_StorageRetention_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setStorageRetention: func(orgUUID string, controls StorageRetentionArgs) error {
			return errors.New("retention write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{
			CacheDays: 5, WorkspaceDays: 5, ArtifactDays: 1,
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("retention write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when SetStorageRetention fails")
	}
}

func TestSyncOrgSettings_StorageRetention_ReportMentionsClamping(t *testing.T) {
	// The report detail must mention that values may be clamped.
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{
			CacheDays: 10, WorkspaceDays: 5, ArtifactDays: 1,
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Target == "storage_retention" {
			if !strings.Contains(a.Detail, "clamp") {
				t.Errorf("storage_retention action detail should mention clamping, got: %q", a.Detail)
			}
			return
		}
	}
	t.Error("storage_retention action not found")
}

// ─────────────────────────────────────────────────────────────────────────────
// Org orbs — report as manual
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_Orbs_ReportedAsManual(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Orbs: []manifest.OrgOrb{
			{OrbName: "acme/my-orb", LatestVersionNumber: "0.3.0", IsPrivate: true},
			{OrbName: "acme/public-orb", LatestVersionNumber: "1.0.0", IsPrivate: false},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manualActions := actionsOfStatus(rep, "manual")
	if len(manualActions) < 2 {
		t.Fatalf("expected at least 2 manual actions for orbs, got %d: %+v", len(manualActions), manualActions)
	}

	// Each orb should be in a manual action.
	var orbTargets []string
	for _, a := range manualActions {
		if strings.HasPrefix(a.Target, "orb:") {
			orbTargets = append(orbTargets, a.Target)
		}
	}
	if len(orbTargets) != 2 {
		t.Errorf("expected 2 orb manual targets, got %d: %+v", len(orbTargets), orbTargets)
	}
}

func TestSyncOrgSettings_Orbs_DetailMentionsRepublish(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Orbs: []manifest.OrgOrb{
			{OrbName: "acme/my-orb", LatestVersionNumber: "0.3.0", IsPrivate: true},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Target == "orb:acme/my-orb" {
			if !strings.Contains(a.Detail, "republish") {
				t.Errorf("orb action detail should mention republish, got: %q", a.Detail)
			}
			return
		}
	}
	t.Error("orb:acme/my-orb action not found")
}

func TestSyncOrgSettings_Orbs_Empty_NoActions(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		Orbs: []manifest.OrgOrb{},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if strings.HasPrefix(a.Target, "orb:") {
			t.Errorf("unexpected orb action when Orbs is empty: %+v", a)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Release-tracker settings — sync
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_ReleaseTracker_DryRunNoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetReleaseTrackerSettings") {
		t.Error("SetReleaseTrackerSettings must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a dry-run set action for release-tracker settings")
	}
}

func TestSyncOrgSettings_ReleaseTracker_ApplyTrue_Written(t *testing.T) {
	var capturedTTL string
	fw := &fakeOrgSettingsWriter{
		setReleaseTrackerSettings: func(orgUUID, ttl string) error {
			capturedTTL = ttl
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "2h"},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedTTL != "2h" {
		t.Errorf("SetReleaseTrackerSettings: ttl got %q want %q", capturedTTL, "2h")
	}

	setActions := actionsOfStatus(rep, "set")
	if len(setActions) == 0 {
		t.Error("expected a set action after applying release-tracker settings")
	}
}

func TestSyncOrgSettings_ReleaseTracker_Nil_NoWrites(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ReleaseTracker: nil,
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetReleaseTrackerSettings") {
		t.Error("SetReleaseTrackerSettings must NOT be called when ReleaseTracker is nil")
	}
}

func TestSyncOrgSettings_ReleaseTracker_WriteError_IsErrorAction(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		setReleaseTrackerSettings: func(orgUUID, ttl string) error {
			return errors.New("release-tracker write failed")
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("release-tracker write error must not propagate, got: %v", err)
	}

	errActions := actionsOfStatus(rep, "error")
	if len(errActions) == 0 {
		t.Error("expected an error action when SetReleaseTrackerSettings fails")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Environment hierarchy — report as manual
// ─────────────────────────────────────────────────────────────────────────────

func TestSyncOrgSettings_EnvironmentHierarchy_ReportedAsManual(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		EnvironmentHierarchy: &manifest.EnvironmentHierarchy{
			Name:        "prod-hierarchy",
			Description: "desc",
			Levels: []manifest.EnvHierarchyLevel{
				{Position: 1, IntegrationName: "orbs-dev"},
				{Position: 2, IntegrationName: "prod-integration"},
			},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manualActions := actionsOfStatus(rep, "manual")
	var found bool
	for _, a := range manualActions {
		if a.Target == "environment_hierarchy" {
			found = true
			if !strings.Contains(a.Detail, "prod-hierarchy") {
				t.Errorf("detail should mention hierarchy name, got: %q", a.Detail)
			}
			if !strings.Contains(a.Detail, "orbs-dev") {
				t.Errorf("detail should mention level integration names, got: %q", a.Detail)
			}
			break
		}
	}
	if !found {
		t.Errorf("expected manual action for environment_hierarchy, got actions: %+v", rep.Actions)
	}
}

func TestSyncOrgSettings_EnvironmentHierarchy_DetailMentionsRecreate(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		EnvironmentHierarchy: &manifest.EnvironmentHierarchy{
			Name: "my-hierarchy",
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Target == "environment_hierarchy" {
			if !strings.Contains(a.Detail, "recreate") {
				t.Errorf("env-hierarchy detail should mention recreate, got: %q", a.Detail)
			}
			return
		}
	}
	t.Error("environment_hierarchy action not found")
}

func TestSyncOrgSettings_EnvironmentHierarchy_Nil_NoActions(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		EnvironmentHierarchy: nil,
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, a := range rep.Actions {
		if a.Target == "environment_hierarchy" {
			t.Errorf("unexpected environment_hierarchy action when nil: %+v", a)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix #2: OTel exporter idempotency (duplicate on re-run guard)
// ─────────────────────────────────────────────────────────────────────────────

// TestSyncOrgSettings_OTel_IdempotentRerun_SkipExisting verifies that when
// GetOTelExporters reports an exporter with the same endpoint+protocol already
// present, it is skipped with status "exists" and CreateOTelExporter is NOT
// called a second time.
func TestSyncOrgSettings_OTel_IdempotentRerun_SkipExisting(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getOTelExporters: func(orgID string) ([]OTelExporter, error) {
			return []OTelExporter{
				{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf"},
			}, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf", Insecure: false},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("CreateOTelExporter") {
		t.Error("CreateOTelExporter must NOT be called when exporter already exists (idempotent re-run)")
	}

	existsActions := actionsOfStatus(rep, "exists")
	found := false
	for _, a := range existsActions {
		if strings.Contains(a.Target, "otel") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an 'exists' action for the already-present OTel exporter, got: %+v", rep.Actions)
	}
}

// TestSyncOrgSettings_OTel_IdempotentRerun_NewExporterStillCreated verifies
// that when one exporter already exists and another is new, only the new one
// is created.
func TestSyncOrgSettings_OTel_IdempotentRerun_NewExporterStillCreated(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getOTelExporters: func(orgID string) ([]OTelExporter, error) {
			return []OTelExporter{
				{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf"},
			}, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf"}, // already exists
			{Endpoint: "grpc.example.com:4317", Protocol: "grpc"},                  // new
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only one call to CreateOTelExporter (for the new exporter).
	if fw.otelCalls != 1 {
		t.Errorf("expected 1 CreateOTelExporter call (new only), got %d", fw.otelCalls)
	}

	existsActions := actionsOfStatus(rep, "exists")
	setActions := actionsOfStatus(rep, "set")
	var otelExists, otelSet int
	for _, a := range existsActions {
		if strings.Contains(a.Target, "otel") {
			otelExists++
		}
	}
	for _, a := range setActions {
		if strings.Contains(a.Target, "otel") {
			otelSet++
		}
	}
	if otelExists != 1 {
		t.Errorf("expected 1 'exists' OTel action, got %d", otelExists)
	}
	if otelSet != 1 {
		t.Errorf("expected 1 'set' OTel action, got %d", otelSet)
	}
}

// TestSyncOrgSettings_OTel_DryRun_NoGetCall verifies that in dry-run mode
// GetOTelExporters is NOT called (pre-flight only runs in apply mode).
func TestSyncOrgSettings_OTel_DryRun_NoGetCall(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getOTelExporters: func(orgID string) ([]OTelExporter, error) {
			return nil, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com:4318", Protocol: "http/protobuf"},
		},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("GetOTelExporters") {
		t.Error("GetOTelExporters must NOT be called in dry-run mode")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix #3: URL-orb allow list idempotency (duplicate on re-run guard)
// ─────────────────────────────────────────────────────────────────────────────

// TestSyncOrgSettings_URLOrb_IdempotentRerun_SkipExisting verifies that when
// GetURLOrbAllowList reports an entry with the same name+prefix already present,
// it is skipped with status "exists" and CreateURLOrbAllowEntry is NOT called.
func TestSyncOrgSettings_URLOrb_IdempotentRerun_SkipExisting(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getURLOrbAllowList: func(slugOrID string) ([]URLOrbAllowEntry, error) {
			return []URLOrbAllowEntry{
				{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/"},
			}, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/", Auth: "none"},
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("CreateURLOrbAllowEntry") {
		t.Error("CreateURLOrbAllowEntry must NOT be called when entry already exists (idempotent re-run)")
	}

	existsActions := actionsOfStatus(rep, "exists")
	found := false
	for _, a := range existsActions {
		if strings.Contains(a.Target, "github-raw") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an 'exists' action for the already-present URL-orb entry, got: %+v", rep.Actions)
	}
}

// TestSyncOrgSettings_URLOrb_IdempotentRerun_NewEntryStillCreated verifies
// that when one URL-orb entry already exists and another is new, only the new
// one is created.
func TestSyncOrgSettings_URLOrb_IdempotentRerun_NewEntryStillCreated(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getURLOrbAllowList: func(slugOrID string) ([]URLOrbAllowEntry, error) {
			return []URLOrbAllowEntry{
				{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/"},
			}, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/", Auth: "none"}, // exists
			{Name: "s3-scripts", Prefix: "https://s3.example.com/scripts/", Auth: "aws"},     // new
		},
	})

	rep, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.urlOrbCalls != 1 {
		t.Errorf("expected 1 CreateURLOrbAllowEntry call (new only), got %d", fw.urlOrbCalls)
	}

	existsActions := actionsOfStatus(rep, "exists")
	found := false
	for _, a := range existsActions {
		if strings.Contains(a.Target, "github-raw") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an 'exists' action for github-raw, got: %+v", rep.Actions)
	}
}

// TestSyncOrgSettings_URLOrb_DryRun_NoGetCall verifies that in dry-run mode
// GetURLOrbAllowList is NOT called.
func TestSyncOrgSettings_URLOrb_DryRun_NoGetCall(t *testing.T) {
	fw := &fakeOrgSettingsWriter{
		getURLOrbAllowList: func(slugOrID string) ([]URLOrbAllowEntry, error) {
			return nil, nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	m := orgSettingsManifest(&manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "github-raw", Prefix: "https://raw.githubusercontent.com/", Auth: "none"},
		},
	})

	_, err := sy.SyncOrgSettings(context.Background(), m, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("GetURLOrbAllowList") {
		t.Error("GetURLOrbAllowList must NOT be called in dry-run mode")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Fix #7: Budget per-project mapping uses UUID (not slug); slug warns
// ─────────────────────────────────────────────────────────────────────────────

// TestSyncOrgSettings_Budget_ProjectUUID_Applied verifies that when the
// mapping value is a valid UUID (no "/"), SetBudget is called with that UUID
// as the project ID.
func TestSyncOrgSettings_Budget_ProjectUUID_Applied(t *testing.T) {
	var capturedProjID *string
	fw := &fakeOrgSettingsWriter{
		setBudget: func(orgUUID string, projectID *string, credits int) error {
			if projectID != nil {
				s := *projectID
				capturedProjID = &s
			}
			return nil
		},
	}
	sy := newOrgSettingsSyncer(fw)

	srcProjUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	destProjUUID := "ffffffff-0000-1111-2222-333333333333"
	credits := 5000

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			ProjectBudgets: []manifest.BudgetEntry{
				{ProjectID: &srcProjUUID, Credits: credits},
			},
		},
	})

	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/src", To: "gh/dest"},
		Projects: map[string]string{srcProjUUID: destProjUUID},
	}

	rep, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedProjID == nil {
		t.Fatal("SetBudget must be called with a non-nil projectID")
	}
	if *capturedProjID != destProjUUID {
		t.Errorf("SetBudget projectID: got %q want %q", *capturedProjID, destProjUUID)
	}

	setActions := actionsOfStatus(rep, "set")
	found := false
	for _, a := range setActions {
		if strings.Contains(a.Target, "budget:project") {
			found = true
			if !strings.Contains(a.Detail, destProjUUID) {
				t.Errorf("budget action detail should mention dest project UUID, got: %q", a.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected a 'set' action for per-project budget, got: %+v", rep.Actions)
	}
}

// TestSyncOrgSettings_Budget_ProjectSlug_Warning verifies that when the
// mapping value looks like a slug (contains "/"), a "manual" action is emitted
// and SetBudget is NOT called (the budgets API requires a UUID).
func TestSyncOrgSettings_Budget_ProjectSlug_Warning(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	srcProjUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	destSlugValue := "gh/acme/web" // slug, not UUID — should warn

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			ProjectBudgets: []manifest.BudgetEntry{
				{ProjectID: &srcProjUUID, Credits: 5000},
			},
		},
	})

	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/src", To: "gh/dest"},
		Projects: map[string]string{srcProjUUID: destSlugValue},
	}

	rep, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBudget") {
		t.Error("SetBudget must NOT be called when mapping value is a slug (not a UUID)")
	}

	manualActions := actionsOfStatus(rep, "manual")
	found := false
	for _, a := range manualActions {
		if strings.Contains(a.Target, "budget:project") {
			found = true
			if !strings.Contains(a.Detail, "UUID") {
				t.Errorf("warning should mention UUID requirement, got: %q", a.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected a 'manual' action warning about slug mapping, got: %+v", rep.Actions)
	}
}

// TestSyncOrgSettings_Budget_ProjectUUID_DryRun verifies that in dry-run mode
// a valid UUID mapping produces a "set" (would-set) action without calling SetBudget.
func TestSyncOrgSettings_Budget_ProjectUUID_DryRun(t *testing.T) {
	fw := &fakeOrgSettingsWriter{}
	sy := newOrgSettingsSyncer(fw)

	srcProjUUID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	destProjUUID := "ffffffff-0000-1111-2222-333333333333"

	m := orgSettingsManifest(&manifest.OrgSettings{
		Budgets: &manifest.OrgBudgets{
			ProjectBudgets: []manifest.BudgetEntry{
				{ProjectID: &srcProjUUID, Credits: 3000},
			},
		},
	})

	mapping := &manifest.Mapping{
		Org:      manifest.OrgMapping{From: "gh/src", To: "gh/dest"},
		Projects: map[string]string{srcProjUUID: destProjUUID},
	}

	rep, err := sy.SyncOrgSettings(context.Background(), m, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fw.hasCalled("SetBudget") {
		t.Error("SetBudget must NOT be called in dry-run mode")
	}

	setActions := actionsOfStatus(rep, "set")
	found := false
	for _, a := range setActions {
		if strings.Contains(a.Target, "budget:project") {
			found = true
			if !strings.Contains(a.Detail, destProjUUID) {
				t.Errorf("dry-run action should mention dest project UUID %q, got: %q", destProjUUID, a.Detail)
			}
		}
	}
	if !found {
		t.Errorf("expected a dry-run 'set' action for per-project budget, got: %+v", rep.Actions)
	}
}
