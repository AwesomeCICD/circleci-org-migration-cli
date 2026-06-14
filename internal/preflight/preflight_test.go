package preflight_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/preflight"
)

func TestPrintSummary_Counts(t *testing.T) {
	results := []preflight.Result{
		{Name: "Tokens", Status: preflight.StatusOK, Detail: "Source and destination tokens resolved."},
		{Name: "Dest org reachable", Status: preflight.StatusOK},
		{Name: "Cross-type", Status: preflight.StatusWarn, Detail: "Org types differ.", Fixable: false},
		{Name: "API trigger flag", Status: preflight.StatusWarn, Detail: "Not enabled."},
		{Name: "Project discovery", Status: preflight.StatusFail, Detail: "No projects found."},
	}

	var buf strings.Builder
	ok, warn, fail := preflight.PrintSummary(&buf, results)

	if ok != 2 {
		t.Errorf("ok=%d, want 2", ok)
	}
	if warn != 2 {
		t.Errorf("warn=%d, want 2", warn)
	}
	if fail != 1 {
		t.Errorf("fail=%d, want 1", fail)
	}

	out := buf.String()
	if !strings.Contains(out, "✅") {
		t.Error("expected ✅ icon in output")
	}
	if !strings.Contains(out, "⚠️") {
		t.Error("expected ⚠️ icon in output")
	}
	if !strings.Contains(out, "❌") {
		t.Error("expected ❌ icon in output")
	}
	if !strings.Contains(out, "2 ok") {
		t.Errorf("expected '2 ok' in summary line; got:\n%s", out)
	}
	if !strings.Contains(out, "2 warning") {
		t.Errorf("expected '2 warning(s)' in summary line; got:\n%s", out)
	}
	if !strings.Contains(out, "1 blocker") {
		t.Errorf("expected '1 blocker(s)' in summary line; got:\n%s", out)
	}
}

func TestPrintSummary_AllOK(t *testing.T) {
	results := []preflight.Result{
		{Name: "Tokens", Status: preflight.StatusOK},
		{Name: "Dest org reachable", Status: preflight.StatusOK},
	}
	var buf strings.Builder
	ok, warn, fail := preflight.PrintSummary(&buf, results)

	if ok != 2 || warn != 0 || fail != 0 {
		t.Errorf("counts: ok=%d warn=%d fail=%d, want 2/0/0", ok, warn, fail)
	}
	out := buf.String()
	// No warning/blocker counts in the summary line when all pass.
	if strings.Contains(out, "warning") {
		t.Error("should not mention 'warning' when no warnings")
	}
	if strings.Contains(out, "blocker") {
		t.Error("should not mention 'blocker' when no failures")
	}
}

func TestPrintSummary_DetailWordWrap(t *testing.T) {
	long := strings.Repeat("word ", 20) // 100 chars
	results := []preflight.Result{
		{Name: "Long detail", Status: preflight.StatusWarn, Detail: long},
	}
	var buf strings.Builder
	preflight.PrintSummary(&buf, results)

	out := buf.String()
	// Detail lines (indented with "       ") should be under ~80 chars
	// (7-char indent + 68-char wrap = 75 chars total), but we skip the
	// box-drawing header/footer lines which contain multi-byte runes.
	for _, line := range strings.Split(out, "\n") {
		// Only check detail lines (those starting with 7 spaces).
		if strings.HasPrefix(line, "       ") && len(line) > 80 {
			t.Errorf("detail line too long (%d chars): %q", len(line), line)
		}
	}
}
