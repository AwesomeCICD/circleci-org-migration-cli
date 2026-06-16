// Package ui provides a small TTY/color-aware renderer for the circleci-migrate
// CLI output. It uses lipgloss for styled output on TTYs and emits clean,
// greppable plain text when color is disabled (pipes, CI, --json mode, NO_COLOR).
//
// Color is automatically disabled when:
//   - the output writer is not a real terminal (os.File whose fd is not a TTY)
//   - the NO_COLOR environment variable is set (any value)
//
// Additionally, color can be force-disabled by calling Renderer.DisableColor()
// or by constructing via NewRendererColor with colorEnabled=false. This is used
// by --json mode and tests.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// Renderer holds an output writer and a flag indicating whether to emit ANSI
// color codes. Construct with New (auto-detects TTY) or NewRendererColor.
//
// A Renderer is not safe for concurrent use; use separate instances per
// goroutine when writing from multiple goroutines.
type Renderer struct {
	w     io.Writer
	color bool
}

// New constructs a Renderer targeting w. Color is enabled when:
//   - w is an *os.File whose file descriptor is a terminal AND
//   - the NO_COLOR environment variable is unset (or empty).
func New(w io.Writer) *Renderer {
	return &Renderer{w: w, color: detectColor(w)}
}

// NewRendererColor constructs a Renderer with an explicit color setting.
// Pass colorEnabled=false to produce pure plain-text output (for --json mode,
// test captures, or any other context where ANSI is unwanted).
func NewRendererColor(w io.Writer, colorEnabled bool) *Renderer {
	return &Renderer{w: w, color: colorEnabled}
}

// DisableColor turns off color emission. Useful when the caller learns that
// output is being redirected after construction.
func (r *Renderer) DisableColor() { r.color = false }

// ColorEnabled reports whether color output is active.
func (r *Renderer) ColorEnabled() bool { return r.color }

// Writer returns the underlying io.Writer.
func (r *Renderer) Writer() io.Writer { return r.w }

// detectColor returns true when w looks like a real terminal and NO_COLOR is
// unset.
func detectColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

// ── Styles ────────────────────────────────────────────────────────────────────

// sectionHeaderStyle renders section headers (e.g. "── Contexts sync — DRY RUN").
var sectionHeaderStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#003740", Dark: "#5FC8FF"})

// dimStyle is used for the dim "exists" / "skipped" status glyphs.
var dimStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#666666"})

// greenStyle is used for the "created" / "set" status glyphs.
var greenStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#167A00", Dark: "#73E06E"})

// yellowStyle is used for the "manual" / warning status glyphs.
var yellowStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#7B5800", Dark: "#FFD060"})

// redStyle is used for the "error" status glyph.
var redStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#B30000", Dark: "#FF7070"})

// attentionLabelStyle renders the "Needs attention:" sub-header in yellow.
var attentionLabelStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#7B5800", Dark: "#FFD060"})

// errorSummaryStyle renders the "NEEDS MANUAL ATTENTION (N)" end-of-run banner.
var errorSummaryStyle = lipgloss.NewStyle().
	Bold(true).
	Foreground(lipgloss.AdaptiveColor{Light: "#B30000", Dark: "#FF7070"})

// ── Status glyphs ─────────────────────────────────────────────────────────────

// StatusGlyph returns the colored glyph for status and the plain-text bracket
// form for non-color output. Both are returned together so callers pick the
// appropriate one.
//
// Color glyphs: ✓ (green) • (dim) ⚠ (yellow) - (dim) ✗ (red)
// Plain markers: [+]      [=]      [!]         [-]    [x]
func (r *Renderer) StatusGlyph(status string) string {
	if r.color {
		return colorGlyph(status)
	}
	return plainGlyph(status)
}

func colorGlyph(status string) string {
	switch status {
	case "created":
		return greenStyle.Render("✓")
	case "set":
		return greenStyle.Render("✓")
	case "exists":
		return dimStyle.Render("•")
	case "manual":
		return yellowStyle.Render("⚠")
	case "skipped":
		return dimStyle.Render("-")
	case "error":
		return redStyle.Render("✗")
	default:
		return "[" + status + "]"
	}
}

func plainGlyph(status string) string {
	switch status {
	case "created":
		return "[+]"
	case "set":
		return "[+]"
	case "exists":
		return "[=]"
	case "manual":
		return "[!]"
	case "skipped":
		return "[-]"
	case "error":
		return "[x]"
	default:
		return "[" + status + "]"
	}
}

// ── Section header ─────────────────────────────────────────────────────────────

// Section prints a section header line to the renderer's writer.
// For colored output: bold cyan text on its own line preceded by a blank line.
// For plain output: "── <title> ──" separator, matching the old == style.
//
// The mode string ("DRY RUN", "APPLIED", etc.) is appended after a dash when
// non-empty.
func (r *Renderer) Section(title, mode string) {
	label := title
	if mode != "" {
		label = title + " — " + mode
	}
	if r.color {
		fmt.Fprintln(r.w, "")
		fmt.Fprintln(r.w, sectionHeaderStyle.Render("── "+label+" ──"))
	} else {
		fmt.Fprintln(r.w, "")
		fmt.Fprintf(r.w, "== %s ==\n", label)
	}
}

// ── Item lines ────────────────────────────────────────────────────────────────

// Item prints a single status item line:
//
//	<glyph> <label>  — <detail>
//
// label and detail are both printed; pass "" for detail to suppress the dash.
func (r *Renderer) Item(status, label, detail string) {
	glyph := r.StatusGlyph(status)
	if detail != "" {
		fmt.Fprintf(r.w, "  %s %s — %s\n", glyph, label, detail)
	} else {
		fmt.Fprintf(r.w, "  %s %s\n", glyph, label)
	}
}

// ── Counts line ────────────────────────────────────────────────────────────────

// Counts is a map from status name to count.
type Counts map[string]int

// CountsLine prints a compact summary of non-zero counts.
// Example: "  created: 5  exists: 3  manual: 1"
// The printed order is always: created, set, exists, manual, skipped, error.
func (r *Renderer) CountsLine(counts Counts) {
	var parts []string
	for _, status := range []string{"created", "set", "exists", "manual", "skipped", "error"} {
		if n := counts[status]; n > 0 {
			parts = append(parts, fmt.Sprintf("%-8s %d", status+":", n))
		}
	}
	if len(parts) > 0 {
		fmt.Fprintf(r.w, "  %s\n", strings.Join(parts, "  "))
	}
}

// ── Needs-attention block ──────────────────────────────────────────────────────

// AttentionItem represents a single item in the "Needs attention" block.
type AttentionItem struct {
	Status string
	Label  string
	Detail string
}

// AttentionBlock prints the "Needs attention" block listing all manual/error
// items. Prints nothing when items is empty.
func (r *Renderer) AttentionBlock(items []AttentionItem) {
	if len(items) == 0 {
		return
	}
	if r.color {
		fmt.Fprintf(r.w, "\n  %s\n", attentionLabelStyle.Render("Needs attention:"))
	} else {
		fmt.Fprintf(r.w, "\n  Needs attention:\n")
	}
	for _, it := range items {
		glyph := r.StatusGlyph(it.Status)
		if it.Detail != "" {
			fmt.Fprintf(r.w, "    %s %s — %s\n", glyph, it.Label, it.Detail)
		} else {
			fmt.Fprintf(r.w, "    %s %s\n", glyph, it.Label)
		}
	}
}

// ── End-of-run summary ────────────────────────────────────────────────────────

// TotalCounts accumulates status counts across sections.
type TotalCounts struct {
	Created int
	Set     int
	Exists  int
	Manual  int
	Skipped int
	Error   int
}

// Add merges counts from a Counts map into tc.
func (tc *TotalCounts) Add(c Counts) {
	tc.Created += c["created"]
	tc.Set += c["set"]
	tc.Exists += c["exists"]
	tc.Manual += c["manual"]
	tc.Skipped += c["skipped"]
	tc.Error += c["error"]
}

// NeedsAttention returns the total count of manual + error items.
func (tc *TotalCounts) NeedsAttention() int { return tc.Manual + tc.Error }

// EndSummary prints the consolidated end-of-run summary block.
// It is the single most valuable readability win: one place to see all counts.
func (r *Renderer) EndSummary(tc TotalCounts) {
	fmt.Fprintln(r.w, "")
	if r.color {
		fmt.Fprintln(r.w, sectionHeaderStyle.Render("── Migration summary ──"))
	} else {
		fmt.Fprintln(r.w, "== Migration summary ==")
	}

	// Emit dot-separated compact line.
	var parts []string
	add := func(label string, n int) {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, label))
		}
	}
	add("created", tc.Created)
	add("set", tc.Set)
	add("exists", tc.Exists)
	add("manual", tc.Manual)
	add("skipped", tc.Skipped)
	add("error", tc.Error)

	if len(parts) == 0 {
		fmt.Fprintln(r.w, "  (nothing to report)")
	} else {
		fmt.Fprintf(r.w, "  Totals: %s\n", strings.Join(parts, " · "))
	}

	if tc.NeedsAttention() > 0 {
		msg := fmt.Sprintf("NEEDS MANUAL ATTENTION (%d)", tc.NeedsAttention())
		if r.color {
			fmt.Fprintf(r.w, "  %s\n", errorSummaryStyle.Render(msg))
		} else {
			fmt.Fprintf(r.w, "  %s\n", msg)
		}
	}
	fmt.Fprintln(r.w, "")
}

// ── Key:value detail line ─────────────────────────────────────────────────────

// KeyVal prints an indented "  Key : value" line, useful for destination slug,
// mode, etc.
func (r *Renderer) KeyVal(key, value string) {
	fmt.Fprintf(r.w, "  %-12s %s\n", key+":", value)
}
