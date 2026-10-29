package cmd

// White-box tests for the App-vs-OAuth enable-builds wording, --skip-pipelines
// flag registration and the no-secrets-bundle notice.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/syncer"
)

func appTargets() []syncer.EnableTarget {
	return []syncer.EnableTarget{
		{Kind: "trigger", ProjectID: "p1", ProjectName: "web", TriggerID: "t1", DefName: "build"},
		{Kind: "trigger", ProjectID: "p1", ProjectName: "web", TriggerID: "t2", DefName: "nightly"},
		{Kind: "trigger", ProjectID: "p2", ProjectName: "api", TriggerID: "t3", DefName: "build"},
	}
}

func TestSummarizeEnable_CountsTriggersAcrossProjects(t *testing.T) {
	s := summarizeEnable(appTargets())
	if s.triggers != 3 || s.triggerProjects != 2 || s.follows != 0 || !s.appOnly() {
		t.Errorf("summary = %+v, want 3 triggers across 2 projects, app only", s)
	}
	o := summarizeEnable([]syncer.EnableTarget{{Kind: "follow", Slug: "gh/a/b"}, {Kind: "follow", Slug: "gh/a/c"}})
	if o.follows != 2 || o.appOnly() {
		t.Errorf("OAuth summary = %+v", o)
	}
}

// A GitHub App destination speaks of triggers and projects — never "follow" or
// "installs the webhook".
func TestEnablePrompt_AppDestination(t *testing.T) {
	s := summarizeEnable(appTargets())
	for name, text := range map[string]string{
		"prompt":     s.prompt(),
		"skippedTTY": s.skippedNoTTY(),
		"skipped":    s.skipped(),
		"progress":   s.progress(),
	} {
		low := strings.ToLower(text)
		if strings.Contains(low, "follow") || strings.Contains(low, "webhook") {
			t.Errorf("%s uses OAuth wording: %q", name, text)
		}
	}
	if !strings.Contains(s.prompt(), "Enable 3 trigger(s) across 2 project(s)") {
		t.Errorf("prompt should count triggers and projects: %q", s.prompt())
	}
	if !strings.Contains(s.progress(), "Enabling 3 trigger(s) across 2 project(s)") {
		t.Errorf("progress: %q", s.progress())
	}
	if !strings.Contains(s.skippedNoTTY(), "--apply --yes") {
		t.Errorf("no-TTY skip should say how to proceed: %q", s.skippedNoTTY())
	}
}

// gh/bb destinations keep the OAuth wording.
func TestEnablePrompt_OAuthDestinationKeepsFollowWording(t *testing.T) {
	s := summarizeEnable([]syncer.EnableTarget{{Kind: "follow", Slug: "gh/a/b"}})
	if !strings.Contains(s.prompt(), "Follow (enable builds for) 1 project(s)") || !strings.Contains(s.prompt(), "installs the webhook") {
		t.Errorf("OAuth prompt changed: %q", s.prompt())
	}
	if !strings.Contains(s.skippedNoTTY(), "follow (enable builds for)") {
		t.Errorf("OAuth skip text changed: %q", s.skippedNoTTY())
	}
}

func TestHandleEnableBuilds_AppDryRunMessage(t *testing.T) {
	c := internalTestCmd()
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	rep := &syncer.Report{PendingEnable: appTargets()}
	if err := handleEnableBuilds(c, nil, rep, false, false, false); err != nil {
		t.Fatal(err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "3 trigger(s) across 2 project(s) would be created disabled") {
		t.Errorf("dry-run message should count triggers and projects, got: %q", out)
	}
	if strings.Contains(out, "created paused") {
		t.Errorf("OAuth dry-run wording leaked into the App message: %q", out)
	}
}

func TestHandleEnableBuilds_AppNoTTYNoYes_SaysEnablingTriggers(t *testing.T) {
	c := internalTestCmd()
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	rep := &syncer.Report{PendingEnable: appTargets()}
	if err := handleEnableBuilds(c, nil, rep, true, false, false); err != nil {
		t.Fatal(err)
	}
	out := errBuf.String()
	if !strings.Contains(out, "Skipped enabling triggers") || strings.Contains(out, "Skipped following") {
		t.Errorf("App skip message wrong: %q", out)
	}
}

func TestHandleEnableBuilds_AppYes_ProgressLine(t *testing.T) {
	c := internalTestCmd()
	var errBuf bytes.Buffer
	c.SetErr(&errBuf)
	rep := &syncer.Report{PendingEnable: appTargets()}
	fe := &fakeEnabler{}
	if err := handleEnableBuilds(c, fe, rep, true, true, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errBuf.String(), "Enabling 3 trigger(s) across 2 project(s)...") {
		t.Errorf("progress line missing: %q", errBuf.String())
	}
}

func TestNoBundleNotice_DoesNotImplyDataLoss(t *testing.T) {
	n := noBundleNotice(93)
	for _, want := range []string{"93 variable(s)", "does not write env var values", "Nothing was lost", "secrets transfer", "secrets capture"} {
		if !strings.Contains(n, want) {
			t.Errorf("notice missing %q: %q", want, n)
		}
	}
	if strings.Contains(n, "WARNING") || strings.Contains(n, "require manual entry") {
		t.Errorf("notice must not read like data loss: %q", n)
	}
}
