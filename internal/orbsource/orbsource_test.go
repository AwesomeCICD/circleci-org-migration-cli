package orbsource

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// serveGraphQL starts an httptest.Server that responds to GraphQL POST
// requests with the supplied response body. The request body is available
// via the requestBody callback.
func serveGraphQL(t *testing.T, statusCode int, respBody any) (*httptest.Server, *http.Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		if err := json.NewEncoder(w).Encode(respBody); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, srv.Client()
}

// ---------------------------------------------------------------------------
// FetchOrbSource — success
// ---------------------------------------------------------------------------

// TestFetchOrbSource_Success verifies that a well-formed GraphQL response
// with an orbVersion payload returns the source YAML string.
func TestFetchOrbSource_Success(t *testing.T) {
	wantSource := "version: 2\ncommands:\n  greet:\n    steps:\n      - run: echo hello\n"
	resp := graphQLResponse{
		Data: &orbVersionData{
			OrbVersion: &orbVersionPayload{
				Version: "1.2.3",
				Source:  wantSource,
			},
		},
	}
	srv, client := serveGraphQL(t, http.StatusOK, resp)

	got, err := fetchOrbSource(client, srv.URL, "tok", "myns/myorb@1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != wantSource {
		t.Errorf("source = %q, want %q", got, wantSource)
	}
}

// ---------------------------------------------------------------------------
// FetchOrbSource — GraphQL errors
// ---------------------------------------------------------------------------

// TestFetchOrbSource_GraphQLErrors verifies that a response whose top-level
// "errors" array is non-empty is treated as an error.
func TestFetchOrbSource_GraphQLErrors(t *testing.T) {
	resp := graphQLResponse{
		Errors: []graphQLError{
			{Message: "orb not found"},
			{Message: "permission denied"},
		},
	}
	srv, client := serveGraphQL(t, http.StatusOK, resp)

	_, err := fetchOrbSource(client, srv.URL, "tok", "myns/myorb@1.2.3")
	if err == nil {
		t.Fatal("expected error for GraphQL errors response, got nil")
	}
	if !strings.Contains(err.Error(), "orb not found") {
		t.Errorf("error %q does not mention 'orb not found'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// FetchOrbSource — null orbVersion
// ---------------------------------------------------------------------------

// TestFetchOrbSource_NullOrbVersion verifies that a response with a null
// orbVersion (orb does not exist) returns an error mentioning the orb ref.
func TestFetchOrbSource_NullOrbVersion(t *testing.T) {
	resp := graphQLResponse{
		Data: &orbVersionData{OrbVersion: nil},
	}
	srv, client := serveGraphQL(t, http.StatusOK, resp)

	_, err := fetchOrbSource(client, srv.URL, "tok", "myns/missing@1.0.0")
	if err == nil {
		t.Fatal("expected error for null orbVersion, got nil")
	}
	if !strings.Contains(err.Error(), "null orbVersion") {
		t.Errorf("error %q does not mention 'null orbVersion'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// FetchOrbSource — no @version appends @volatile
// ---------------------------------------------------------------------------

// TestFetchOrbSource_NoVersionAppendsVolatile verifies that when the orbRef
// has no "@version" suffix the outgoing request appends "@volatile".
func TestFetchOrbSource_NoVersionAppendsVolatile(t *testing.T) {
	var capturedOrbRef string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body graphQLRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding request body: %v", err)
		}
		if ref, ok := body.Variables["r"].(string); ok {
			capturedOrbRef = ref
		}
		w.Header().Set("Content-Type", "application/json")
		resp := graphQLResponse{
			Data: &orbVersionData{
				OrbVersion: &orbVersionPayload{
					Version: "1.0.0",
					Source:  "version: 2\n",
				},
			},
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encoding response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	_, err := fetchOrbSource(srv.Client(), srv.URL, "tok", "myns/myorb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedOrbRef != "myns/myorb@volatile" {
		t.Errorf("orbRef sent = %q, want %q", capturedOrbRef, "myns/myorb@volatile")
	}
}

// ---------------------------------------------------------------------------
// FetchOrbSource — HTTP-level error
// ---------------------------------------------------------------------------

// TestFetchOrbSource_HTTP4xx verifies that an HTTP 4xx response is treated
// as an error (not silently decoded as an empty response).
func TestFetchOrbSource_HTTP4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, err := fetchOrbSource(srv.Client(), srv.URL, "badtoken", "myns/myorb@1.0.0")
	if err == nil {
		t.Fatal("expected error for HTTP 401, got nil")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("error %q does not mention 401", err.Error())
	}
}
