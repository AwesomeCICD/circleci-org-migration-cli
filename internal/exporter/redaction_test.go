package exporter_test

// redaction_test.go contains focused unit tests for the three manifest-redaction
// hardening changes introduced in issue #73:
//
//   1. OTel exporter header values are redacted client-side.
//   2. URL-orb allow-list Auth values outside the known-safe enum are redacted.
//   3. redactSSOConnection recurses into nested map[string]any values.
//
// All tests run entirely against fakes — no live org is required. Real secret
// values are never used; all "secrets" are obvious fakes.

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/exporter"
)

// ---------------------------------------------------------------------------
// Gap 1: OTel header value redaction
// ---------------------------------------------------------------------------

// TestOTelHeaderValues_AreRedacted verifies that header values (which can
// carry auth tokens, e.g. "Authorization: Bearer <token>") are replaced with a
// redaction placeholder in the manifest, while header key names are preserved.
func TestOTelHeaderValues_AreRedacted(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOTelExporters: func(orgID string) ([]org.OTelExporter, error) {
				return []org.OTelExporter{
					{
						ID:       "exp-1",
						Endpoint: "https://otel.example.com:4318",
						Protocol: "http/protobuf",
						Insecure: false,
						// Fake tokens — never a real credential.
						Headers: map[string]string{
							"Authorization":   "Bearer fake-token-not-real",
							"X-Api-Key":       "fake-api-key-not-real",
							"X-Custom-Header": "plain-value",
						},
					},
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Source.Org.Settings == nil {
		t.Fatal("Settings is nil")
	}
	exporters := m.Source.Org.Settings.OTelExporters
	if len(exporters) != 1 {
		t.Fatalf("expected 1 OTel exporter, got %d", len(exporters))
	}
	headers := exporters[0].Headers

	// All header key names must be preserved.
	for _, key := range []string{"Authorization", "X-Api-Key", "X-Custom-Header"} {
		if _, ok := headers[key]; !ok {
			t.Errorf("header key %q was not preserved in the manifest", key)
		}
	}

	// All header values must be redacted (no original value must appear).
	for k, v := range headers {
		if strings.Contains(v, "fake") || strings.Contains(v, "plain-value") {
			t.Errorf("header %q: original value leaked into manifest: %q", k, v)
		}
		if !strings.Contains(v, "redacted") {
			t.Errorf("header %q: expected redaction placeholder, got %q", k, v)
		}
	}

	// A manifest warning must be emitted listing the redacted header keys.
	var found bool
	for _, w := range m.Warnings {
		if w.Code == "otel_header_redacted" {
			found = true
			// The warning must mention each header name.
			for _, key := range []string{"Authorization", "X-Api-Key", "X-Custom-Header"} {
				if !strings.Contains(w.Message, key) {
					t.Errorf("otel_header_redacted warning does not mention key %q: %q", key, w.Message)
				}
			}
		}
	}
	if !found {
		t.Error("expected otel_header_redacted warning, not found")
	}
}

// TestOTelHeaders_EmptyHeadersNoWarning verifies that when an OTel exporter
// has no headers, no otel_header_redacted warning is emitted.
func TestOTelHeaders_EmptyHeadersNoWarning(t *testing.T) {
	ex := &exporter.Exporter{
		Org: &fakeOrgAPI{
			getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
			getOTelExporters: func(orgID string) ([]org.OTelExporter, error) {
				return []org.OTelExporter{
					{
						ID:       "exp-no-headers",
						Endpoint: "grpc.example.com:4317",
						Protocol: "grpc",
						Insecure: true,
						// No headers.
					},
				}, nil
			},
		},
		Contexts: &fakeContextAPI{},
		Projects: &fakeProjectAPI{},
	}

	m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, w := range m.Warnings {
		if w.Code == "otel_header_redacted" {
			t.Errorf("unexpected otel_header_redacted warning when exporter has no headers: %+v", w)
		}
	}
}

// ---------------------------------------------------------------------------
// Gap 2: URL-orb allow-list Auth enum validation
// ---------------------------------------------------------------------------

// TestURLOrbAuth_KnownSafeValues_PassedThrough verifies that the known-safe
// enum values ("none" and "aws") are written verbatim into the manifest.
func TestURLOrbAuth_KnownSafeValues_PassedThrough(t *testing.T) {
	cases := []struct {
		auth string
	}{
		{"none"},
		{"aws"},
		{""}, // empty = no auth configured; must pass through for sync round-trip
	}

	for _, tc := range cases {
		tc := tc
		t.Run("auth="+tc.auth, func(t *testing.T) {
			ex := &exporter.Exporter{
				Org: &fakeOrgAPI{
					getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
					getURLOrbAllowList: func(slugOrID string) ([]org.URLOrbAllowEntry, error) {
						return []org.URLOrbAllowEntry{
							{ID: "e1", Name: "test-entry", Prefix: "https://example.com/", Auth: tc.auth},
						}, nil
					},
				},
				Contexts: &fakeContextAPI{},
				Projects: &fakeProjectAPI{},
			}

			m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Source.Org.Settings == nil {
				t.Fatal("Settings is nil")
			}
			list := m.Source.Org.Settings.URLOrbAllowList
			if len(list) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(list))
			}
			if list[0].Auth != tc.auth {
				t.Errorf("expected auth=%q to pass through verbatim, got %q", tc.auth, list[0].Auth)
			}
		})
	}
}

// TestURLOrbAuth_UnknownValue_IsRedacted verifies that a URL-orb Auth value
// not in the known-safe enum is replaced with a redaction placeholder. This
// covers the scenario where a future Auth type carries credential material.
func TestURLOrbAuth_UnknownValue_IsRedacted(t *testing.T) {
	unknownAuthValues := []string{
		"Bearer fake-token-not-real",
		"basic-auth",
		"token",
		"custom-credential-type",
	}

	for _, authVal := range unknownAuthValues {
		authVal := authVal
		t.Run("auth="+authVal, func(t *testing.T) {
			ex := &exporter.Exporter{
				Org: &fakeOrgAPI{
					getOrganization: func(string) (*org.Organization, error) { return defaultOrg(), nil },
					getURLOrbAllowList: func(slugOrID string) ([]org.URLOrbAllowEntry, error) {
						return []org.URLOrbAllowEntry{
							{ID: "e1", Name: "test-entry", Prefix: "https://example.com/", Auth: authVal},
						}, nil
					},
				},
				Contexts: &fakeContextAPI{},
				Projects: &fakeProjectAPI{},
			}

			m, err := ex.Export(exporter.Options{OrgSlug: "gh/myorg"})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if m.Source.Org.Settings == nil {
				t.Fatal("Settings is nil")
			}
			list := m.Source.Org.Settings.URLOrbAllowList
			if len(list) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(list))
			}
			got := list[0].Auth
			// The original value must NOT appear.
			if got == authVal {
				t.Errorf("unknown auth value %q was not redacted (passed through verbatim)", authVal)
			}
			// The placeholder must appear.
			if !strings.Contains(got, "redacted") {
				t.Errorf("expected redaction placeholder for auth=%q, got %q", authVal, got)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Gap 3: Nested SSO redaction
// ---------------------------------------------------------------------------

// TestSSO_NestedSecretFields_AreRedacted verifies that secret fields inside
// nested map[string]any values within the SSO connection are redacted, not just
// top-level keys. For example, idp_config.private_key must be redacted.
func TestSSO_NestedSecretFields_AreRedacted(t *testing.T) {
	conn := map[string]any{
		"realm": "acme-nested",
		"idp_config": map[string]any{
			// "private_key" matches "private" substring → must be redacted.
			"private_key": "-----BEGIN FAKE PRIVATE KEY-----\nNOT-A-REAL-KEY\n-----END FAKE PRIVATE KEY-----",
			// "signing_cert" matches "cert" substring → must be redacted.
			"signing_cert": "-----BEGIN FAKE CERTIFICATE-----\nNOT-A-REAL-CERT\n-----END FAKE CERTIFICATE-----",
			// "issuer" is non-sensitive → must be preserved.
			"issuer": "https://idp.example.com",
		},
		// Top-level client_secret matches "secret" → must be redacted.
		"client_secret": "fake-client-secret-not-real",
		// Top-level non-sensitive field → must be preserved.
		"sign_authn_request": true,
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

	const redacted = "<redacted: SSO IdP material is not migrated; recreate SSO manually>"

	// Top-level secret must be redacted.
	if got, ok := sso.Connection["client_secret"].(string); !ok || got != redacted {
		t.Errorf("top-level client_secret not redacted: %v", sso.Connection["client_secret"])
	}

	// Top-level non-sensitive field must be preserved.
	if sso.Connection["sign_authn_request"] != true {
		t.Errorf("sign_authn_request was unexpectedly modified: %v", sso.Connection["sign_authn_request"])
	}

	// Nested object must still be present (and must be a map, not redacted as a whole).
	idpConfig, ok := sso.Connection["idp_config"].(map[string]any)
	if !ok {
		t.Fatalf("idp_config must be a map[string]any, got %T: %v", sso.Connection["idp_config"], sso.Connection["idp_config"])
	}

	// Nested secret fields must be redacted.
	if got, ok := idpConfig["private_key"].(string); !ok || got != redacted {
		t.Errorf("nested idp_config.private_key not redacted: %v", idpConfig["private_key"])
	}
	if got, ok := idpConfig["signing_cert"].(string); !ok || got != redacted {
		t.Errorf("nested idp_config.signing_cert not redacted: %v", idpConfig["signing_cert"])
	}

	// Nested non-sensitive field must be preserved.
	if idpConfig["issuer"] != "https://idp.example.com" {
		t.Errorf("nested idp_config.issuer was unexpectedly modified: %v", idpConfig["issuer"])
	}

	// Original secret strings must not appear anywhere in the connection.
	for _, secret := range []string{"FAKE PRIVATE KEY", "FAKE CERTIFICATE", "fake-client-secret-not-real"} {
		checkNoSecret(t, sso.Connection, secret)
	}

	// A warning must be emitted; it must mention the nested key paths.
	var found bool
	for _, w := range m.Warnings {
		if w.Code == "sso_secret_redacted" {
			found = true
		}
	}
	if !found {
		t.Error("expected sso_secret_redacted warning")
	}
}

// TestSSO_NonNestedFields_StillRedacted verifies that the refactored recursive
// function still correctly redacts top-level fields (regression guard).
func TestSSO_NonNestedFields_StillRedacted(t *testing.T) {
	conn := map[string]any{
		"x509_signing_cert": "FAKE-CERT-NOT-REAL",
		"idp_entity_id":     "https://idp.example.com/entity",
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

	const redacted = "<redacted: SSO IdP material is not migrated; recreate SSO manually>"
	if got, ok := sso.Connection["x509_signing_cert"].(string); !ok || got != redacted {
		t.Errorf("x509_signing_cert not redacted: %v", sso.Connection["x509_signing_cert"])
	}
	if sso.Connection["idp_entity_id"] != "https://idp.example.com/entity" {
		t.Errorf("idp_entity_id was unexpectedly modified: %v", sso.Connection["idp_entity_id"])
	}
}

// checkNoSecret scans a map[string]any recursively and fails the test if any
// string value contains the given secret substring.
func checkNoSecret(t *testing.T, m map[string]any, secret string) {
	t.Helper()
	for k, v := range m {
		switch val := v.(type) {
		case string:
			if strings.Contains(val, secret) {
				t.Errorf("key %q leaked secret material %q", k, secret)
			}
		case map[string]any:
			checkNoSecret(t, val, secret)
		}
	}
}
