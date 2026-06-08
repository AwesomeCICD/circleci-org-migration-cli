// Package settings holds the runtime configuration for circleci-migrate.
// It mirrors the shape of github.com/CircleCI-Public/circleci-cli/settings so
// that merging the two codebases in the future requires minimal adaptation.
package settings

import (
	"net/http"
	"net/url"
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

// SourceTokenOrDefault returns SourceToken when set, falling back to Token.
func (cfg *Config) SourceTokenOrDefault() string {
	if cfg.SourceToken != "" {
		return cfg.SourceToken
	}
	return cfg.Token
}

// DestTokenOrDefault returns DestToken when set, falling back to Token.
func (cfg *Config) DestTokenOrDefault() string {
	if cfg.DestToken != "" {
		return cfg.DestToken
	}
	return cfg.Token
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
