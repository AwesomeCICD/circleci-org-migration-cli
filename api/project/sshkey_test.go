package project

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestListAdditionalSSHKeys_HappyPath verifies that the client parses the
// ssh_keys array from the v1.1 settings response and builds SSHKeyMeta values.
func TestListAdditionalSSHKeys_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v1.1/project/gh/acme/web/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		if got := r.URL.Query().Get("ssh-key-digest"); got != "sha256" {
			t.Errorf("ssh-key-digest query param: got %q, want sha256", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"ssh_keys": []map[string]any{
				{
					"hostname":    "github.com",
					"fingerprint": "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=",
					"public_key":  "ssh-rsa AAAAB3NzaC1yc2EAAAA... user@host",
				},
				{
					"hostname":    "bitbucket.org",
					"fingerprint": "XYZabc123=",
					"public_key":  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA...",
				},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	keys, err := c.ListAdditionalSSHKeys(slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}

	k0 := keys[0]
	if k0.Hostname != "github.com" {
		t.Errorf("Hostname[0]: got %q, want github.com", k0.Hostname)
	}
	if k0.Fingerprint != "Cv1BbZPFHMZzCPx+1CsJqO0kRBIlOm7DEqR/jPbHnBg=" {
		t.Errorf("Fingerprint[0]: got %q", k0.Fingerprint)
	}
	if k0.PublicKey != "ssh-rsa AAAAB3NzaC1yc2EAAAA... user@host" {
		t.Errorf("PublicKey[0]: got %q", k0.PublicKey)
	}

	k1 := keys[1]
	if k1.Hostname != "bitbucket.org" {
		t.Errorf("Hostname[1]: got %q, want bitbucket.org", k1.Hostname)
	}
}

// TestListAdditionalSSHKeys_EmptyList verifies that an empty ssh_keys array
// returns nil (not an error).
func TestListAdditionalSSHKeys_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"ssh_keys":      []any{},
			"feature_flags": map[string]any{},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	keys, err := c.ListAdditionalSSHKeys("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

// TestListAdditionalSSHKeys_MissingSSHKeysField verifies that a response body
// without the ssh_keys field (other settings endpoints) returns nil, nil.
func TestListAdditionalSSHKeys_MissingSSHKeysField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{
			"feature_flags": map[string]any{"api-trigger-with-config": true},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	keys, err := c.ListAdditionalSSHKeys("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

// TestListAdditionalSSHKeys_StandaloneSlug verifies that the circleci/<uuid>/<uuid>
// slug form is correctly encoded in the request path.
func TestListAdditionalSSHKeys_StandaloneSlug(t *testing.T) {
	const orgUUID = "org-uuid-123"
	const projUUID = "proj-uuid-456"
	slug := "circleci/" + orgUUID + "/" + projUUID

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/circleci/" + orgUUID + "/" + projUUID + "/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"ssh_keys": []map[string]any{
				{"hostname": "test", "fingerprint": "fp1=", "public_key": "ssh-rsa AAAA test@host"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	keys, err := c.ListAdditionalSSHKeys(slug)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}
	if keys[0].Hostname != "test" {
		t.Errorf("Hostname: got %q, want test", keys[0].Hostname)
	}
}

// TestListAdditionalSSHKeys_APIError verifies that a non-2xx response
// returns an error.
func TestListAdditionalSSHKeys_APIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListAdditionalSSHKeys("gh/acme/web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestListAdditionalSSHKeys_SlugEncoding verifies that a slug with spaces in
// the org/repo name is percent-encoded on the wire.
func TestListAdditionalSSHKeys_SlugEncoding(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/gh/my%20org/my%20repo/settings"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("path: got %q want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, map[string]any{"ssh_keys": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListAdditionalSSHKeys("gh/my org/my repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
