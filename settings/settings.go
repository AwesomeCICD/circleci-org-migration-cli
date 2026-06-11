// Package settings holds the runtime configuration for circleci-migrate.
// It mirrors the shape of github.com/CircleCI-Public/circleci-cli/settings so
// that merging the two codebases in the future requires minimal adaptation.
package settings

import (
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	DefaultHost         = "https://circleci.com"
	DefaultRestEndpoint = "api/v2"
)

// Config is used to represent the current state of a CLI instance.
type Config struct {
	Host         string       `yaml:"host"`
	Token        string       `yaml:"token"`
	SourceToken  string       `yaml:"source_token"`
	DestToken    string       `yaml:"dest_token"`
	RestEndpoint string       `yaml:"rest_endpoint"`
	Debug        bool         `yaml:"-"`
	HTTPClient   *http.Client `yaml:"-"`
}

// NewConfig returns a Config initialised with sensible defaults.
func NewConfig() *Config {
	cfg := &Config{
		Host:         DefaultHost,
		RestEndpoint: DefaultRestEndpoint,
		HTTPClient:   &http.Client{},
	}
	return cfg
}

// TokenOrDefault returns the shared Token, falling back through the
// environment in priority order:
//  1. cfg.Token (set by --token flag)
//  2. CIRCLECI_CLI_TOKEN (our primary env var)
//  3. CIRCLE_TOKEN (injected by `circleci run migrate` — lowest precedence)
//
// Token flags default to "" (so the secret never appears in --help); the env
// fallback is resolved here.
func (cfg *Config) TokenOrDefault() string {
	if cfg.Token != "" {
		return cfg.Token
	}
	if v := os.Getenv("CIRCLECI_CLI_TOKEN"); v != "" {
		return v
	}
	// CIRCLE_TOKEN is injected by the official circleci-cli when invoked as
	// `circleci run migrate ...`. It carries the user's CLI-configured token
	// and is intentionally the lowest-precedence fallback.
	return os.Getenv("CIRCLE_TOKEN")
}

// SourceTokenOrDefault returns SourceToken when set, then the
// CIRCLECI_SOURCE_TOKEN env var, then the shared token (flag or
// CIRCLECI_CLI_TOKEN).
func (cfg *Config) SourceTokenOrDefault() string {
	if cfg.SourceToken != "" {
		return cfg.SourceToken
	}
	if v := os.Getenv("CIRCLECI_SOURCE_TOKEN"); v != "" {
		return v
	}
	return cfg.TokenOrDefault()
}

// DestTokenOrDefault returns DestToken when set, then the CIRCLECI_DEST_TOKEN
// env var, then the shared token (flag or CIRCLECI_CLI_TOKEN).
func (cfg *Config) DestTokenOrDefault() string {
	if cfg.DestToken != "" {
		return cfg.DestToken
	}
	if v := os.Getenv("CIRCLECI_DEST_TOKEN"); v != "" {
		return v
	}
	return cfg.TokenOrDefault()
}

// ServerURL builds the base REST API URL from Host and RestEndpoint.
func (cfg *Config) ServerURL() (*url.URL, error) {
	endpoint := cfg.RestEndpoint
	if !strings.HasSuffix(endpoint, "/") {
		endpoint += "/"
	}

	base, err := url.Parse(cfg.Host)
	if err != nil {
		return nil, err
	}

	return base.Parse(endpoint)
}
