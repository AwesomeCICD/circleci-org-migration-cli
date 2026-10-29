package syncer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
)

// triggerDetailCreator is implemented by the project client: it returns the
// webhook URL CircleCI generates for a "webhook" trigger. It is asserted
// optionally so other ProjectWriter implementations keep working (they simply
// cannot record the new URL).
type triggerDetailCreator interface {
	CreateTriggerDetailed(ctx context.Context, projectID, defID string, spec project.TriggerSpec) (*project.CreatedTrigger, error)
}

// OutboundWebhookSecret is one recreated outbound webhook and the signing
// secret it was created with.
type OutboundWebhookSecret struct {
	Project       string   `json:"project"`
	Name          string   `json:"name"`
	URL           string   `json:"url"`
	Events        []string `json:"events,omitempty"`
	SigningSecret string   `json:"signing_secret"`
}

// InboundWebhookTrigger is one recreated inbound custom-webhook trigger and its
// NEW URL (which embeds the server-generated secret).
type InboundWebhookTrigger struct {
	Project    string `json:"project"`
	Definition string `json:"definition"`
	Trigger    string `json:"trigger_id"`
	EventName  string `json:"event_name,omitempty"`
	Sender     string `json:"sender,omitempty"`
	URL        string `json:"url"`
}

// WebhookSecrets is the content of the --webhook-secrets-out file.
type WebhookSecrets struct {
	GeneratedAt     string                  `json:"generated_at"`
	OutboundWebhook []OutboundWebhookSecret `json:"outbound_webhooks"`
	InboundTriggers []InboundWebhookTrigger `json:"inbound_webhook_triggers"`
}

// newSigningSecret returns a fresh 256-bit random secret (64 hex characters).
func newSigningSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate signing secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// RecordedWebhookSecrets returns how many outbound webhooks and inbound
// triggers have been recorded for the secrets file in this run.
func (s *Syncer) RecordedWebhookSecrets() (outbound, inbound int) {
	return len(s.webhookSecrets.OutboundWebhook), len(s.webhookSecrets.InboundTriggers)
}

// WriteWebhookSecrets writes everything recorded so far to path as JSON with
// mode 0600 (an existing file is truncated and its mode tightened). The values
// are secrets: nothing here is ever printed or logged.
func (s *Syncer) WriteWebhookSecrets(path string) error {
	out := s.webhookSecrets
	out.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	if out.OutboundWebhook == nil {
		out.OutboundWebhook = []OutboundWebhookSecret{}
	}
	if out.InboundTriggers == nil {
		out.InboundTriggers = []InboundWebhookTrigger{}
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("encode webhook secrets: %w", err)
	}
	return writeSecretFile(path, append(data, '\n'))
}

// writeSecretFile writes data to path with mode 0600.
func writeSecretFile(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600) // #nosec G304 -- operator-chosen output path
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return fmt.Errorf("chmod %s: %w", path, err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	return f.Close()
}

// PrepareWebhookSecretsFile creates (or truncates) path with mode 0600 and an
// empty document, so an unwritable path fails BEFORE any webhook is created
// with a secret that could then not be saved.
func PrepareWebhookSecretsFile(path string) error {
	s := &Syncer{}
	return s.WriteWebhookSecrets(path)
}
