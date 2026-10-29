package syncer

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

const secretURL = "https://hooks.example.com/private/soc/e/abc?secret=SUPERSECRETVALUE"

// detailedWriter adds CreateTriggerDetailed to the shared project fake.
type detailedWriter struct {
	*fakeProjectWriter
	url string
}

func (d *detailedWriter) CreateTriggerDetailed(_ context.Context, _, _ string, _ project.TriggerSpec) (*project.CreatedTrigger, error) {
	return &project.CreatedTrigger{ID: "trig-1", WebhookURL: d.url, WebhookSender: "datadog"}, nil
}

func webhookProject() manifest.Project {
	return manifest.Project{Slug: "gh/acme/web", Name: "web", Webhooks: []manifest.Webhook{
		{Name: "notify", URL: "https://recv.example.com/hook", Events: []string{"workflow-completed"}},
	}}
}

func detailsOf(rep *Report, kind string) string {
	var b strings.Builder
	for _, a := range rep.Actions {
		if a.Kind == kind {
			b.WriteString(a.Status + ": " + a.Detail + "\n")
		}
	}
	return b.String()
}

func TestSyncProjectWebhooks_GeneratesSecretOnlyWithFlag(t *testing.T) {
	var sent []project.Webhook
	fp := &fakeProjectWriter{createWebhook: func(_ string, w project.Webhook) error { sent = append(sent, w); return nil }}
	s := &Syncer{Projects: fp}

	rep := &Report{}
	s.syncProjectWebhooks(context.Background(), rep, webhookProject(), "gh/acme-new/web", "pid", Options{Apply: true})
	if len(sent) != 1 || sent[0].SigningSecret != "" {
		t.Fatalf("without the flag the secret must stay empty: %+v", sent)
	}
	if out := detailsOf(rep, "project-webhook"); !strings.Contains(out, "EMPTY") || !strings.Contains(out, "Project Settings → Webhooks") || !strings.Contains(out, "--webhook-secrets-out") {
		t.Errorf("manual line should say where to set the secret: %s", out)
	}

	sent = nil
	rep = &Report{}
	s.syncProjectWebhooks(context.Background(), rep, webhookProject(), "gh/acme-new/web", "pid", Options{Apply: true, WebhookSecretsOut: "/x/secrets.json"})
	if len(sent) != 1 || len(sent[0].SigningSecret) != 64 {
		t.Fatalf("with the flag a 256-bit hex secret is sent on create: %+v", sent)
	}
	if o, _ := s.RecordedWebhookSecrets(); o != 1 {
		t.Errorf("secret not recorded: %d", o)
	}
	for _, a := range rep.Actions {
		if strings.Contains(a.Detail, sent[0].SigningSecret) || strings.Contains(a.Target, sent[0].SigningSecret) {
			t.Fatalf("the secret leaked into the report: %+v", a)
		}
	}
	if out := detailsOf(rep, "project-webhook"); !strings.Contains(out, "/x/secrets.json") || !strings.Contains(out, "https://recv.example.com/hook") {
		t.Errorf("manual line should point at the file and the receiver: %s", out)
	}
}

func TestSyncProjectWebhooks_DryRunAndExistingDoNotGenerate(t *testing.T) {
	fp := &fakeProjectWriter{listWebhooks: func(string) ([]project.Webhook, error) {
		return []project.Webhook{{Name: "notify", URL: "https://recv.example.com/hook"}}, nil
	}}
	s := &Syncer{Projects: fp}
	rep := &Report{}
	s.syncProjectWebhooks(context.Background(), rep, webhookProject(), "d", "pid", Options{Apply: true, WebhookSecretsOut: "f.json"})
	if fp.hasCalled("CreateWebhook") {
		t.Error("an existing webhook must not be recreated")
	}
	if o, _ := s.RecordedWebhookSecrets(); o != 0 {
		t.Error("nothing to record for an existing webhook")
	}
	if out := detailsOf(rep, "project-webhook"); !strings.Contains(out, "not changed") {
		t.Errorf("existing webhook manual line: %s", out)
	}

	s2 := &Syncer{Projects: &fakeProjectWriter{}}
	rep = &Report{}
	s2.syncProjectWebhooks(context.Background(), rep, webhookProject(), "d", "", Options{WebhookSecretsOut: "f.json"})
	if o, _ := s2.RecordedWebhookSecrets(); o != 0 {
		t.Error("dry run must not generate or record secrets")
	}
	if out := detailsOf(rep, "project-webhook"); !strings.Contains(out, "would create") || !strings.Contains(out, "generated signing secret") {
		t.Errorf("dry-run detail: %s", out)
	}
}

func inboundDef() manifest.PipelineDefinition {
	return manifest.PipelineDefinition{Name: "deploy", Triggers: []manifest.Trigger{{
		Name: "dd", EventName: "alert", CheckoutRef: "main", ConfigRef: "main",
		EventSource: manifest.TriggerEventSource{Provider: "webhook", WebhookSender: "datadog"},
	}}}
}

func TestSyncAppTriggers_InboundWebhookURLRecordedOnlyWithFlag(t *testing.T) {
	dw := &detailedWriter{fakeProjectWriter: &fakeProjectWriter{}, url: secretURL}
	s := &Syncer{Projects: dw}

	rep := &Report{}
	s.syncAppTriggers(context.Background(), rep, "web", "pid", inboundDef(), "def-1", nil, nil, Options{Apply: true}, false)
	if _, i := s.RecordedWebhookSecrets(); i != 0 {
		t.Error("without the flag the URL must not be recorded")
	}
	if out := detailsOf(rep, "project-trigger"); !strings.Contains(out, "REDACTED") || !strings.Contains(out, "--webhook-secrets-out") || strings.Contains(out, "SUPERSECRETVALUE") {
		t.Errorf("manual line without flag: %s", out)
	}

	rep = &Report{}
	s.syncAppTriggers(context.Background(), rep, "web", "pid", inboundDef(), "def-1", nil, nil, Options{Apply: true, WebhookSecretsOut: "f.json"}, false)
	if _, i := s.RecordedWebhookSecrets(); i != 1 {
		t.Fatal("URL not recorded")
	}
	got := s.webhookSecrets.InboundTriggers[0]
	if got.URL != secretURL || got.Project != "web" || got.Definition != "deploy" || got.Sender != "datadog" || got.Trigger != "trig-1" {
		t.Errorf("recorded = %+v", got)
	}
	if out := detailsOf(rep, "project-trigger"); strings.Contains(out, "SUPERSECRETVALUE") || !strings.Contains(out, "f.json") {
		t.Errorf("report must name the file but never the URL: %s", out)
	}
}

func TestSyncAppTriggers_InboundWebhookNoURLReturned(t *testing.T) {
	dw := &detailedWriter{fakeProjectWriter: &fakeProjectWriter{}}
	s := &Syncer{Projects: dw}
	rep := &Report{}
	s.syncAppTriggers(context.Background(), rep, "web", "pid", inboundDef(), "def-1", nil, nil, Options{Apply: true, WebhookSecretsOut: "f.json"}, false)
	if out := detailsOf(rep, "project-trigger"); !strings.Contains(out, "returned no URL") {
		t.Errorf("want a clear note when the API returned no URL: %s", out)
	}
}

func TestWriteWebhookSecrets_Mode0600AndContent(t *testing.T) {
	s := &Syncer{}
	s.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{{Project: "p", Name: "n", URL: "u", SigningSecret: "s3cret"}}
	path := filepath.Join(t.TempDir(), "secrets.json")
	// A pre-existing, too-open file must be tightened.
	if err := os.WriteFile(path, []byte(`{"outbound_webhooks":[],"inbound_webhook_triggers":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
	var got WebhookSecrets
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil || len(got.OutboundWebhook) != 1 || got.OutboundWebhook[0].SigningSecret != "s3cret" || got.InboundTriggers == nil {
		t.Fatalf("content: %s (%v)", data, err)
	}
}

func TestPrepareWebhookSecretsFile_FailsOnUnwritablePath(t *testing.T) {
	if err := PrepareWebhookSecretsFile(filepath.Join(t.TempDir(), "no", "such", "dir", "f.json")); err == nil {
		t.Error("expected an error for an unwritable path")
	}
	p := filepath.Join(t.TempDir(), "ok.json")
	if err := PrepareWebhookSecretsFile(p); err != nil {
		t.Fatal(err)
	}
}

func newOutbound(project, name, secret string) OutboundWebhookSecret {
	return OutboundWebhookSecret{Project: project, Name: name, URL: "https://recv.example.com/" + name, SigningSecret: secret}
}

// A re-run must not wipe what an earlier run saved: prior entries stay, new
// ones are added, and an entry with the same project+name is replaced.
func TestWriteWebhookSecrets_MergesWithExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")

	first := &Syncer{}
	first.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{newOutbound("web", "notify", "s1"), newOutbound("api", "notify", "s2")}
	first.webhookSecrets.InboundTriggers = []InboundWebhookTrigger{{Project: "web", Definition: "deploy", Trigger: "t1", URL: "https://u/1"}}
	if err := first.WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}

	second := &Syncer{}
	second.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{newOutbound("web", "notify", "s1-new"), newOutbound("web", "audit", "s3")}
	second.webhookSecrets.InboundTriggers = []InboundWebhookTrigger{{Project: "web", Definition: "deploy", Trigger: "t2", URL: "https://u/2"}}
	if err := second.WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}

	got, err := LoadWebhookSecrets(path)
	if err != nil {
		t.Fatal(err)
	}
	secrets := map[string]string{}
	for _, o := range got.OutboundWebhook {
		secrets[o.Project+"/"+o.Name] = o.SigningSecret
	}
	if len(got.OutboundWebhook) != 3 || secrets["web/notify"] != "s1-new" || secrets["api/notify"] != "s2" || secrets["web/audit"] != "s3" {
		t.Errorf("outbound = %+v", got.OutboundWebhook)
	}
	if len(got.InboundTriggers) != 2 {
		t.Errorf("inbound = %+v (the earlier trigger must be kept)", got.InboundTriggers)
	}
}

// A run that recorded nothing never touches the file (so it can neither
// truncate nor re-stamp saved secrets), and --skip-projects style runs that
// never call Write leave it alone too.
func TestWriteWebhookSecrets_NothingRecordedLeavesFileUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	orig := []byte("{\n  \"generated_at\": \"2020-01-01T00:00:00Z\",\n  \"outbound_webhooks\": [{\"project\":\"web\",\"name\":\"n\",\"url\":\"u\",\"signing_secret\":\"keep-me\"}],\n  \"inbound_webhook_triggers\": []\n}\n")
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := (&Syncer{}).WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(orig) {
		t.Errorf("file changed:\n%s", got)
	}
	// Recording an identical entry again is not "something new" either.
	s := &Syncer{}
	s.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{{Project: "web", Name: "n", URL: "u", SigningSecret: "keep-me"}}
	if err := s.WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(orig) {
		t.Errorf("an identical entry must not rewrite the file:\n%s", got)
	}
}

// Prepare never truncates an existing file, creates a missing one, and refuses a
// file that is not a secrets document.
func TestPrepareWebhookSecretsFile_DoesNotTruncate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	orig := []byte(`{"outbound_webhooks":[{"project":"p","name":"n","url":"u","signing_secret":"keep"}],"inbound_webhook_triggers":[]}`)
	if err := os.WriteFile(path, orig, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebhookSecretsFile(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != string(orig) {
		t.Errorf("Prepare truncated or rewrote the file: %s", got)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("Prepare must leave no probe file behind: %v", entries)
	}

	garbage := filepath.Join(dir, "other.json")
	if err := os.WriteFile(garbage, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := PrepareWebhookSecretsFile(garbage); err == nil {
		t.Error("a file that is not a secrets document must be refused")
	}
	if got, _ := os.ReadFile(garbage); string(got) != "not json" {
		t.Errorf("the refused file must be untouched: %q", got)
	}
	s := &Syncer{}
	s.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{newOutbound("p", "n", "x")}
	if err := s.WriteWebhookSecrets(garbage); err == nil {
		t.Error("Write must not overwrite a file it cannot parse")
	}
	if got, _ := os.ReadFile(garbage); string(got) != "not json" {
		t.Errorf("Write must leave the unparseable file as it was: %q", got)
	}
}

// The save is atomic: no temp file is left and the result is 0600.
func TestWriteWebhookSecrets_AtomicNoTempLeft(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secrets.json")
	s := &Syncer{}
	s.webhookSecrets.OutboundWebhook = []OutboundWebhookSecret{newOutbound("p", "n", "x")}
	if err := s.WriteWebhookSecrets(path); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "secrets.json" {
		t.Errorf("directory = %v, want only secrets.json", entries)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v", fi.Mode().Perm())
		}
	}
}
