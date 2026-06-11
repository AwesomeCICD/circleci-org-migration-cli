package cmd

// sync_extras_test.go white-box tests the unexported stripProjectExtras helper
// that backs sync's --skip-extras flag.

import (
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func TestStripProjectExtras_ClearsCheckoutKeysWebhooksSchedulesSSHKeys(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{
				Slug:         "gh/acme/web",
				CheckoutKeys: []manifest.CheckoutKey{{Type: "deploy-key"}},
				Webhooks:     []manifest.Webhook{{Name: "ci", URL: "https://example.com"}},
				Schedules:    []manifest.Schedule{{Name: "nightly"}},
				SSHKeys:      []manifest.ProjectSSHKey{{Fingerprint: "ab:cd"}},
			},
			{
				Slug:     "gh/acme/api",
				Webhooks: []manifest.Webhook{{Name: "deploy", URL: "https://example.com/2"}},
			},
		},
	}

	stripProjectExtras(m)

	for _, p := range m.Projects {
		if p.CheckoutKeys != nil {
			t.Errorf("project %s: CheckoutKeys not cleared: %+v", p.Slug, p.CheckoutKeys)
		}
		if p.Webhooks != nil {
			t.Errorf("project %s: Webhooks not cleared: %+v", p.Slug, p.Webhooks)
		}
		if p.Schedules != nil {
			t.Errorf("project %s: Schedules not cleared: %+v", p.Slug, p.Schedules)
		}
		if p.SSHKeys != nil {
			t.Errorf("project %s: SSHKeys not cleared: %+v", p.Slug, p.SSHKeys)
		}
	}
}

func TestStripProjectExtras_PreservesOtherFields(t *testing.T) {
	m := &manifest.Manifest{
		Projects: []manifest.Project{
			{
				Slug:     "gh/acme/web",
				Name:     "web",
				EnvVars:  []manifest.ProjectEnvVar{{Name: "FOO"}},
				Webhooks: []manifest.Webhook{{Name: "ci"}},
			},
		},
	}

	stripProjectExtras(m)

	p := m.Projects[0]
	if p.Slug != "gh/acme/web" || p.Name != "web" {
		t.Errorf("identity fields mutated: %+v", p)
	}
	if len(p.EnvVars) != 1 || p.EnvVars[0].Name != "FOO" {
		t.Errorf("EnvVars were modified: %+v", p.EnvVars)
	}
}
