package version_test

import (
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/version"
)

func TestUserAgentContainsProductName(t *testing.T) {
	ua := version.UserAgent()
	if !strings.HasPrefix(ua, "circleci-migrate/") {
		t.Errorf("UserAgent() = %q; want prefix %q", ua, "circleci-migrate/")
	}
}

func TestUserAgentContainsVersion(t *testing.T) {
	ua := version.UserAgent()
	// Version defaults to "dev" in test builds; we just verify that the
	// value of version.Version appears somewhere after the slash.
	if !strings.Contains(ua, version.Version) {
		t.Errorf("UserAgent() = %q; expected to contain Version %q", ua, version.Version)
	}
}

func TestUserAgentFormat(t *testing.T) {
	ua := version.UserAgent()
	// Must be "circleci-migrate/<version>" with exactly one slash separating
	// the product name from the version token.
	parts := strings.SplitN(ua, "/", 2)
	if len(parts) != 2 {
		t.Fatalf("UserAgent() = %q; expected exactly one '/' separator", ua)
	}
	if parts[0] != "circleci-migrate" {
		t.Errorf("product name = %q; want %q", parts[0], "circleci-migrate")
	}
	if parts[1] == "" {
		t.Error("version token must not be empty")
	}
}
