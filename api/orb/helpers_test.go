package orb

// This file tests unexported helpers that cannot be reached from the external
// orb_test package (client_test.go). Keep it small; prefer external tests for
// everything that can be tested through the public API.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

func TestIsAlreadyExistsErr(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{name: "already exists message", err: errors.New("orb already exists"), want: true},
		{name: "conflict message", err: errors.New("conflict detected"), want: true},
		{name: "unrelated error", err: errors.New("something went wrong"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isAlreadyExistsErr(tc.err)
			if got != tc.want {
				t.Errorf("isAlreadyExistsErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestGetSource_InvalidVersionID covers the url.Parse error branch in GetSource
// that is triggered when versionID contains an invalid percent-encoded sequence.
// url.Parse("orb/versions/%zz/source") returns a parse error.
func TestGetSource_InvalidVersionID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := &settings.Config{HTTPClient: srv.Client()}
	c, err := NewClientWithBase(srv.URL, "tok", cfg) // nosec G101 -- fake token
	if err != nil {
		t.Fatalf("NewClientWithBase: %v", err)
	}
	// "%zz" is an invalid percent-encoded sequence; url.Parse will return an error.
	_, getErr := c.GetSource(context.Background(), "%zz")
	if getErr == nil {
		t.Fatal("expected url.Parse error for invalid versionID, got nil")
	}
}
