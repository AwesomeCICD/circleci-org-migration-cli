package ui_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/ui"
)

// ── Color detection ───────────────────────────────────────────────────────────

func TestNew_NonFileWriter_ColorDisabled(t *testing.T) {
	var sb strings.Builder
	r := ui.New(&sb)
	if r.ColorEnabled() {
		t.Error("expected color disabled for a non-*os.File writer")
	}
}

func TestNewRendererColor_ForceDisabled(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	if r.ColorEnabled() {
		t.Error("expected color disabled when explicitly false")
	}
}

func TestNewRendererColor_ForceEnabled(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	if !r.ColorEnabled() {
		t.Error("expected color enabled when explicitly true")
	}
}

func TestDisableColor(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	r.DisableColor()
	if r.ColorEnabled() {
		t.Error("expected color disabled after DisableColor()")
	}
}

// ── StatusGlyph — plain ───────────────────────────────────────────────────────

func TestStatusGlyph_Plain(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"created", "[+]"},
		{"set", "[+]"},
		{"exists", "[=]"},
		{"manual", "[!]"},
		{"skipped", "[-]"},
		{"error", "[x]"},
		{"unknown", "[unknown]"},
	}
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	for _, tc := range cases {
		got := r.StatusGlyph(tc.status)
		if got != tc.want {
			t.Errorf("StatusGlyph(%q) plain = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// ── StatusGlyph — color (just check no ANSI on plain) ────────────────────────

func TestStatusGlyph_Color_ContainsGlyphChars(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	// With color enabled the renderer wraps with ANSI but still contains
	// the Unicode glyph character.
	cases := map[string]string{
		"created": "✓",
		"set":     "✓",
		"exists":  "•",
		"manual":  "⚠",
		"skipped": "-",
		"error":   "✗",
	}
	for status, wantChar := range cases {
		got := r.StatusGlyph(status)
		if !strings.Contains(got, wantChar) {
			t.Errorf("color StatusGlyph(%q) = %q, should contain %q", status, got, wantChar)
		}
	}
}

// ── Section ───────────────────────────────────────────────────────────────────

func TestSection_Plain_ContainsTitle(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.Section("Contexts", "DRY RUN")
	out := sb.String()
	if !strings.Contains(out, "Contexts") {
		t.Errorf("Section plain: expected 'Contexts' in %q", out)
	}
	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("Section plain: expected 'DRY RUN' in %q", out)
	}
	// Plain text uses == wrapper.
	if !strings.Contains(out, "==") {
		t.Errorf("Section plain: expected '==' in %q", out)
	}
}

func TestSection_Plain_NoMode(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.Section("Projects", "")
	out := sb.String()
	if !strings.Contains(out, "Projects") {
		t.Errorf("Section no-mode: expected 'Projects' in %q", out)
	}
}

func TestSection_Color_ContainsTitle(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	r.Section("Contexts", "APPLIED")
	out := sb.String()
	if !strings.Contains(out, "Contexts") {
		t.Errorf("Section color: expected 'Contexts' in %q", out)
	}
	if !strings.Contains(out, "APPLIED") {
		t.Errorf("Section color: expected 'APPLIED' in %q", out)
	}
}

// ── Item ─────────────────────────────────────────────────────────────────────

func TestItem_Plain_WithDetail(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.Item("created", "ctx-alpha", "created OK")
	out := sb.String()
	if !strings.Contains(out, "[+]") {
		t.Errorf("expected '[+]' glyph in plain item output: %q", out)
	}
	if !strings.Contains(out, "ctx-alpha") {
		t.Errorf("expected 'ctx-alpha' in item output: %q", out)
	}
	if !strings.Contains(out, "created OK") {
		t.Errorf("expected detail 'created OK' in item output: %q", out)
	}
}

func TestItem_Plain_NoDetail(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.Item("exists", "ctx-beta", "")
	out := sb.String()
	if !strings.Contains(out, "[=]") {
		t.Errorf("expected '[=]' glyph: %q", out)
	}
	if strings.Contains(out, " — ") {
		t.Errorf("no detail: should not contain ' — ' separator: %q", out)
	}
}

// ── CountsLine ───────────────────────────────────────────────────────────────

func TestCountsLine_Plain(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.CountsLine(ui.Counts{"created": 3, "manual": 1, "error": 0})
	out := sb.String()
	if !strings.Contains(out, "created:") {
		t.Errorf("expected 'created:' in counts line: %q", out)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("expected count 3 in counts line: %q", out)
	}
	if !strings.Contains(out, "manual:") {
		t.Errorf("expected 'manual:' in counts line: %q", out)
	}
	// zero counts should be suppressed
	if strings.Contains(out, "error:") {
		t.Errorf("zero-count 'error:' should be suppressed: %q", out)
	}
}

func TestCountsLine_Empty_PrintsNothing(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.CountsLine(ui.Counts{})
	if sb.Len() > 0 {
		t.Errorf("empty counts should print nothing, got: %q", sb.String())
	}
}

// ── AttentionBlock ─────────────────────────────────────────────────────────────

func TestAttentionBlock_Plain(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.AttentionBlock([]ui.AttentionItem{
		{Status: "manual", Label: "ctx-x", Detail: "needs setup"},
		{Status: "error", Label: "ctx-y", Detail: "API failed"},
	})
	out := sb.String()
	if !strings.Contains(out, "Needs attention") {
		t.Errorf("expected 'Needs attention' header: %q", out)
	}
	if !strings.Contains(out, "ctx-x") {
		t.Errorf("expected 'ctx-x' in attention block: %q", out)
	}
	if !strings.Contains(out, "ctx-y") {
		t.Errorf("expected 'ctx-y' in attention block: %q", out)
	}
	if !strings.Contains(out, "[!]") {
		t.Errorf("expected '[!]' glyph for manual: %q", out)
	}
	if !strings.Contains(out, "[x]") {
		t.Errorf("expected '[x]' glyph for error: %q", out)
	}
}

func TestAttentionBlock_Empty_PrintsNothing(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.AttentionBlock(nil)
	if sb.Len() > 0 {
		t.Errorf("empty attention block should print nothing, got: %q", sb.String())
	}
}

// ── EndSummary ────────────────────────────────────────────────────────────────

func TestEndSummary_Plain_NoAttention(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.EndSummary(ui.TotalCounts{Created: 5, Exists: 3})
	out := sb.String()
	if !strings.Contains(out, "Migration summary") {
		t.Errorf("expected 'Migration summary' header: %q", out)
	}
	if !strings.Contains(out, "5 created") {
		t.Errorf("expected '5 created' in summary: %q", out)
	}
	if !strings.Contains(out, "3 exists") {
		t.Errorf("expected '3 exists' in summary: %q", out)
	}
	if strings.Contains(out, "NEEDS MANUAL ATTENTION") {
		t.Errorf("should not show attention banner with no manual/error: %q", out)
	}
}

func TestEndSummary_Plain_WithAttention(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.EndSummary(ui.TotalCounts{Created: 2, Manual: 3, Error: 1})
	out := sb.String()
	if !strings.Contains(out, "NEEDS MANUAL ATTENTION (4)") {
		t.Errorf("expected 'NEEDS MANUAL ATTENTION (4)' in summary: %q", out)
	}
}

func TestEndSummary_Plain_Empty(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.EndSummary(ui.TotalCounts{})
	out := sb.String()
	if !strings.Contains(out, "nothing to report") {
		t.Errorf("expected 'nothing to report' for zero totals: %q", out)
	}
}

// ── TotalCounts ───────────────────────────────────────────────────────────────

func TestTotalCounts_Add(t *testing.T) {
	var tc ui.TotalCounts
	tc.Add(ui.Counts{"created": 2, "manual": 1})
	tc.Add(ui.Counts{"created": 3, "error": 1})
	if tc.Created != 5 {
		t.Errorf("Created: got %d, want 5", tc.Created)
	}
	if tc.Manual != 1 {
		t.Errorf("Manual: got %d, want 1", tc.Manual)
	}
	if tc.Error != 1 {
		t.Errorf("Error: got %d, want 1", tc.Error)
	}
}

func TestTotalCounts_NeedsAttention(t *testing.T) {
	tc := ui.TotalCounts{Manual: 2, Error: 1}
	if got := tc.NeedsAttention(); got != 3 {
		t.Errorf("NeedsAttention: got %d, want 3", got)
	}
}

// ── No ANSI in plain output ───────────────────────────────────────────────────

func TestPlainOutput_NoANSI(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)

	r.Section("Contexts", "DRY RUN")
	r.Item("created", "ctx-a", "ok")
	r.Item("manual", "ctx-b", "needs setup")
	r.CountsLine(ui.Counts{"created": 1, "manual": 1})
	r.AttentionBlock([]ui.AttentionItem{{Status: "manual", Label: "ctx-b", Detail: "needs setup"}})
	r.EndSummary(ui.TotalCounts{Created: 1, Manual: 1})

	out := sb.String()
	// ESC character (0x1b) must not appear in plain output.
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("plain output contains ANSI escape bytes:\n%q", out)
	}
}

// ── KeyVal ────────────────────────────────────────────────────────────────────

func TestKeyVal_Plain(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.KeyVal("Destination", "gh/acme-new")
	out := sb.String()
	if !strings.Contains(out, "Destination") {
		t.Errorf("expected 'Destination' in key-val output: %q", out)
	}
	if !strings.Contains(out, "gh/acme-new") {
		t.Errorf("expected 'gh/acme-new' in key-val output: %q", out)
	}
}

// ── Writer accessor ───────────────────────────────────────────────────────────

func TestWriter_ReturnsUnderlying(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	if r.Writer() != &sb {
		t.Error("Writer() should return the underlying io.Writer")
	}
}

// ── NO_COLOR env detection ─────────────────────────────────────────────────────

func TestNew_NoColorEnv_DisablesColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	var sb strings.Builder
	r := ui.New(&sb)
	if r.ColorEnabled() {
		t.Error("expected color disabled when NO_COLOR is set")
	}
}

// ── AttentionBlock with color enabled ─────────────────────────────────────────

func TestAttentionBlock_Color_ContainsAttentionHeader(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	r.AttentionBlock([]ui.AttentionItem{
		{Status: "manual", Label: "ctx-x", Detail: "needs setup"},
	})
	out := sb.String()
	// Attention header must be present even in colored mode.
	if !strings.Contains(out, "Needs attention") {
		t.Errorf("expected 'Needs attention' header in color mode: %q", out)
	}
	if !strings.Contains(out, "ctx-x") {
		t.Errorf("expected 'ctx-x' in color attention block: %q", out)
	}
}

// ── EndSummary with color enabled ─────────────────────────────────────────────

func TestEndSummary_Color_WithAttention(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	r.EndSummary(ui.TotalCounts{Manual: 1, Error: 1})
	out := sb.String()
	if !strings.Contains(out, "NEEDS MANUAL ATTENTION (2)") {
		t.Errorf("expected attention banner in color mode: %q", out)
	}
}

func TestEndSummary_Color_NoAttention(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	r.EndSummary(ui.TotalCounts{Created: 3, Exists: 2})
	out := sb.String()
	if !strings.Contains(out, "Migration summary") {
		t.Errorf("expected 'Migration summary' header in color mode: %q", out)
	}
	if strings.Contains(out, "NEEDS MANUAL ATTENTION") {
		t.Errorf("should not show attention banner with no manual/error in color mode: %q", out)
	}
}

// ── StatusGlyph — color unknown status ────────────────────────────────────────

func TestStatusGlyph_Color_Unknown(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, true)
	got := r.StatusGlyph("unknown-status")
	if !strings.Contains(got, "unknown-status") {
		t.Errorf("unknown status glyph should contain the status name: %q", got)
	}
}

// ── AttentionBlock item with no detail ────────────────────────────────────────

func TestAttentionBlock_ItemNoDetail(t *testing.T) {
	var sb strings.Builder
	r := ui.NewRendererColor(&sb, false)
	r.AttentionBlock([]ui.AttentionItem{
		{Status: "manual", Label: "ctx-x", Detail: ""},
	})
	out := sb.String()
	if strings.Contains(out, " — ") {
		t.Errorf("attention item with empty detail should not contain ' — ': %q", out)
	}
	if !strings.Contains(out, "ctx-x") {
		t.Errorf("expected 'ctx-x' in attention block: %q", out)
	}
}
