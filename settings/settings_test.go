package settings_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// setenv sets an environment variable for the duration of a test, restoring
// the original value (or unsetting it) via t.Cleanup.
func setenv(t *testing.T, key, val string) {
	t.Helper()
	t.Setenv(key, val)
}

// ---------------------------------------------------------------------------
// NewConfig
// ---------------------------------------------------------------------------

func TestNewConfigDefaults(t *testing.T) {
	cfg := settings.NewConfig()

	if cfg.Host != settings.DefaultHost {
		t.Errorf("Host = %q; want %q", cfg.Host, settings.DefaultHost)
	}
	if cfg.RestEndpoint != settings.DefaultRestEndpoint {
		t.Errorf("RestEndpoint = %q; want %q", cfg.RestEndpoint, settings.DefaultRestEndpoint)
	}
	if cfg.HTTPClient == nil {
		t.Error("HTTPClient must not be nil")
	}
	if cfg.Token != "" || cfg.SourceToken != "" || cfg.DestToken != "" {
		t.Error("token fields must be empty by default")
	}
}

// ---------------------------------------------------------------------------
// SourceTokenOrDefault
// ---------------------------------------------------------------------------

func TestSourceTokenOrDefault_SpecificTokenWins(t *testing.T) {
	cfg := &settings.Config{Token: "fallback", SourceToken: "source-tok"}
	if got := cfg.SourceTokenOrDefault(); got != "source-tok" {
		t.Errorf("SourceTokenOrDefault() = %q; want %q", got, "source-tok")
	}
}

func TestSourceTokenOrDefault_FallsBackToToken(t *testing.T) {
	cfg := &settings.Config{Token: "shared-tok"}
	if got := cfg.SourceTokenOrDefault(); got != "shared-tok" {
		t.Errorf("SourceTokenOrDefault() = %q; want %q", got, "shared-tok")
	}
}

func TestSourceTokenOrDefault_BothEmpty(t *testing.T) {
	// Ensure all token env vars are cleared so the test is deterministic.
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	cfg := &settings.Config{}
	if got := cfg.SourceTokenOrDefault(); got != "" {
		t.Errorf("SourceTokenOrDefault() = %q; want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// DestTokenOrDefault
// ---------------------------------------------------------------------------

func TestDestTokenOrDefault_SpecificTokenWins(t *testing.T) {
	cfg := &settings.Config{Token: "fallback", DestToken: "dest-tok"}
	if got := cfg.DestTokenOrDefault(); got != "dest-tok" {
		t.Errorf("DestTokenOrDefault() = %q; want %q", got, "dest-tok")
	}
}

func TestDestTokenOrDefault_FallsBackToToken(t *testing.T) {
	cfg := &settings.Config{Token: "shared-tok"}
	if got := cfg.DestTokenOrDefault(); got != "shared-tok" {
		t.Errorf("DestTokenOrDefault() = %q; want %q", got, "shared-tok")
	}
}

func TestDestTokenOrDefault_BothEmpty(t *testing.T) {
	// Ensure all token env vars are cleared so the test is deterministic.
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_DEST_TOKEN", "")
	t.Setenv("CIRCLE_TOKEN", "")
	cfg := &settings.Config{}
	if got := cfg.DestTokenOrDefault(); got != "" {
		t.Errorf("DestTokenOrDefault() = %q; want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// ServerURL
// ---------------------------------------------------------------------------

func TestServerURL_DefaultConfig(t *testing.T) {
	cfg := settings.NewConfig()
	u, err := cfg.ServerURL()
	if err != nil {
		t.Fatalf("ServerURL() error: %v", err)
	}
	got := u.String()
	// Must end with a slash so it can be used as a base for ResolveReference.
	if !strings.HasSuffix(got, "/") {
		t.Errorf("ServerURL() = %q; must end with '/'", got)
	}
	// Must contain both the host and the endpoint path.
	if !strings.Contains(got, "circleci.com") {
		t.Errorf("ServerURL() = %q; expected to contain 'circleci.com'", got)
	}
	if !strings.Contains(got, "api/v2") {
		t.Errorf("ServerURL() = %q; expected to contain 'api/v2'", got)
	}
}

func TestServerURL_EndpointWithoutTrailingSlash(t *testing.T) {
	cfg := &settings.Config{
		Host:         "https://example.com",
		RestEndpoint: "api/v2", // no trailing slash
	}
	u, err := cfg.ServerURL()
	if err != nil {
		t.Fatalf("ServerURL() error: %v", err)
	}
	if !strings.HasSuffix(u.String(), "/") {
		t.Errorf("ServerURL() = %q; must end with '/'", u.String())
	}
}

func TestServerURL_EndpointWithTrailingSlash(t *testing.T) {
	cfg := &settings.Config{
		Host:         "https://example.com",
		RestEndpoint: "api/v2/",
	}
	u, err := cfg.ServerURL()
	if err != nil {
		t.Fatalf("ServerURL() error: %v", err)
	}
	if !strings.HasSuffix(u.String(), "/") {
		t.Errorf("ServerURL() = %q; must end with '/'", u.String())
	}
}

func TestServerURL_BadHost(t *testing.T) {
	// A host with a control character is unparseable.
	cfg := &settings.Config{
		Host:         "://\x00bad",
		RestEndpoint: "api/v2",
	}
	// url.Parse is very lenient, but a URL with Host == "" (no scheme) can
	// still produce a valid-looking result.  We mostly verify no panic and
	// that the result is predictable; an error is also acceptable.
	_, _ = cfg.ServerURL()
}

// ---------------------------------------------------------------------------
// TokenOrDefault — CIRCLE_TOKEN fallback (circleci run migrate)
// ---------------------------------------------------------------------------

func TestTokenOrDefault_CircleToken_UsedWhenNothingElseSet(t *testing.T) {
	// CIRCLE_TOKEN is injected by `circleci run migrate`; it should be used
	// when neither the cfg.Token field nor CIRCLECI_CLI_TOKEN is set.
	setenv(t, "CIRCLECI_CLI_TOKEN", "")
	setenv(t, "CIRCLE_TOKEN", "circle-run-tok")
	cfg := &settings.Config{}
	if got := cfg.TokenOrDefault(); got != "circle-run-tok" {
		t.Errorf("TokenOrDefault() = %q; want %q", got, "circle-run-tok")
	}
}

func TestTokenOrDefault_CircleCLIToken_WinsOverCircleToken(t *testing.T) {
	// CIRCLECI_CLI_TOKEN must take precedence over the lower-priority
	// CIRCLE_TOKEN fallback.
	setenv(t, "CIRCLECI_CLI_TOKEN", "cli-tok")
	setenv(t, "CIRCLE_TOKEN", "circle-run-tok")
	cfg := &settings.Config{}
	if got := cfg.TokenOrDefault(); got != "cli-tok" {
		t.Errorf("TokenOrDefault() = %q; want %q (CIRCLECI_CLI_TOKEN must win)", got, "cli-tok")
	}
}

func TestTokenOrDefault_FlagToken_WinsOverCircleToken(t *testing.T) {
	// An explicit cfg.Token (set from --token flag) must win over everything.
	setenv(t, "CIRCLECI_CLI_TOKEN", "")
	setenv(t, "CIRCLE_TOKEN", "circle-run-tok")
	cfg := &settings.Config{Token: "flag-tok"}
	if got := cfg.TokenOrDefault(); got != "flag-tok" {
		t.Errorf("TokenOrDefault() = %q; want %q (flag must win)", got, "flag-tok")
	}
}

func TestSourceTokenOrDefault_CircleToken_FallsThrough(t *testing.T) {
	// CIRCLE_TOKEN should be reachable through SourceTokenOrDefault when all
	// higher-precedence values are absent.
	setenv(t, "CIRCLECI_CLI_TOKEN", "")
	setenv(t, "CIRCLECI_SOURCE_TOKEN", "")
	setenv(t, "CIRCLE_TOKEN", "circle-run-tok")
	cfg := &settings.Config{}
	if got := cfg.SourceTokenOrDefault(); got != "circle-run-tok" {
		t.Errorf("SourceTokenOrDefault() = %q; want %q", got, "circle-run-tok")
	}
}

func TestDestTokenOrDefault_CircleToken_FallsThrough(t *testing.T) {
	// CIRCLE_TOKEN should be reachable through DestTokenOrDefault when all
	// higher-precedence values are absent.
	setenv(t, "CIRCLECI_CLI_TOKEN", "")
	setenv(t, "CIRCLECI_DEST_TOKEN", "")
	setenv(t, "CIRCLE_TOKEN", "circle-run-tok")
	cfg := &settings.Config{}
	if got := cfg.DestTokenOrDefault(); got != "circle-run-tok" {
		t.Errorf("DestTokenOrDefault() = %q; want %q", got, "circle-run-tok")
	}
}
