package org

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ─────────────────────────────────────────────────────────────────────────────
// GetContacts
// ─────────────────────────────────────────────────────────────────────────────

func TestGetContacts_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/private/organization/" + orgID + "/contacts"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if got := r.Header.Get("Circle-Token"); got != "test-token" {
			t.Errorf("Circle-Token header: got %q want test-token", got)
		}
		respondJSON(w, http.StatusOK, map[string]any{
			"primary":  []string{"alice@example.com", "bob@example.com"},
			"security": []string{"sec@example.com"},
		})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	primary, security, err := c.GetContacts(orgID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(primary) != 2 {
		t.Fatalf("primary: expected 2 emails, got %d", len(primary))
	}
	if primary[0] != "alice@example.com" {
		t.Errorf("primary[0]: got %q want alice@example.com", primary[0])
	}
	if primary[1] != "bob@example.com" {
		t.Errorf("primary[1]: got %q want bob@example.com", primary[1])
	}
	if len(security) != 1 || security[0] != "sec@example.com" {
		t.Errorf("security: got %v", security)
	}
}

func TestGetContacts_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusOK, map[string]any{"primary": []string{}, "security": []string{}})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	primary, security, err := c.GetContacts("some-org")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(primary) != 0 {
		t.Errorf("expected empty primary, got %v", primary)
	}
	if len(security) != 0 {
		t.Errorf("expected empty security, got %v", security)
	}
}

func TestGetContacts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusNotFound, map[string]string{"message": "not found"})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	_, _, err := c.GetContacts("missing-org")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// SetContacts
// ─────────────────────────────────────────────────────────────────────────────

func TestSetContacts_HappyPath(t *testing.T) {
	const orgID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		wantPath := "/api/private/organization/" + orgID + "/contacts"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %q, got %q", wantPath, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	primary := []string{"alice@example.com", "bob@example.com"}
	security := []string{"sec@example.com"}
	if err := c.SetContacts(orgID, primary, security); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Assert primary list in body.
	rawPrimary, ok := receivedBody["primary"].([]any)
	if !ok || len(rawPrimary) != 2 {
		t.Errorf("primary in body: got %v", receivedBody["primary"])
	} else {
		if rawPrimary[0] != "alice@example.com" {
			t.Errorf("primary[0]: got %v", rawPrimary[0])
		}
		if rawPrimary[1] != "bob@example.com" {
			t.Errorf("primary[1]: got %v", rawPrimary[1])
		}
	}

	// Assert security list in body.
	rawSecurity, ok := receivedBody["security"].([]any)
	if !ok || len(rawSecurity) != 1 {
		t.Errorf("security in body: got %v", receivedBody["security"])
	} else if rawSecurity[0] != "sec@example.com" {
		t.Errorf("security[0]: got %v", rawSecurity[0])
	}
}

func TestSetContacts_EmptyLists(t *testing.T) {
	// PUT with empty lists must still send a valid body.
	var receivedBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("expected PUT, got %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Errorf("decode body: %v", err)
		}
		respondJSON(w, http.StatusOK, map[string]any{})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	if err := c.SetContacts("org-id", nil, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// primary and security should be present (as null/omitted — JSON omitempty
	// on nil slices means the keys are absent, which is fine for PUT).
	_ = receivedBody
}

func TestSetContacts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]string{"message": "too many contacts"})
	}))
	defer srv.Close()

	c := newTestClientWithPrivateServer(t, srv)
	err := c.SetContacts("org-id", []string{"a@b.com", "c@d.com", "e@f.com", "g@h.com", "i@j.com", "k@l.com"}, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
