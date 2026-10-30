package syncer

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
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

// LoadWebhookSecrets reads an existing --webhook-secrets-out file. A missing or
// empty file is an empty document; a file that is not valid JSON is an error
// (it may hold secrets that must not be overwritten).
func LoadWebhookSecrets(path string) (*WebhookSecrets, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- operator-chosen output path
	if errors.Is(err, os.ErrNotExist) {
		return &WebhookSecrets{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	ws := &WebhookSecrets{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return ws, nil
	}
	if err := json.Unmarshal(data, ws); err != nil {
		return nil, fmt.Errorf("%s exists but is not a webhook secrets file (%v); move it aside or choose another path so its contents are not overwritten", path, err)
	}
	return ws, nil
}

// mergeWebhookSecrets adds the recorded entries to existing: an entry with the
// same key (outbound: project + webhook name; inbound: project + definition +
// trigger id) replaces the earlier one, everything else is kept. It reports
// whether anything was added or changed.
func mergeWebhookSecrets(existing *WebhookSecrets, recorded WebhookSecrets) bool {
	changed := false
	for _, n := range recorded.OutboundWebhook {
		idx := -1
		for i, e := range existing.OutboundWebhook {
			if e.Project == n.Project && e.Name == n.Name {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			existing.OutboundWebhook = append(existing.OutboundWebhook, n)
			changed = true
		case !reflect.DeepEqual(existing.OutboundWebhook[idx], n):
			existing.OutboundWebhook[idx] = n
			changed = true
		}
	}
	for _, n := range recorded.InboundTriggers {
		idx := -1
		for i, e := range existing.InboundTriggers {
			if e.Project == n.Project && e.Definition == n.Definition && e.Trigger == n.Trigger {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			existing.InboundTriggers = append(existing.InboundTriggers, n)
			changed = true
		case existing.InboundTriggers[idx] != n:
			existing.InboundTriggers[idx] = n
			changed = true
		}
	}
	return changed
}

// WriteWebhookSecrets merges everything recorded in this run into the file at
// path and saves it atomically (temp file in the same directory, mode 0600,
// fsync, rename). Entries already in the file are kept; the file is rewritten
// only when something new was recorded, so a re-run that creates nothing never
// touches (or truncates) secrets saved by an earlier run. The values are
// secrets: nothing here is ever printed or logged.
func (s *Syncer) WriteWebhookSecrets(path string) error {
	existing, err := LoadWebhookSecrets(path)
	if err != nil {
		return err
	}
	if !mergeWebhookSecrets(existing, s.webhookSecrets) {
		return nil
	}
	return saveWebhookSecrets(path, existing)
}

func saveWebhookSecrets(path string, out *WebhookSecrets) error {
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

// PrepareWebhookSecretsFile checks, BEFORE any webhook is created with a
// secret, that the secrets could be saved: an existing file must parse (it is
// never truncated) and the directory must accept a new file. A missing file is
// created with an empty document.
func PrepareWebhookSecretsFile(path string) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return saveWebhookSecrets(path, &WebhookSecrets{})
	}
	if _, err := LoadWebhookSecrets(path); err != nil {
		return err
	}
	return checkDirWritable(path)
}
