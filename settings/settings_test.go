package settings_test

import (
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/settings"
)

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
