package orb_test

import (
	"context"
	"encoding/json"
	"fmt"
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
