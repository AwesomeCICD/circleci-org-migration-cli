package cmd

// migrate_preflight_offer_test.go — white-box unit tests for
// offerEnableOrgTrigger (Fix 3: interactive offer to enable the org-level
// api-trigger flag from the preflight check result list).
//
// Tests live in package cmd so they can call the unexported function and
// override stdinIsTerminal.

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/capture"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/preflight"
)

// ─────────────────────────────────────────────────────────────────────────────
// Fake featureFlagUpdater
// ─────────────────────────────────────────────────────────────────────────────

// fakeFeatureFlagUpdater is a minimal test double for featureFlagUpdater.
type fakeFeatureFlagUpdater struct {
	updateCalls []map[string]bool
	updateErr   error
}

func (f *fakeFeatureFlagUpdater) UpdateFeatureFlags(_ context.Context, _, _ string, flags map[string]bool) error {
	f.updateCalls = append(f.updateCalls, flags)
	return f.updateErr
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// triggWarnResults returns a Results slice with a fixable WARN for "Source
// api-trigger flag" to simulate the preflight having detected the flag is off.
func triggWarnResults() []preflight.Result {
	return []preflight.Result{
		{Name: "Source api-trigger flag", Status: preflight.StatusWarn, Fixable: true, Detail: "flag is off"},
	}
}

// triggOKResults returns a Results slice where the api-trigger check is OK.
func triggOKResults() []preflight.Result {
	return []preflight.Result{
		{Name: "Source api-trigger flag", Status: preflight.StatusOK, Fixable: false},
	}
}

func makeTestOrg(slug string) *org.Organization {
	return &org.Organization{Slug: slug}
}

// ─────────────────────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────────────────────

// TestOfferEnableOrgTrigger_NonInteractive verifies that when the TTY is not
// interactive the function returns immediately without prompting or updating.
func TestOfferEnableOrgTrigger_NonInteractive(t *testing.T) {
	overrideTTY(t, false)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected no update calls in non-interactive mode; got %v", mgr.updateCalls)
	}
	if out.Len() != 0 {
		t.Errorf("expected no output in non-interactive mode; got: %s", out.String())
	}
}

// TestOfferEnableOrgTrigger_NilManager verifies that a nil manager does not
// panic and returns without output.
func TestOfferEnableOrgTrigger_NilManager(t *testing.T) {
	overrideTTY(t, true)
	var out bytes.Buffer

	// Should not panic.
	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), nil, makeTestOrg("gh/myorg"))

	// No output expected.
	if out.Len() != 0 {
		t.Errorf("expected no output with nil manager; got: %s", out.String())
	}
}

// TestOfferEnableOrgTrigger_NilOrg verifies that a nil org does not panic.
func TestOfferEnableOrgTrigger_NilOrg(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), mgr, nil)

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected no update calls with nil org; got %v", mgr.updateCalls)
	}
}

// TestOfferEnableOrgTrigger_FlagAlreadyOK verifies that when no fixable WARN
// exists for the api-trigger flag the function exits without prompting.
func TestOfferEnableOrgTrigger_FlagAlreadyOK(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	offerEnableOrgTrigger(context.Background(), &out, triggOKResults(), mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected no update calls when flag is already OK; got %v", mgr.updateCalls)
	}
}

// TestOfferEnableOrgTrigger_Interactive_Yes verifies that in interactive mode
// with a fixable WARN, saying "y" enables the flag.
func TestOfferEnableOrgTrigger_Interactive_Yes(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	origStdin := replaceTriggerStdin(t, "y\n")
	defer restoreTriggerStdin(origStdin)

	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call; got %d: %v", len(mgr.updateCalls), mgr.updateCalls)
	}
	if !mgr.updateCalls[0][capture.OrgAPITriggerKey] {
		t.Error("update call should enable the flag")
	}
	if !strings.Contains(out.String(), "allow_api_trigger_with_config") {
		t.Errorf("output should mention the flag name; got: %s", out.String())
	}
}

// TestOfferEnableOrgTrigger_Interactive_No verifies that saying "n" skips the
// update silently.
func TestOfferEnableOrgTrigger_Interactive_No(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	origStdin := replaceTriggerStdin(t, "n\n")
	defer restoreTriggerStdin(origStdin)

	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected no update calls when user says no; got %v", mgr.updateCalls)
	}
}

// TestOfferEnableOrgTrigger_Interactive_Yes_UpdateErr verifies that when the
// API update fails the function logs a WARNING and does not panic.
func TestOfferEnableOrgTrigger_Interactive_Yes_UpdateErr(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{updateErr: fmt.Errorf("API timeout")}
	var out bytes.Buffer

	origStdin := replaceTriggerStdin(t, "y\n")
	defer restoreTriggerStdin(origStdin)

	offerEnableOrgTrigger(context.Background(), &out, triggWarnResults(), mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 1 {
		t.Fatalf("expected 1 update call (attempted); got %d", len(mgr.updateCalls))
	}
	if !strings.Contains(out.String(), "WARNING") {
		t.Errorf("expected WARNING in output on API error; got: %s", out.String())
	}
}

// TestOfferEnableOrgTrigger_EmptyResults verifies that an empty results list
// does not trigger the offer.
func TestOfferEnableOrgTrigger_EmptyResults(t *testing.T) {
	overrideTTY(t, true)
	mgr := &fakeFeatureFlagUpdater{}
	var out bytes.Buffer

	offerEnableOrgTrigger(context.Background(), &out, nil, mgr, makeTestOrg("gh/myorg"))

	if len(mgr.updateCalls) != 0 {
		t.Errorf("expected no update calls with empty results; got %v", mgr.updateCalls)
	}
}
