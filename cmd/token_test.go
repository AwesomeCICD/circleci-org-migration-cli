package cmd

import (
	"strings"
	"testing"
)

// TestNoSourceTokenError verifies that noSourceTokenError returns an error
// whose message covers all four legs of the source-token fallback chain so
// that users see a helpful, consistent message regardless of which orb or
// export command surfaces it.
func TestNoSourceTokenError(t *testing.T) {
	err := noSourceTokenError()
	if err == nil {
		t.Fatal("noSourceTokenError() returned nil, want non-nil error")
	}

	msg := err.Error()

	// The message must mention the canonical env vars and flags so users know
	// exactly what to set.
	for _, want := range []string{
		"no source API token",
		"--source-token",
		"--token",
		"CIRCLECI_SOURCE_TOKEN",
		"CIRCLECI_CLI_TOKEN",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q does not contain %q", msg, want)
		}
	}
}
