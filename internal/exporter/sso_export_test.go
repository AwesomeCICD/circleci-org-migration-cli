package exporter_test

// sso_export_test.go contains focused unit tests for the exportSSO code path
// (internal/exporter/exporter.go:exportSSO and its callers in exportOrgSettings).
//
// These tests run entirely against fakes — no live SSO-enabled org is required.
// All fake types and helpers (fakeOrgAPI, fakeContextAPI, fakeProjectAPI,
// minimalExporter, defaultOrg) are defined in exporter_test.go and are
// reused here unchanged.

import (
	"errors"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/org"
	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/exporter"
)

// ---------------------------------------------------------------------------
// Helper
// ---------------------------------------------------------------------------

// ssoExporter builds an Exporter whose SSO fakes are controlled by the caller.
// All other org API methods return safe defaults (nil / zero-value) so they do
// not pollute the manifest under test.
func ssoExporter(
	getSSOEnforced func(orgID string) (bool, error),
	getSSOConnection func(orgID string) (map[string]any, bool, error),
) *exporter.Exporter {
	return &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization:  func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getSSOEnforced:   getSSOEnforced,
			getSSOConnection: getSSOConnection,
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}
}

// exportSSO is the Options preset used by all SSO-focused tests.
var ssoOpts = exporter.Options{OrgSlug: "gh/myorg"}

// ---------------------------------------------------------------------------
// Scenario 1: SSO enforced=true with a fully-populated SAML connection body
// ---------------------------------------------------------------------------

// TestSSO_Enforced_WithConnection verifies that when enforcement is on and a
// SAML connection exists, exportSSO records the full state into OrgSettings.SSO
// and causes exportOrgSettings to set hasAny=true (so Settings is non-nil).
func TestSSO_Enforced_WithConnection(t *testing.T) {
	conn := map[string]any{
		"realm":            "acme-saml",
		"idp":              "okta",
		"sign_in_endpoint": "https://idp.example.com/sso/saml",
		"idp_entity_id":    "https://idp.example.com/entity",
	}

	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings must not be nil when SSO is configured")
	}
	sso := m.Source.Org.Settings.SSO
	if sso == nil {
		t.Fatal("SSO must not be nil when enforced=true and connection present")
	}
	if !sso.Enforced {
		t.Error("SSO.Enforced must be true")
	}
	if sso.Realm != "acme-saml" {
		t.Errorf("SSO.Realm: got %q, want %q", sso.Realm, "acme-saml")
	}
	if len(sso.Connection) == 0 {
		t.Fatal("SSO.Connection must not be empty")
	}
	if sso.Connection["sign_in_endpoint"] != "https://idp.example.com/sso/saml" {
		t.Errorf("SSO.Connection[sign_in_endpoint]: got %v", sso.Connection["sign_in_endpoint"])
	}
	if sso.Connection["idp"] != "okta" {
		t.Errorf("SSO.Connection[idp]: got %v", sso.Connection["idp"])
	}
}

// ---------------------------------------------------------------------------
// Scenario 2: SSO not enforced, no connection → nothing recorded, returns false
// ---------------------------------------------------------------------------

// TestSSO_NotEnforced_NoConnection verifies the "no SSO" path: when enforcement
// is off and the connection endpoint returns not-found (found=false), exportSSO
// should return false and OrgSettings.SSO must remain nil.
func TestSSO_NotEnforced_NoConnection(t *testing.T) {
	ex := ssoExporter(
		func(string) (bool, error) { return false, nil },
		func(string) (map[string]any, bool, error) { return nil, false, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Settings may be nil (no other settings populated either) or non-nil but SSO nil.
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.SSO != nil {
		t.Errorf("SSO must be nil when not enforced and no connection, got %+v",
			m.Source.Org.Settings.SSO)
	}
	// Confirm no sso_unreadable warning was emitted (no errors occurred).
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			t.Errorf("unexpected sso_unreadable warning when APIs succeeded: %+v", w)
		}
	}
}

// ---------------------------------------------------------------------------
// Scenario 3: SSO connection sub-fields — realistic SAML map with sensitive keys
// ---------------------------------------------------------------------------

// TestSSO_ConnectionSubFields_StoredAsIs verifies how the exporter stores a
// realistic SAML connection map that includes potentially sensitive IdP fields
// such as x509_signing_cert, idp_metadata_xml, and client_secret.
//
// FINDING (hardening gap): The exporter copies the connection map verbatim into
// the manifest without redacting any fields. Sensitive IdP material such as
// x509_signing_cert, idp_metadata_xml, and any client_secret value will appear
// in plaintext in the exported manifest. This is intentional "reference-only"
// behavior (the manifest is never written back) but operators should be aware
// that the manifest file itself must be treated as sensitive when SSO is
// configured. A future hardening step could redact or flag these fields.
func TestSSO_ConnectionSubFields_StoredAsIs(t *testing.T) {
	// Realistic SAML connection body as returned by the CircleCI SSO API.
	conn := map[string]any{
		"realm":              "acme-saml",
		"idp":                "okta",
		"sign_in_endpoint":   "https://idp.example.com/sso/saml",
		"idp_entity_id":      "https://idp.example.com/entity",
		"x509_signing_cert":  "-----BEGIN CERTIFICATE-----\nMIIDXTCCAk...base64cert\n-----END CERTIFICATE-----",
		"idp_metadata_xml":   "<EntityDescriptor entityID=\"https://idp.example.com/entity\">...</EntityDescriptor>",
		"client_secret":      "super-secret-oauth-client-secret",
		"sign_authn_request": true,
		"allowed_domains":    []any{"example.com", "acme.io"},
	}

	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil || m.Source.Org.Settings.SSO == nil {
		t.Fatal("SSO must be populated")
	}
	sso := m.Source.Org.Settings.SSO

	// Verify realm is correctly extracted from the connection map.
	if sso.Realm != "acme-saml" {
		t.Errorf("SSO.Realm: got %q want %q", sso.Realm, "acme-saml")
	}

	// Document current behavior: sensitive fields are stored verbatim.
	// This is a finding: the manifest may contain sensitive IdP material.
	storedCert, _ := sso.Connection["x509_signing_cert"].(string)
	if !strings.HasPrefix(storedCert, "-----BEGIN CERTIFICATE-----") {
		t.Errorf("x509_signing_cert not stored as-is (current behavior check failed): %q", storedCert)
	}

	storedSecret, _ := sso.Connection["client_secret"].(string)
	if storedSecret == "" {
		t.Error("client_secret: expected non-empty (current behavior is to store verbatim, no redaction)")
	}
	// NOTE: This documents a HARDENING GAP. The exporter does NOT redact
	// client_secret or x509_signing_cert. If redaction is added in the future
	// this assertion should be updated to expect a redacted sentinel value.
	if storedSecret != "super-secret-oauth-client-secret" {
		t.Logf("client_secret value changed — possible redaction was added: %q", storedSecret)
	}

	storedXML, _ := sso.Connection["idp_metadata_xml"].(string)
	if !strings.Contains(storedXML, "EntityDescriptor") {
		t.Errorf("idp_metadata_xml not stored as-is: %q", storedXML)
	}

	// Non-sensitive structural fields should also be preserved.
	if sso.Connection["sign_authn_request"] != true {
		t.Errorf("sign_authn_request: got %v want true", sso.Connection["sign_authn_request"])
	}
}

// TestSSO_RealmExtraction_NoRealmKey verifies that when the connection map does
// not contain a "realm" key, SSO.Realm is left empty (no panic, no false value).
func TestSSO_RealmExtraction_NoRealmKey(t *testing.T) {
	conn := map[string]any{
		"idp_entity_id": "https://idp.example.com/entity",
		"sign_in_url":   "https://idp.example.com/sso",
		// no "realm" key
	}
	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sso := m.Source.Org.Settings.SSO
	if sso == nil {
		t.Fatal("SSO must not be nil when enforced=true and connection present")
	}
	if sso.Realm != "" {
		t.Errorf("SSO.Realm should be empty when connection has no 'realm' key, got %q", sso.Realm)
	}
	// Connection body itself is still stored.
	if sso.Connection["idp_entity_id"] != "https://idp.example.com/entity" {
		t.Errorf("SSO.Connection not stored when realm is absent: %v", sso.Connection)
	}
}

// TestSSO_RealmExtraction_NonStringRealm verifies that when the "realm" value is
// not a string (e.g., a number), SSO.Realm is left empty and no panic occurs.
func TestSSO_RealmExtraction_NonStringRealm(t *testing.T) {
	conn := map[string]any{
		"realm": 42, // wrong type — realm key exists but is not a string
	}
	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sso := m.Source.Org.Settings.SSO
	if sso == nil {
		t.Fatal("SSO must not be nil when enforced=true and connection present")
	}
	if sso.Realm != "" {
		t.Errorf("SSO.Realm should be empty for non-string realm, got %q", sso.Realm)
	}
}

// ---------------------------------------------------------------------------
// Scenario 4: Connection present but enforcement OFF
// ---------------------------------------------------------------------------

// TestSSO_ConnectionOnly_NoEnforcement verifies that when a SAML connection
// exists but enforcement is off, exportSSO still records the SSO state
// (connection alone is "worth recording") and returns true.
func TestSSO_ConnectionOnly_NoEnforcement(t *testing.T) {
	conn := map[string]any{
		"realm":            "acme-saml",
		"sign_in_endpoint": "https://idp.example.com/sso",
	}

	ex := ssoExporter(
		func(string) (bool, error) { return false, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil || m.Source.Org.Settings.SSO == nil {
		t.Fatal("SSO must be recorded even when enforcement is off but a connection exists")
	}
	sso := m.Source.Org.Settings.SSO
	if sso.Enforced {
		t.Error("SSO.Enforced must be false when enforcement is off")
	}
	if sso.Realm != "acme-saml" {
		t.Errorf("SSO.Realm: got %q want %q", sso.Realm, "acme-saml")
	}
}

// ---------------------------------------------------------------------------
// Scenario 5: Error handling — individual API failures
// ---------------------------------------------------------------------------

// TestSSO_GetSSOEnforced_Error_AddsWarning verifies that when GetSSOEnforced
// returns an error, a warning with code "sso_unreadable" is added and the export
// still completes (does not return an error from Export).
func TestSSO_GetSSOEnforced_Error_AddsWarning(t *testing.T) {
	enforcedErr := errors.New("SSO enforcement API unavailable (503)")

	ex := ssoExporter(
		func(string) (bool, error) { return false, enforcedErr },
		// Connection succeeds (to confirm export continues regardless).
		func(string) (map[string]any, bool, error) { return nil, false, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("Export must not fail when GetSSOEnforced errors, got: %v", err)
	}

	var found bool
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			found = true
			if !strings.Contains(w.Message, "503") && !strings.Contains(w.Message, "unavailable") {
				t.Errorf("warning message does not mention the original error: %q", w.Message)
			}
		}
	}
	if !found {
		t.Error("expected sso_unreadable warning when GetSSOEnforced errors, not found")
	}
}

// TestSSO_GetSSOConnection_NonNotFoundError_AddsWarning verifies that when
// GetSSOConnection returns a non-404 error (e.g., 500 server error), a warning
// with code "sso_unreadable" is added and the export continues.
func TestSSO_GetSSOConnection_NonNotFoundError_AddsWarning(t *testing.T) {
	connErr := errors.New("SSO connection API 500: internal server error")

	ex := ssoExporter(
		// Enforcement succeeds.
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return nil, false, connErr },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("Export must not fail when GetSSOConnection errors, got: %v", err)
	}

	var found bool
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			found = true
		}
	}
	if !found {
		t.Error("expected sso_unreadable warning when GetSSOConnection returns non-404 error")
	}

	// Even though connection errored, enforcement=true means SSO is still recorded.
	if m.Source.Org.Settings == nil || m.Source.Org.Settings.SSO == nil {
		t.Fatal("SSO must still be recorded when enforcement=true even if connection errored")
	}
	if !m.Source.Org.Settings.SSO.Enforced {
		t.Error("SSO.Enforced must be true (enforcement read succeeded)")
	}
	if m.Source.Org.Settings.SSO.Connection != nil {
		t.Errorf("SSO.Connection must be nil when GetSSOConnection errored, got %v",
			m.Source.Org.Settings.SSO.Connection)
	}
}

// TestSSO_BothAPIsError_TwoWarnings verifies that when both GetSSOEnforced and
// GetSSOConnection return errors, two "sso_unreadable" warnings are added
// (one per call site) and the export continues.
func TestSSO_BothAPIsError_TwoWarnings(t *testing.T) {
	ex := ssoExporter(
		func(string) (bool, error) { return false, errors.New("enforced API down") },
		func(string) (map[string]any, bool, error) { return nil, false, errors.New("connection API down") },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("Export must not fail on SSO errors, got: %v", err)
	}

	var count int
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			count++
		}
	}
	if count < 2 {
		t.Errorf("expected at least 2 sso_unreadable warnings (one per API error), got %d", count)
	}
}

// TestSSO_GetSSOEnforced_Error_ConnectionSucceeds_NilSSO verifies the edge case
// where GetSSOEnforced errors (enforced defaults to false) and GetSSOConnection
// returns not-found: since neither enforced nor found is true, SSO must remain nil.
func TestSSO_GetSSOEnforced_Error_ConnectionNotFound_NilSSO(t *testing.T) {
	ex := ssoExporter(
		func(string) (bool, error) { return false, errors.New("enforced API down") },
		func(string) (map[string]any, bool, error) { return nil, false, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Warning must be present.
	var warnFound bool
	for _, w := range m.Warnings {
		if w.Code == "sso_unreadable" {
			warnFound = true
		}
	}
	if !warnFound {
		t.Error("expected sso_unreadable warning")
	}

	// SSO must be nil since neither enforcement was confirmed nor connection found.
	if m.Source.Org.Settings != nil && m.Source.Org.Settings.SSO != nil {
		t.Errorf("SSO must be nil when enforced=false (defaulted after error) and no connection, got %+v",
			m.Source.Org.Settings.SSO)
	}
}

// ---------------------------------------------------------------------------
// Scenario 6: OrgID is empty — SSO not attempted
// ---------------------------------------------------------------------------

// TestSSO_NotCalledWhenOrgIDEmpty verifies that when the resolved org has an
// empty ID (no UUID), exportSSO is never invoked (the org-settings block skips
// all UUID-keyed sub-reads including SSO).
func TestSSO_NotCalledWhenOrgIDEmpty(t *testing.T) {
	ssoCalled := false
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) {
				return &org.Organization{
					// ID intentionally empty — simulates an org that resolved without UUID.
					ID:      "",
					Name:    "myorg",
					Slug:    "gh/myorg",
					VCSType: "github",
				}, nil
			},
			getSSOEnforced: func(string) (bool, error) {
				ssoCalled = true
				return true, nil
			},
			getSSOConnection: func(string) (map[string]any, bool, error) {
				ssoCalled = true
				return nil, false, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	_, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ssoCalled {
		t.Error("GetSSOEnforced/GetSSOConnection must NOT be called when org has no UUID")
	}
}

// ---------------------------------------------------------------------------
// Scenario 7: Enforced=true, no connection (404) — SSO recorded with no realm
// ---------------------------------------------------------------------------

// TestSSO_EnforcedOnly_NoConnection verifies that when enforcement is on but the
// connection endpoint returns not-found (found=false), the SSO block is still
// recorded (enforced=true is "worth recording"), Connection is nil, Realm is "".
func TestSSO_EnforcedOnly_NoConnection(t *testing.T) {
	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return nil, false, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil || m.Source.Org.Settings.SSO == nil {
		t.Fatal("SSO must be recorded when enforced=true even with no connection")
	}
	sso := m.Source.Org.Settings.SSO
	if !sso.Enforced {
		t.Error("SSO.Enforced must be true")
	}
	if sso.Realm != "" {
		t.Errorf("SSO.Realm should be empty when no connection, got %q", sso.Realm)
	}
	if sso.Connection != nil {
		t.Errorf("SSO.Connection should be nil when not found, got %v", sso.Connection)
	}
}

// ---------------------------------------------------------------------------
// Scenario 8: Large / realistic SAML metadata stored correctly
// ---------------------------------------------------------------------------

// TestSSO_FullSAMLConnectionBody verifies that a comprehensive SAML connection
// body (as might be returned for a Okta/ADFS/PingFederate integration) is stored
// intact in the manifest. This test also records all fields so a reader can see
// exactly what the CircleCI SSO API may return.
func TestSSO_FullSAMLConnectionBody(t *testing.T) {
	conn := map[string]any{
		// Standard SAML identification fields.
		"realm":              "big-corp-saml",
		"idp":                "adfs",
		"idp_entity_id":      "https://adfs.bigcorp.example/adfs/services/trust",
		"sign_in_endpoint":   "https://adfs.bigcorp.example/adfs/ls/",
		"sign_out_endpoint":  "https://adfs.bigcorp.example/adfs/ls/?wa=wsignout1.0",
		"sign_authn_request": true,
		// Certificate material — potentially sensitive.
		"x509_signing_cert": "MIIC6DCCAdCgAwIBAgIQW2...truncated==",
		// Metadata blob — may contain multiple certs and endpoints.
		"idp_metadata_xml": `<?xml version="1.0"?><md:EntityDescriptor xmlns:md="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://adfs.bigcorp.example/adfs/services/trust">...</md:EntityDescriptor>`,
		// Attribute mappings.
		"email_attribute":      "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress",
		"first_name_attribute": "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/givenname",
		"last_name_attribute":  "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/surname",
		"groups_attribute":     "http://schemas.microsoft.com/ws/2008/06/identity/claims/groups",
		// Domain allowlist.
		"allowed_domains": []any{"bigcorp.example", "bigcorp.co.uk"},
		// Numeric fields.
		"session_duration_seconds": float64(28800),
	}

	ex := ssoExporter(
		func(string) (bool, error) { return true, nil },
		func(string) (map[string]any, bool, error) { return conn, true, nil },
	)

	m, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sso := m.Source.Org.Settings.SSO
	if sso == nil {
		t.Fatal("SSO must not be nil")
	}
	if sso.Realm != "big-corp-saml" {
		t.Errorf("SSO.Realm: got %q want %q", sso.Realm, "big-corp-saml")
	}

	wantKeys := []string{
		"realm", "idp", "idp_entity_id", "sign_in_endpoint", "sign_out_endpoint",
		"sign_authn_request", "x509_signing_cert", "idp_metadata_xml",
		"email_attribute", "first_name_attribute", "last_name_attribute",
		"groups_attribute", "allowed_domains", "session_duration_seconds",
	}
	for _, k := range wantKeys {
		if _, ok := sso.Connection[k]; !ok {
			t.Errorf("SSO.Connection missing expected key %q", k)
		}
	}

	// Verify numeric field round-trip.
	if sso.Connection["session_duration_seconds"] != float64(28800) {
		t.Errorf("session_duration_seconds: got %v want 28800.0", sso.Connection["session_duration_seconds"])
	}
}

// ---------------------------------------------------------------------------
// Scenario 9: OrgID is called with the correct value
// ---------------------------------------------------------------------------

// TestSSO_OrgIDPassedCorrectly verifies that both GetSSOEnforced and
// GetSSOConnection are called with the resolved org UUID (not the slug).
func TestSSO_OrgIDPassedCorrectly(t *testing.T) {
	const wantOrgID = "org-uuid-123" // matches defaultOrg().ID

	var enforcedCalledWith, connectionCalledWith string

	ex := ssoExporter(
		func(orgID string) (bool, error) {
			enforcedCalledWith = orgID
			return false, nil
		},
		func(orgID string) (map[string]any, bool, error) {
			connectionCalledWith = orgID
			return nil, false, nil
		},
	)

	_, err := ex.Export(ssoOpts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if enforcedCalledWith != wantOrgID {
		t.Errorf("GetSSOEnforced called with %q, want %q", enforcedCalledWith, wantOrgID)
	}
	if connectionCalledWith != wantOrgID {
		t.Errorf("GetSSOConnection called with %q, want %q", connectionCalledWith, wantOrgID)
	}
}
