package context

// extra_test.go adds error-path and pagination tests for the context package
// to raise coverage above 80%.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------------------------------------------------
// NewClient / newClientWithURLs error paths
// ---------------------------------------------------------------------------

func TestNewClientWithURLs_InvalidHost(t *testing.T) {
	_, err := newClientWithURLs("://bad-url", "tok", nil)
	if err == nil {
		t.Fatal("expected error for invalid host, got nil")
	}
}

func TestNewClientWithURLs_EmptyHost(t *testing.T) {
	_, err := newClientWithURLs("", "tok", nil)
	if err == nil {
		t.Fatal("expected error for empty host, got nil")
	}
}

func TestNewClientWithURLs_ValidHost_ReturnsClient(t *testing.T) {
	c, err := newClientWithURLs("https://circleci.com", "tok", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("expected non-nil client")
	}
}

// ---------------------------------------------------------------------------
// ListContexts — error paths
// ---------------------------------------------------------------------------

func TestListContexts_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"internal server error"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListContexts(context.Background(), "org-uuid", "")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestListContexts_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"message":"unauthorized"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListContexts(context.Background(), "", "github/myorg")
	if err == nil {
		t.Fatal("expected error for unauthorized, got nil")
	}
}

// ---------------------------------------------------------------------------
// ListEnvVars — error paths and pagination
// ---------------------------------------------------------------------------

func TestListEnvVars_EmptyContextID(t *testing.T) {
	c := &Client{}
	_, err := c.ListEnvVars(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty contextID, got nil")
	}
}

func TestListEnvVars_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"message":"internal error"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListEnvVars(context.Background(), "ctx-test")
	if err == nil {
		t.Fatal("expected error for server error, got nil")
	}
}

func TestListEnvVars_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []EnvVar{{Name: "VAR_A"}},
		"next_page_token": "tok2",
	}
	page2 := map[string]interface{}{
		"items":           []EnvVar{{Name: "VAR_B"}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			w.Write(jsonBody(t, page1)) //nolint:errcheck
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "tok2" {
				t.Errorf("second call: expected page-token=tok2, got %q", got)
			}
			w.Write(jsonBody(t, page2)) //nolint:errcheck
		default:
			t.Errorf("unexpected third call")
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListEnvVars(context.Background(), "ctx-paged")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(got))
	}
}

// ---------------------------------------------------------------------------
// ListRestrictions — error paths and pagination
// ---------------------------------------------------------------------------

func TestListRestrictions_EmptyContextID(t *testing.T) {
	c := &Client{}
	_, err := c.ListRestrictions(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty contextID, got nil")
	}
}

func TestListRestrictions_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"forbidden"}`)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListRestrictions(context.Background(), "ctx-403")
	if err == nil {
		t.Fatal("expected error for forbidden, got nil")
	}
}

func TestListRestrictions_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []Restriction{{ID: "r-1", Type: "project", Value: "proj-1"}},
		"next_page_token": "tokR2",
	}
	page2 := map[string]interface{}{
		"items":           []Restriction{{ID: "r-2", Type: "expression", Value: "main"}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			w.Write(jsonBody(t, page1)) //nolint:errcheck
		case 2:
			if got := r.URL.Query().Get("page-token"); got != "tokR2" {
				t.Errorf("expected page-token=tokR2, got %q", got)
			}
			w.Write(jsonBody(t, page2)) //nolint:errcheck
		default:
			t.Errorf("unexpected third call")
			http.Error(w, "unexpected", http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListRestrictions(context.Background(), "ctx-paginated")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 restrictions, got %d", len(got))
	}
}
