package project

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---- ListAdditionalSSHKeys --------------------------------------------------

func TestListAdditionalSSHKeys_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.EscapedPath() != "/api/v1.1/project/gh/acme/web/ssh-key" {
			t.Errorf("unexpected path: %q", r.URL.EscapedPath())
		}
		respondJSON(w, http.StatusOK, []map[string]string{
			{"hostname": "github.com", "fingerprint": "SHA256:abc123"},
			{"hostname": "", "fingerprint": "SHA256:def456"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListAdditionalSSHKeys("gh/acme/web")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(got))
	}
	if got[0].Hostname != "github.com" {
		t.Errorf("Hostname[0]: got %q, want %q", got[0].Hostname, "github.com")
	}
	if got[0].Fingerprint != "SHA256:abc123" {
		t.Errorf("Fingerprint[0]: got %q, want %q", got[0].Fingerprint, "SHA256:abc123")
	}
	if got[1].Hostname != "" {
		t.Errorf("Hostname[1]: got %q, want empty", got[1].Hostname)
	}
}

func TestListAdditionalSSHKeys_EncodedSlug(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/api/v1.1/project/circleci/org-uuid-123/proj-uuid-456/ssh-key"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("EscapedPath = %q, want %q", r.URL.EscapedPath(), wantPath)
		}
		respondJSON(w, http.StatusOK, []map[string]string{})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	keys, err := c.ListAdditionalSSHKeys("circleci/org-uuid-123/proj-uuid-456")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(keys))
	}
}

func TestListAdditionalSSHKeys_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListAdditionalSSHKeys("gh/acme/web")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestListAdditionalSSHKeys_EmptySlug(t *testing.T) {
	c := &Client{}
	_, err := c.ListAdditionalSSHKeys("")
	if err == nil {
		t.Fatal("expected error for empty slug")
	}
}

// ---- AddAdditionalSSHKey ----------------------------------------------------

func TestAddAdditionalSSHKey_HappyPath(t *testing.T) {
	const slug = "gh/acme/web"
	const hostname = "github.com"
	const privateKey = "-----BEGIN OPENSSH PRIVATE KEY-----\ntest-key-data\n-----END OPENSSH PRIVATE KEY-----"

	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		wantPath := "/api/v1.1/project/gh/acme/web/ssh-key"
		if r.URL.EscapedPath() != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.EscapedPath())
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AddAdditionalSSHKey(slug, hostname, privateKey); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedBody["hostname"] != hostname {
		t.Errorf("hostname: got %v, want %s", capturedBody["hostname"], hostname)
	}
	if capturedBody["private_key"] != privateKey {
		t.Errorf("private_key: got %v, want %s", capturedBody["private_key"], privateKey)
	}
	// Verify exactly the two expected keys are present.
	if len(capturedBody) != 2 {
		t.Errorf("body should have exactly 2 keys (hostname, private_key), got %d: %v", len(capturedBody), capturedBody)
	}
}

func TestAddAdditionalSSHKey_EmptyHostname(t *testing.T) {
	// A key with an empty hostname is valid — the API accepts it.
	var capturedBody map[string]interface{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AddAdditionalSSHKey("gh/acme/web", "", "-----BEGIN OPENSSH PRIVATE KEY-----\ndata\n-----END OPENSSH PRIVATE KEY-----"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedBody["hostname"] != "" {
		t.Errorf("hostname: got %v, want empty string", capturedBody["hostname"])
	}
}

func TestAddAdditionalSSHKey_AppSlug(t *testing.T) {
	// Verify the circleci/<orgUUID>/<projUUID> slug is encoded correctly on the wire.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := "/api/v1.1/project/circleci/org-id-abc/proj-id-def/ssh-key"
		if r.URL.EscapedPath() != want {
			t.Errorf("EscapedPath = %q, want %q", r.URL.EscapedPath(), want)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.AddAdditionalSSHKey("circleci/org-id-abc/proj-id-def", "github.com", "key-data"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAddAdditionalSSHKey_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusForbidden, map[string]string{"message": "forbidden"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.AddAdditionalSSHKey("gh/acme/web", "github.com", "key-data")
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
}

func TestAddAdditionalSSHKey_EmptySlug(t *testing.T) {
	c := &Client{}
	if err := c.AddAdditionalSSHKey("", "github.com", "key-data"); err == nil {
		t.Fatal("expected error for empty slug")
	}
}

func TestAddAdditionalSSHKey_EmptyPrivateKey(t *testing.T) {
	c := &Client{}
	if err := c.AddAdditionalSSHKey("gh/acme/web", "github.com", ""); err == nil {
		t.Fatal("expected error for empty private key")
	}
}
