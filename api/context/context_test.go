package context

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/api/rest"
)

// ---- helpers ----------------------------------------------------------------

// newTestClient wires a Client whose REST base and GraphQL base both point to
// the given httptest server.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	srvURL, _ := url.Parse(srv.URL)
	restBase, _ := srvURL.Parse("api/v2/")
	gqlBase, _ := srvURL.Parse("graphql-unstable")
	return &Client{
		rest:       rest.New(restBase, "test-token", srv.Client()),
		gqlBaseURL: gqlBase,
		token:      "test-token",
		httpClient: srv.Client(),
	}
}

func jsonBody(t *testing.T, v interface{}) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return b
}

// ---- ListContexts -----------------------------------------------------------

func TestListContexts_OwnerID(t *testing.T) {
	wantCtx := Context{ID: "ctx-1", Name: "production", CreatedAt: "2024-01-01T00:00:00Z"}
	page := map[string]interface{}{
		"items":           []Context{wantCtx},
		"next_page_token": "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v2/context" {
			t.Errorf("expected path /api/v2/context, got %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("owner-id"); got != "org-uuid" {
			t.Errorf("expected owner-id=org-uuid, got %q", got)
		}
		if r.URL.Query().Get("owner-slug") != "" {
			t.Errorf("owner-slug should not be set when owner-id is provided")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBody(t, page)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListContexts("org-uuid", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 context, got %d", len(got))
	}
	if got[0] != wantCtx {
		t.Errorf("context mismatch: got %+v, want %+v", got[0], wantCtx)
	}
}

func TestListContexts_OwnerSlug(t *testing.T) {
	wantCtx := Context{ID: "ctx-2", Name: "staging", CreatedAt: "2024-02-01T00:00:00Z"}
	page := map[string]interface{}{
		"items":           []Context{wantCtx},
		"next_page_token": "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("owner-slug"); got != "github/myorg" {
			t.Errorf("expected owner-slug=github/myorg, got %q", got)
		}
		if r.URL.Query().Get("owner-id") != "" {
			t.Errorf("owner-id should not be set when ownerID is empty")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBody(t, page)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListContexts("", "github/myorg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0] != wantCtx {
		t.Errorf("unexpected result: %+v", got)
	}
}

func TestListContexts_Pagination(t *testing.T) {
	page1 := map[string]interface{}{
		"items":           []Context{{ID: "ctx-a", Name: "alpha"}},
		"next_page_token": "tok2",
	}
	page2 := map[string]interface{}{
		"items":           []Context{{ID: "ctx-b", Name: "beta"}},
		"next_page_token": "",
	}

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1:
			if r.URL.Query().Get("page-token") != "" {
				t.Errorf("first call should have no page-token")
			}
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
	got, err := c.ListContexts("org-id", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("expected 2 HTTP calls, got %d", callCount)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 contexts, got %d", len(got))
	}
	if got[0].ID != "ctx-a" || got[1].ID != "ctx-b" {
		t.Errorf("unexpected contexts: %+v", got)
	}
}

func TestListContexts_BothEmpty_Error(t *testing.T) {
	c := &Client{} // no server needed
	_, err := c.ListContexts("", "")
	if err == nil {
		t.Fatal("expected error when both ownerID and ownerSlug are empty")
	}
}

// ---- ListEnvVars ------------------------------------------------------------

func TestListEnvVars_NamesOnly(t *testing.T) {
	// The API never returns a "value" field; assert it is absent from the struct.
	vars := []EnvVar{
		{Name: "SECRET_KEY", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-02T00:00:00Z"},
		{Name: "DB_PASS", CreatedAt: "2024-02-01T00:00:00Z", UpdatedAt: "2024-02-02T00:00:00Z"},
	}
	page := map[string]interface{}{
		"items":           vars,
		"next_page_token": "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/context/ctx-123/environment-variable"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBody(t, page)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListEnvVars("ctx-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 env vars, got %d", len(got))
	}
	// Assert no "Value" field leaks – the type simply has no Value field.
	// We confirm that only Name, CreatedAt, UpdatedAt are accessible.
	if got[0].Name != "SECRET_KEY" {
		t.Errorf("expected Name=SECRET_KEY, got %q", got[0].Name)
	}
	if got[1].Name != "DB_PASS" {
		t.Errorf("expected Name=DB_PASS, got %q", got[1].Name)
	}
}

func TestListEnvVars_JSONTag_Variable(t *testing.T) {
	// Confirm Name maps to JSON key "variable" (not "name").
	raw := `{"items":[{"variable":"MY_VAR","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z"}],"next_page_token":""}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, raw)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListEnvVars("ctx-x")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Name != "MY_VAR" {
		t.Errorf("Name should decode from 'variable' JSON key; got %+v", got)
	}
}

// ---- ListRestrictions -------------------------------------------------------

func TestListRestrictions_IncludingGroup(t *testing.T) {
	restrictions := []Restriction{
		{
			ID:        "r-1",
			Type:      "project",
			Value:     "proj-uuid",
			Name:      "my-project",
			ContextID: "ctx-abc",
		},
		{
			ID:        "r-2",
			Type:      "group",
			Value:     "group-uuid",
			Name:      "",
			ContextID: "ctx-abc",
		},
		{
			ID:        "r-3",
			Type:      "expression",
			Value:     `pipeline.git.branch == "main"`,
			Name:      "",
			ContextID: "ctx-abc",
		},
	}
	page := map[string]interface{}{
		"items":           restrictions,
		"next_page_token": "",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		wantPath := "/api/v2/context/ctx-abc/restrictions"
		if r.URL.Path != wantPath {
			t.Errorf("expected path %s, got %s", wantPath, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonBody(t, page)) //nolint:errcheck
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListRestrictions("ctx-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 restrictions, got %d", len(got))
	}

	// Spot-check the group restriction.
	var groupRestr *Restriction
	for i := range got {
		if got[i].Type == "group" {
			groupRestr = &got[i]
		}
	}
	if groupRestr == nil {
		t.Fatal("group restriction not found")
		return
	}
	if groupRestr.Value != "group-uuid" {
		t.Errorf("group restriction value: got %q, want %q", groupRestr.Value, "group-uuid")
	}
	if groupRestr.ContextID != "ctx-abc" {
		t.Errorf("group restriction context_id: got %q, want %q", groupRestr.ContextID, "ctx-abc")
	}
}

func TestListRestrictions_JSONTags(t *testing.T) {
	// Verify restriction_type and restriction_value decode correctly.
	raw := `{"items":[{
		"id":"r-99",
		"restriction_type":"expression",
		"restriction_value":"pipeline.git.branch == \"main\"",
		"name":"",
		"context_id":"ctx-z"
	}],"next_page_token":""}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, raw)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListRestrictions("ctx-z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 restriction, got %d", len(got))
	}
	if got[0].Type != "expression" {
		t.Errorf("Type: got %q, want expression", got[0].Type)
	}
	if !strings.Contains(got[0].Value, "main") {
		t.Errorf("Value: got %q", got[0].Value)
	}
	if got[0].ContextID != "ctx-z" {
		t.Errorf("ContextID: got %q, want ctx-z", got[0].ContextID)
	}
}

// ---- GraphQL ----------------------------------------------------------------

func TestListOrgGroups_Success(t *testing.T) {
	respBody := `{"data":{"organization":{"groups":{"edges":[
		{"node":{"id":"grp-1","name":"team-alpha","groupType":"TEAM"}},
		{"node":{"id":"grp-2","name":"org-admins","groupType":"ORGANIZATION"}}
	]}}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/graphql-unstable" {
			t.Errorf("expected path /graphql-unstable, got %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("expected Content-Type application/json, got %q", ct)
		}
		if auth := r.Header.Get("Authorization"); auth != "test-token" {
			t.Errorf("expected Authorization header test-token, got %q", auth)
		}

		// Verify request body has query and variables.
		var body gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if !strings.Contains(body.Query, "organization") {
			t.Errorf("query should reference 'organization', got: %s", body.Query)
		}
		if body.Variables["id"] != "org-uuid-1" {
			t.Errorf("expected variable id=org-uuid-1, got %v", body.Variables["id"])
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListOrgGroups("org-uuid-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(got))
	}
	if got[0].ID != "grp-1" || got[0].Name != "team-alpha" || got[0].GroupType != "TEAM" {
		t.Errorf("unexpected group[0]: %+v", got[0])
	}
	if got[1].GroupType != "ORGANIZATION" {
		t.Errorf("unexpected group[1].GroupType: %q", got[1].GroupType)
	}
}

func TestListContextGroups_Success(t *testing.T) {
	respBody := `{"data":{"context":{"groups":{"edges":[
		{"node":{"id":"grp-10","name":"deployers","groupType":"TEAM"}}
	]}}}}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql-unstable" {
			t.Errorf("expected path /graphql-unstable, got %s", r.URL.Path)
		}
		var body gqlRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if !strings.Contains(body.Query, "context") {
			t.Errorf("query should reference 'context', got: %s", body.Query)
		}
		if body.Variables["id"] != "ctx-uuid-9" {
			t.Errorf("expected variable id=ctx-uuid-9, got %v", body.Variables["id"])
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := c.ListContextGroups("ctx-uuid-9")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 group, got %d", len(got))
	}
	if got[0].ID != "grp-10" || got[0].Name != "deployers" || got[0].GroupType != "TEAM" {
		t.Errorf("unexpected group: %+v", got[0])
	}
}

func TestListOrgGroups_GraphQLError(t *testing.T) {
	// Server returns GraphQL errors array — should surface as a Go error.
	respBody := `{"errors":[{"message":"organization not found"},{"message":"access denied"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListOrgGroups("bad-org")
	if err == nil {
		t.Fatal("expected error from GraphQL errors, got nil")
	}
	if !strings.Contains(err.Error(), "organization not found") {
		t.Errorf("error should contain 'organization not found', got: %v", err)
	}
}

func TestListContextGroups_GraphQLError(t *testing.T) {
	respBody := `{"errors":[{"message":"context not found"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, respBody)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListContextGroups("nonexistent-ctx")
	if err == nil {
		t.Fatal("expected error from GraphQL errors, got nil")
	}
	if !strings.Contains(err.Error(), "context not found") {
		t.Errorf("error should contain 'context not found', got: %v", err)
	}
}
