package orb_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/orb"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// newTestClient builds an orb.Client pointed at srv, with the API base resolved
// to srv.URL/api/v3/ — matching the orb client's URL layout.
func newTestClient(t *testing.T, srv *httptest.Server) *orb.Client {
	t.Helper()
	cfg := &settings.Config{HTTPClient: srv.Client()}
	c, err := orb.NewClientWithBase(srv.URL, "fake-token", cfg) // nosec G101 -- fake test token, not a credential
	if err != nil {
		t.Fatalf("NewClientWithBase: %v", err)
	}
	return c
}

// checkBearerAuth asserts that the request carries "Authorization: Bearer …"
// and NOT "Circle-Token", mirroring the orb API's auth requirements.
func checkBearerAuth(t *testing.T, r *http.Request) {
	t.Helper()
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		t.Errorf("expected Authorization: Bearer …, got %q", auth)
	}
	if r.Header.Get("Circle-Token") != "" {
		t.Error("Circle-Token header must NOT be set for orb API requests")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveNamespaceID
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveNamespaceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBearerAuth(t, r)
		if r.URL.Query().Get("filter[name]") != "acme" {
			t.Errorf("unexpected filter[name]: %q", r.URL.Query().Get("filter[name]"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"id":         "ns-uuid-1234",
				"attributes": map[string]any{"name": "acme"},
			},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	id, err := c.ResolveNamespaceID(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ResolveNamespaceID: %v", err)
	}
	if id != "ns-uuid-1234" {
		t.Errorf("ID = %q, want %q", id, "ns-uuid-1234")
	}
}

func TestResolveNamespaceID_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Empty data.id = not found.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"id": "", "attributes": map[string]any{"name": ""}},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ResolveNamespaceID(context.Background(), "nonexistent")
	if err == nil {
		t.Fatal("expected error for not-found namespace, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListOrbs — public + private merge and pagination
// ─────────────────────────────────────────────────────────────────────────────

// makeOrbItem builds a minimal JSON:API orb item map.
func makeOrbItem(id, name string, isPrivate bool) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"name":       name,
			"is_private": isPrivate,
			"is_listed":  !isPrivate,
		},
		"references": map[string]any{
			"namespace": map[string]any{"id": "ns-uuid"},
		},
	}
}

func TestListOrbs_PublicAndPrivateMerged(t *testing.T) {
	callCounts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vis := r.URL.Query().Get("filter[visibility]")
		callCounts[vis]++
		checkBearerAuth(t, r)

		w.Header().Set("Content-Type", "application/json")
		switch vis {
		case "":
			// Public call: return one public orb.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("orb-pub-1", "pub-orb", false)},
				"next_page_token": "",
			})
		case "private":
			// Private call: return one private orb.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("orb-priv-1", "priv-orb", true)},
				"next_page_token": "",
			})
		default:
			t.Errorf("unexpected filter[visibility]=%q", vis)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	orbs, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err != nil {
		t.Fatalf("ListOrbs: %v", err)
	}
	if len(orbs) != 2 {
		t.Fatalf("expected 2 orbs (1 public + 1 private), got %d", len(orbs))
	}
	if callCounts[""] != 1 {
		t.Errorf("expected 1 public call, got %d", callCounts[""])
	}
	if callCounts["private"] != 1 {
		t.Errorf("expected 1 private call, got %d", callCounts["private"])
	}
}

func TestListOrbs_Pagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vis := r.URL.Query().Get("filter[visibility]")
		if vis == "private" {
			// Private: empty.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}, "next_page_token": ""})
			return
		}
		// Public: two pages.
		page++
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("orb-1", "orb-one", false)},
				"next_page_token": "cursor-page-2",
			})
		default:
			token := r.URL.Query().Get("page[token]")
			if token != "cursor-page-2" {
				t.Errorf("expected page[token]=cursor-page-2, got %q", token)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("orb-2", "orb-two", false)},
				"next_page_token": "",
			})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	orbs, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err != nil {
		t.Fatalf("ListOrbs: %v", err)
	}
	if len(orbs) != 2 {
		t.Fatalf("expected 2 orbs across pages, got %d: %+v", len(orbs), orbs)
	}
}

func TestListOrbs_DeduplicatesOverlap(t *testing.T) {
	// When the same orb appears in both public and private calls, it should
	// only appear once in the merged result.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Both calls return the same orb ID.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":            []any{makeOrbItem("orb-shared", "shared-orb", false)},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	orbs, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err != nil {
		t.Fatalf("ListOrbs: %v", err)
	}
	if len(orbs) != 1 {
		t.Fatalf("expected 1 deduplicated orb, got %d", len(orbs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListStableVersions
// ─────────────────────────────────────────────────────────────────────────────

func makeVersionItem(id, version string) map[string]any {
	return map[string]any{
		"id": id,
		"attributes": map[string]any{
			"version":    version,
			"created_at": "2026-01-01T00:00:00Z",
		},
	}
}

func TestListStableVersions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBearerAuth(t, r)
		if r.URL.Query().Get("filter[channel]") != "stable" {
			t.Errorf("expected filter[channel]=stable, got %q", r.URL.Query().Get("filter[channel]"))
		}
		if r.URL.Query().Get("filter[orb_id]") != "orb-uuid-123" {
			t.Errorf("unexpected filter[orb_id]: %q", r.URL.Query().Get("filter[orb_id]"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{
				makeVersionItem("ver-1", "1.0.0"),
				makeVersionItem("ver-2", "1.1.0"),
			},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	versions, err := c.ListStableVersions(context.Background(), "orb-uuid-123")
	if err != nil {
		t.Fatalf("ListStableVersions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
	if versions[0].Version != "1.0.0" {
		t.Errorf("versions[0].Version = %q, want %q", versions[0].Version, "1.0.0")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetSource — text/plain body
// ─────────────────────────────────────────────────────────────────────────────

func TestGetSource(t *testing.T) {
	const wantSource = "version: 2.1\norbs:\n  hello: circleci/hello-build@1.0.0\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBearerAuth(t, r)
		if !strings.HasSuffix(r.URL.Path, "/source") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, wantSource)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	src, err := c.GetSource(context.Background(), "ver-uuid-abc")
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if src != wantSource {
		t.Errorf("source = %q, want %q", src, wantSource)
	}
}

func TestGetSource_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.GetSource(context.Background(), "missing-ver")
	if err == nil {
		t.Fatal("expected error for 404, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveVersionRef
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveVersionRef_Found(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ref := r.URL.Query().Get("filter[ref]")
		if ref != "acme/my-orb@1.2.3" {
			t.Errorf("unexpected filter[ref]: %q", ref)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{makeVersionItem("ver-abc", "1.2.3")},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ver, err := c.ResolveVersionRef(context.Background(), "acme/my-orb@1.2.3")
	if err != nil {
		t.Fatalf("ResolveVersionRef: %v", err)
	}
	if ver == nil {
		t.Fatal("expected non-nil version, got nil")
	}
	if ver.Version != "1.2.3" {
		t.Errorf("ver.Version = %q, want %q", ver.Version, "1.2.3")
	}
}

func TestResolveVersionRef_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ver, err := c.ResolveVersionRef(context.Background(), "acme/my-orb@9.9.9")
	if err != nil {
		t.Fatalf("ResolveVersionRef: %v", err)
	}
	if ver != nil {
		t.Errorf("expected nil version for not-found, got %+v", ver)
	}
}

// TestResolveVersionRef_NotFound404 covers the live behavior: a missing ref
// returns HTTP 404 with an {"error":{"title":"not found"}} body (NOT a 200 with
// empty data). This must map to (nil, nil) so the syncer publishes the version
// rather than treating the lookup as a hard error.
func TestResolveVersionRef_NotFound404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"id": "abc", "title": "not found"},
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	ver, err := c.ResolveVersionRef(context.Background(), "acme/my-orb@9.9.9")
	if err != nil {
		t.Fatalf("ResolveVersionRef: 404 should map to not-found, got err: %v", err)
	}
	if ver != nil {
		t.Errorf("expected nil version for 404 not-found, got %+v", ver)
	}
}

// TestListOrbs_StripsNamespacePrefix verifies that the full "<ns>/<orb>" name
// the API returns in attributes.name is reduced to the short orb name, which is
// what OrbPackage.Name is documented to hold (and what CreateOrb/resolve match
// against). A regression here doubles the namespace on export.
func TestListOrbs_StripsNamespacePrefix(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("filter[visibility]") == "private" {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{
			map[string]any{
				"id":         "orb-1",
				"attributes": map[string]any{"name": "acme/my-orb", "is_private": false},
				"references": map[string]any{"namespace": map[string]any{"id": "ns-1"}},
			},
		}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	orbs, err := c.ListOrbs(context.Background(), "ns-1")
	if err != nil {
		t.Fatalf("ListOrbs: %v", err)
	}
	if len(orbs) != 1 {
		t.Fatalf("expected 1 orb, got %d", len(orbs))
	}
	if orbs[0].Name != "my-orb" {
		t.Errorf("OrbPackage.Name = %q; want short name %q", orbs[0].Name, "my-orb")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateOrb — idempotency (409 conflict path)
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateOrb_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": makeOrbItem("new-orb-id", "my-orb", false),
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	pkg, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
	if err != nil {
		t.Fatalf("CreateOrb: %v", err)
	}
	if pkg.ID != "new-orb-id" {
		t.Errorf("pkg.ID = %q, want %q", pkg.ID, "new-orb-id")
	}
}

func TestCreateOrb_AlreadyExists_Idempotent(t *testing.T) {
	// Simulate: POST returns 409; subsequent list returns the existing orb.
	listCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			// Return a 409-style conflict message.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "orb already exists"})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			// ListOrbs calls — return the pre-existing orb for both visibility calls.
			listCalled = true
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("existing-orb-id", "my-orb", false)},
				"next_page_token": "",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	pkg, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
	if err != nil {
		t.Fatalf("CreateOrb (conflict→resolve): %v", err)
	}
	if !listCalled {
		t.Error("expected ListOrbs to be called for conflict resolution")
	}
	if pkg.ID != "existing-orb-id" {
		t.Errorf("pkg.ID = %q, want %q", pkg.ID, "existing-orb-id")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PublishVersion — 500-but-committed verify-after-write path
// ─────────────────────────────────────────────────────────────────────────────

func TestPublishVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/versions") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": makeVersionItem("ver-new", "1.0.0")})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.PublishVersion(context.Background(), "orb-uuid", "1.0.0", "version: 2.1\n", "acme-new/my-orb@1.0.0")
	if err != nil {
		t.Fatalf("PublishVersion clean success: %v", err)
	}
}

func TestPublishVersion_SpuriousError_VerifySucceeds(t *testing.T) {
	// Simulate the known API quirk: POST /orb/versions returns 500 but the
	// version actually commits. The verify call (GET filter[ref]=...) returns
	// the version → client should return success.
	postCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/versions") {
			postCalled = true
			// Spurious 500 — same as the live API quirk.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{
					"type":  "uuid_invalidlengtherror",
					"title": "invalid UUID length: 0",
				},
			})
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/orb/versions") {
			// Verify call: version resolves successfully.
			ref := r.URL.Query().Get("filter[ref]")
			if ref != "acme-new/my-orb@1.0.0" {
				t.Errorf("unexpected ref %q in verify call", ref)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{makeVersionItem("ver-committed", "1.0.0")},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.PublishVersion(context.Background(), "orb-uuid", "1.0.0", "version: 2.1\n", "acme-new/my-orb@1.0.0")
	if err != nil {
		t.Fatalf("PublishVersion with spurious 500 should succeed after verify: %v", err)
	}
	if !postCalled {
		t.Error("expected POST to be called")
	}
}

func TestPublishVersion_GenuineError_NotFound(t *testing.T) {
	// POST fails with 500 AND the verify call returns empty data → genuine failure.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"title": "real error"}})
			return
		}
		// Verify: not found.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.PublishVersion(context.Background(), "orb-uuid", "1.0.0", "yaml", "acme-new/orb@1.0.0")
	if err == nil {
		t.Fatal("expected error for genuine publish failure, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Constructor validation
// ─────────────────────────────────────────────────────────────────────────────

func TestNewClientWithBase_InvalidHost(t *testing.T) {
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	_, err := orb.NewClientWithBase("://bad-url", "tok", cfg) // nosec G101 -- fake test token
	if err == nil {
		t.Fatal("expected error for invalid host URL, got nil")
	}
}

// TestNewClient covers the default-host constructor path (delegates to
// NewClientWithBase with the real OrbHost). We don't make real HTTP calls;
// we just assert no error is returned and the client is non-nil.
func TestNewClient(t *testing.T) {
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	c, err := orb.NewClient(cfg, "fake-token") // nosec G101 -- fake test token
	if err != nil {
		t.Fatalf("NewClient: unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("NewClient: returned nil client")
	}
}

// TestNewClientWithHTTP_InvalidHost covers the error branch in newClientWithHTTP
// that is reached when the host URL has no Host component (empty host).
func TestNewClientWithHTTP_InvalidHost(t *testing.T) {
	// A relative path has no scheme/host and should trigger the error path.
	_, err := orb.NewClientWithBase("not-a-valid-host-url-with-no-scheme", "tok", nil)
	if err == nil {
		t.Fatal("expected error for host URL with no host component, got nil")
	}
}

// TestNewClientWithBase_NilConfig covers the nil-httpClient branch in
// newClientWithHTTP: when cfg is nil, a default http.Client is allocated.
func TestNewClientWithBase_NilConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"id": "ns-1", "attributes": map[string]any{"name": "acme"}},
		})
	}))
	defer srv.Close()

	// Pass nil cfg — newClientWithHTTP will allocate a default http.Client.
	c, err := orb.NewClientWithBase(srv.URL, "fake-token", nil) // nosec G101 -- fake test token
	if err != nil {
		t.Fatalf("NewClientWithBase with nil cfg: %v", err)
	}
	if c == nil {
		t.Fatal("NewClientWithBase: returned nil client")
	}
	// Make a real call to confirm the client is functional.
	id, err := c.ResolveNamespaceID(context.Background(), "acme")
	if err != nil {
		t.Fatalf("ResolveNamespaceID via nil-cfg client: %v", err)
	}
	if id != "ns-1" {
		t.Errorf("id = %q, want %q", id, "ns-1")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveNamespaceID — additional branches
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveNamespaceID_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ResolveNamespaceID(context.Background(), "acme")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// listOrbsByVisibility / ListOrbs — error branch
// ─────────────────────────────────────────────────────────────────────────────

func TestListOrbs_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err == nil {
		t.Fatal("expected error when server returns 500, got nil")
	}
}

func TestListOrbs_PrivateCallError(t *testing.T) {
	// Public call succeeds; private call returns 500 → ListOrbs must propagate
	// the private-call error (covers the second error branch in ListOrbs).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		vis := r.URL.Query().Get("filter[visibility]")
		w.Header().Set("Content-Type", "application/json")
		if vis == "private" {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "private list error"})
			return
		}
		// Public call succeeds.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":            []any{makeOrbItem("orb-pub-1", "pub-orb", false)},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err == nil {
		t.Fatal("expected error when private list call returns 500, got nil")
	}
}

func TestListOrbs_Empty(t *testing.T) {
	// Both visibility calls return empty data (no orbs in namespace).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":            []any{},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	orbs, err := c.ListOrbs(context.Background(), "ns-uuid")
	if err != nil {
		t.Fatalf("ListOrbs empty namespace: %v", err)
	}
	if len(orbs) != 0 {
		t.Errorf("expected 0 orbs, got %d", len(orbs))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ListStableVersions — error and empty/last-page branches
// ─────────────────────────────────────────────────────────────────────────────

func TestListStableVersions_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ListStableVersions(context.Background(), "orb-uuid-123")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestListStableVersions_Empty(t *testing.T) {
	// No versions for this orb — single page, empty data.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data":            []any{},
			"next_page_token": "",
		})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	versions, err := c.ListStableVersions(context.Background(), "orb-uuid-empty")
	if err != nil {
		t.Fatalf("ListStableVersions empty: %v", err)
	}
	if len(versions) != 0 {
		t.Errorf("expected 0 versions, got %d", len(versions))
	}
}

func TestListStableVersions_PaginationEnd(t *testing.T) {
	// Two pages via next_page_token, then empty cursor signals last page.
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page++
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeVersionItem("ver-1", "1.0.0")},
				"next_page_token": "cursor-ver-p2",
			})
		default:
			if r.URL.Query().Get("page[token]") != "cursor-ver-p2" {
				t.Errorf("expected page[token]=cursor-ver-p2, got %q", r.URL.Query().Get("page[token]"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeVersionItem("ver-2", "2.0.0")},
				"next_page_token": "",
			})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	versions, err := c.ListStableVersions(context.Background(), "orb-uuid-paginated")
	if err != nil {
		t.Fatalf("ListStableVersions paginated: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("expected 2 versions, got %d", len(versions))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// GetSource — success trim branch and RawDo error branch
// ─────────────────────────────────────────────────────────────────────────────

func TestGetSource_SuccessWithTrailingWhitespace(t *testing.T) {
	// GetSource returns the raw body; verify that calling code gets exactly what
	// the server sends (including trailing newlines — it's the caller's job to
	// trim if needed, but we also exercise the success path fully).
	const body = "version: 2.1\ndescription: hello\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkBearerAuth(t, r)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	src, err := c.GetSource(context.Background(), "ver-trim-test")
	if err != nil {
		t.Fatalf("GetSource: %v", err)
	}
	if src != body {
		t.Errorf("GetSource = %q, want %q", src, body)
	}
}

func TestGetSource_RawDoError(t *testing.T) {
	// Create a server, build a client pointing at it, close it, then call GetSource
	// so that RawDo returns a connection-refused error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	c := newTestClient(t, srv)
	srv.Close() // Close before the call to trigger a transport error.

	_, err := c.GetSource(context.Background(), "ver-uuid")
	if err == nil {
		t.Fatal("expected error when server is closed, got nil")
	}
}

// TestGetSource_BodyReadError covers the io.ReadAll error branch in GetSource
// by using a raw TCP server that sends valid HTTP headers but closes the
// connection before completing the body, causing io.ReadAll to return an error.
func TestGetSource_BodyReadError(t *testing.T) {
	// Start a raw TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	defer ln.Close() //nolint:errcheck

	addr := ln.Addr().String()

	// Accept one connection, write HTTP headers with Content-Length > body, then
	// abruptly close, causing the client's io.ReadAll to get an unexpected EOF.
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck

		// Consume the request.
		br := bufio.NewReader(conn)
		for {
			line, err := br.ReadString('\n')
			if err != nil || line == "\r\n" {
				break
			}
		}

		// Respond with Content-Length larger than the actual body, then close.
		// The client will try to read 1000 bytes but get EOF after 5.
		resp := "HTTP/1.1 200 OK\r\nContent-Type: text/plain\r\nContent-Length: 1000\r\n\r\nhello"
		_, _ = conn.Write([]byte(resp))
		// Close the connection immediately — client's ReadAll gets unexpected EOF.
	}()

	// Build a client pointed at the raw listener using a plain http.Client
	// (no TLS — the raw server doesn't do TLS).
	cfg := &settings.Config{HTTPClient: &http.Client{}}
	c, err := orb.NewClientWithBase("http://"+addr, "fake-token", cfg) // nosec G101 -- fake token
	if err != nil {
		t.Fatalf("NewClientWithBase: %v", err)
	}

	_, bodyErr := c.GetSource(context.Background(), "ver-uuid")
	if bodyErr == nil {
		t.Fatal("expected error for truncated body read, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ResolveVersionRef — non-404 HTTP error branch (e.g. 500)
// ─────────────────────────────────────────────────────────────────────────────

func TestResolveVersionRef_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "internal server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.ResolveVersionRef(context.Background(), "acme/my-orb@1.0.0")
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// CreateOrb — non-conflict HTTP error branch
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateOrb_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	// Must NOT be an "already exists" / conflict error — we expect the raw error.
	if strings.Contains(strings.ToLower(err.Error()), "already exists") {
		t.Errorf("unexpected 'already exists' in error for 500 response: %v", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// resolveOrbByName — "not found after conflict" error branch
// ─────────────────────────────────────────────────────────────────────────────

func TestCreateOrb_AlreadyExists_ResolveNotFound(t *testing.T) {
	// POST returns 409, but the subsequent list returns orbs that don't match
	// the requested short name → resolveOrbByName returns an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "already exists"})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			// Return an orb with a DIFFERENT name so the match fails.
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data":            []any{makeOrbItem("other-orb-id", "other-orb", false)},
				"next_page_token": "",
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
	if err == nil {
		t.Fatal("expected error when orb not found after conflict, got nil")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestCreateOrb_AlreadyExists_ListError(t *testing.T) {
	// POST returns 409, but the subsequent ListOrbs call (resolveOrbByName)
	// fails with a server error → resolveOrbByName error path.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{"message": "already exists"})
			return
		}
		if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/orb/packages") {
			// List fails with a server error.
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "list server error"})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
	if err == nil {
		t.Fatal("expected error when ListOrbs fails in resolveOrbByName, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// PublishVersion — POST fails AND verify fails (genuine failure)
// ─────────────────────────────────────────────────────────────────────────────

func TestPublishVersion_GenuineError_VerifyFails(t *testing.T) {
	// POST returns 500 AND the verify call (GET) also returns 500 → both fail.
	// The client must surface the original POST error (wrapped with verify error).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "server error"})
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := c.PublishVersion(context.Background(), "orb-uuid", "1.0.0", "yaml", "acme/orb@1.0.0")
	if err == nil {
		t.Fatal("expected error when both POST and verify fail, got nil")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// isAlreadyExistsErr — table-driven tests
// ─────────────────────────────────────────────────────────────────────────────

func TestIsAlreadyExistsErr(t *testing.T) {
	// isAlreadyExistsErr is unexported, so we test it indirectly through
	// CreateOrb: a 409 with "already exists" must trigger the idempotency path,
	// while a 500 with an unrelated message must surface as an error.

	cases := []struct {
		name         string
		statusCode   int
		body         string
		wantResolved bool // true = CreateOrb should succeed via idempotency path
	}{
		{
			name:         "conflict with already-exists message",
			statusCode:   http.StatusConflict,
			body:         `{"message":"orb already exists"}`,
			wantResolved: true,
		},
		{
			name:         "conflict with conflict message",
			statusCode:   http.StatusConflict,
			body:         `{"message":"conflict detected"}`,
			wantResolved: true,
		},
		{
			name:         "non-conflict error",
			statusCode:   http.StatusInternalServerError,
			body:         `{"error":"unrelated error"}`,
			wantResolved: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.body
			code := tc.statusCode
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/orb/packages") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(code)
					fmt.Fprint(w, body)
					return
				}
				if r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/orb/packages") {
					// Provide a matching orb for the idempotency path.
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{
						"data":            []any{makeOrbItem("existing-id", "my-orb", false)},
						"next_page_token": "",
					})
					return
				}
				w.WriteHeader(http.StatusNotFound)
			}))
			defer srv.Close()

			c := newTestClient(t, srv)
			pkg, err := c.CreateOrb(context.Background(), "my-orb", "ns-uuid", false)
			if tc.wantResolved {
				if err != nil {
					t.Fatalf("expected success via idempotency, got error: %v", err)
				}
				if pkg == nil {
					t.Fatal("expected non-nil package, got nil")
				}
			} else if err == nil {
				t.Fatal("expected error for non-conflict HTTP error, got nil")
			}
		})
	}
}
