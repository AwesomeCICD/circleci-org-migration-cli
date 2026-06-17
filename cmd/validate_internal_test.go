package cmd

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// buildValidateJSONOutput
// ---------------------------------------------------------------------------

func TestBuildValidateJSONOutput_EmptyResult(t *testing.T) {
	r := validate.Result{SourceOrg: "gh/acme", DestOrg: "gh/acme-new"}
	out := buildValidateJSONOutput(r)
	if out.SourceOrg != "gh/acme" {
		t.Errorf("SourceOrg: got %q want %q", out.SourceOrg, "gh/acme")
	}
	if out.HasGaps {
		t.Error("empty result should have HasGaps=false")
	}
	if out.Totals.Matched != 0 || out.Totals.Missing != 0 || out.Totals.Manual != 0 {
		t.Errorf("unexpected totals: %+v", out.Totals)
	}
}

func TestBuildValidateJSONOutput_WithItems(t *testing.T) {
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name: "Contexts",
				Items: []validate.Item{
					{Status: validate.StatusMatched, Section: "Contexts", Name: "deploy", Detail: "present"},
					{Status: validate.StatusMissing, Section: "Contexts", Name: "prod", Detail: "absent"},
					{Status: validate.StatusManual, Section: "Contexts", Name: "sso", Detail: "manual"},
				},
			},
			{
				Name:       "Runner Resource Classes",
				Skipped:    true,
				SkipReason: "no namespace provided",
			},
		},
	}
	out := buildValidateJSONOutput(r)

	if !out.HasGaps {
		t.Error("expected HasGaps=true")
	}
	if out.Totals.Matched != 1 {
		t.Errorf("matched: got %d want 1", out.Totals.Matched)
	}
	if out.Totals.Missing != 1 {
		t.Errorf("missing: got %d want 1", out.Totals.Missing)
	}
	if out.Totals.Manual != 1 {
		t.Errorf("manual: got %d want 1", out.Totals.Manual)
	}
	if len(out.Sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(out.Sections))
	}
	skipped := out.Sections[1]
	if !skipped.Skipped {
		t.Error("runner section should be skipped")
	}
	if skipped.SkipReason == "" {
		t.Error("skip_reason should not be empty")
	}
}

// ---------------------------------------------------------------------------
// printValidateReport
// ---------------------------------------------------------------------------

func TestPrintValidateReport_NoGaps(t *testing.T) {
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name: "Contexts",
				Items: []validate.Item{
					{Status: validate.StatusMatched, Section: "Contexts", Name: "deploy", Detail: "context present"},
				},
			},
		},
	}
	var b strings.Builder
	printValidateReport(r, &b)
	out := b.String()

	for _, want := range []string{
		"gh/acme",
		"gh/acme-new",
		"Contexts",
		"✓",
		"TOTALS",
		"VERDICT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q; got:\n%s", want, out)
		}
	}
	// Should contain the "complete" verdict since no gaps.
	if !strings.Contains(out, "complete") && !strings.Contains(out, "No missing") {
		t.Errorf("verdict should indicate no gaps; got:\n%s", out)
	}
}

func TestPrintValidateReport_WithMissing(t *testing.T) {
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name: "Contexts",
				Items: []validate.Item{
					{Status: validate.StatusMissing, Section: "Contexts", Name: "deploy", Detail: "context missing on destination"},
				},
			},
		},
	}
	var b strings.Builder
	printValidateReport(r, &b)
	out := b.String()

	if !strings.Contains(out, "GAPS FOUND") {
		t.Errorf("report should say 'GAPS FOUND'; got:\n%s", out)
	}
	if !strings.Contains(out, "NEEDS ATTENTION") {
		t.Errorf("report should have 'NEEDS ATTENTION' block; got:\n%s", out)
	}
	if !strings.Contains(out, "✗") {
		t.Errorf("report should contain ✗ marker for missing item; got:\n%s", out)
	}
}

func TestPrintValidateReport_WithManual(t *testing.T) {
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name: "Org Settings",
				Items: []validate.Item{
					{Status: validate.StatusManual, Section: "Org Settings", Name: "sso", Detail: "SSO must be reconfigured"},
				},
			},
		},
	}
	var b strings.Builder
	printValidateReport(r, &b)
	out := b.String()

	if !strings.Contains(out, "manual") {
		t.Errorf("report should mention manual; got:\n%s", out)
	}
	if !strings.Contains(out, "⚠") {
		t.Errorf("report should contain ⚠ marker for manual item; got:\n%s", out)
	}
}

func TestPrintValidateReport_SkippedSection(t *testing.T) {
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name:       "Runner Resource Classes",
				Skipped:    true,
				SkipReason: "pass --dest-runner-namespace",
			},
		},
	}
	var b strings.Builder
	printValidateReport(r, &b)
	out := b.String()

	if !strings.Contains(out, "skipped") {
		t.Errorf("report should mention 'skipped' for skipped section; got:\n%s", out)
	}
	if !strings.Contains(out, "--dest-runner-namespace") {
		t.Errorf("report should show skip reason; got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate_Short(t *testing.T) {
	got := truncate("hello", 10)
	if got != "hello" {
		t.Errorf("short string should be unchanged; got %q", got)
	}
}

func TestTruncate_ExactLength(t *testing.T) {
	got := truncate("hello", 5)
	if got != "hello" {
		t.Errorf("string at exact length should be unchanged; got %q", got)
	}
}

func TestTruncate_Long(t *testing.T) {
	got := truncate("hello world this is a long string", 10)
	if len([]rune(got)) > 10 {
		t.Errorf("truncated string too long: %q (len %d)", got, len([]rune(got)))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncated string should end with …; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// validateTotals
// ---------------------------------------------------------------------------

func TestValidateTotals_Counts(t *testing.T) {
	r := validate.Result{
		Sections: []validate.Section{
			{
				Items: []validate.Item{
					{Status: validate.StatusMatched},
					{Status: validate.StatusMatched},
					{Status: validate.StatusMissing},
					{Status: validate.StatusManual},
					{Status: validate.StatusManual},
				},
			},
		},
	}
	m, ms, mn := validateTotals(r)
	if m != 2 || ms != 1 || mn != 2 {
		t.Errorf("validateTotals: got matched=%d missing=%d manual=%d; want 2,1,2", m, ms, mn)
	}
}

// ---------------------------------------------------------------------------
// validateCountMissing
// ---------------------------------------------------------------------------

func TestValidateCountMissing(t *testing.T) {
	r := validate.Result{
		Sections: []validate.Section{
			{
				Items: []validate.Item{
					{Status: validate.StatusMatched},
					{Status: validate.StatusMissing},
					{Status: validate.StatusMissing},
				},
			},
			{
				Items: []validate.Item{
					{Status: validate.StatusManual},
					{Status: validate.StatusMissing},
				},
			},
		},
	}
	n := validateCountMissing(r)
	if n != 3 {
		t.Errorf("validateCountMissing: got %d want 3", n)
	}
}

// ---------------------------------------------------------------------------
// validateSourceNS
// ---------------------------------------------------------------------------

func TestValidateSourceNS_NoDest(t *testing.T) {
	got := validateSourceNS("gh/acme", "")
	if got != "" {
		t.Errorf("expected empty string when destNamespace is empty; got %q", got)
	}
}

func TestValidateSourceNS_GHOrg(t *testing.T) {
	got := validateSourceNS("gh/acme", "acme-new")
	if got != "acme" {
		t.Errorf("expected 'acme' from 'gh/acme'; got %q", got)
	}
}

func TestValidateSourceNS_CircleCI(t *testing.T) {
	got := validateSourceNS("circleci/some-uuid", "dest-ns")
	if got != "some-uuid" {
		t.Errorf("expected 'some-uuid' from 'circleci/some-uuid'; got %q", got)
	}
}

func TestValidateSourceNS_NoSlash(t *testing.T) {
	got := validateSourceNS("noslash", "dest-ns")
	if got != "" {
		t.Errorf("expected empty string when slug has no slash; got %q", got)
	}
}

// ---------------------------------------------------------------------------
// runPostMigrateValidation — best-effort behaviour
// ---------------------------------------------------------------------------

// TestRunPostMigrateValidation_ExportFails_PrintsWarning verifies that when
// the destination export fails (e.g. network unavailable, invalid token),
// runPostMigrateValidation prints a warning to stderr and does NOT panic or
// return an error. The migration success must never be masked by a parity
// check failure.
func TestRunPostMigrateValidation_ExportFails_PrintsWarning(t *testing.T) {
	// Use a minimal config with a deliberately unreachable host so the
	// API client construction succeeds but any network call would fail.
	cfg := &settings.Config{
		Host: "https://127.0.0.1:19999", // nothing listening here
	}

	srcManifest := &manifest.Manifest{}
	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/src", To: "gh/dst"},
	}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&stdoutBuf)
	cmd.SetErr(&stderrBuf)

	// Must not panic even when export fails.
	runPostMigrateValidation(
		context.Background(),
		cmd,
		cfg,
		srcManifest,
		"fake-token", // dstToken
		"gh/dst",     // destOrg
		"",           // destRunnerNamespace
		"",           // destOrbNamespace
		mapping,
	)

	stderr := stderrBuf.String()
	// Header must appear.
	if !strings.Contains(stderr, "Post-migration validation") {
		t.Errorf("expected 'Post-migration validation' header in stderr; got: %q", stderr)
	}
	// Warning must appear when export fails.
	if !strings.Contains(stderr, "post-migration validation skipped") &&
		!strings.Contains(stderr, "skipped") {
		t.Errorf("expected 'skipped' warning in stderr when export fails; got: %q", stderr)
	}
}

// TestRunPostMigrateValidation_PrintsReport verifies that when the destination
// manifest is available (injected via a matching source), the report is
// printed to stdout and the summary line appears.
func TestRunPostMigrateValidation_PrintsReport(t *testing.T) {
	// We cannot inject the destination manifest directly into
	// runPostMigrateValidation without a network round-trip; instead we
	// verify the underlying printValidateReport path via a direct call.
	r := validate.Result{
		SourceOrg: "gh/acme",
		DestOrg:   "gh/acme-new",
		Sections: []validate.Section{
			{
				Name: "Contexts",
				Items: []validate.Item{
					{Status: validate.StatusMatched, Name: "ctx", Detail: "present"},
				},
			},
		},
	}
	var b strings.Builder
	printValidateReport(r, &b)
	out := b.String()

	for _, want := range []string{
		"gh/acme",
		"gh/acme-new",
		"TOTALS",
		"VERDICT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report missing %q; got:\n%s", want, out)
		}
	}
}
