// Package preflight provides a lightweight check-runner for the migrate command.
//
// Each check returns a Result describing its outcome.  The runner collects
// results, prints a summary table, and returns a final status so the caller
// can decide whether to proceed.
package preflight

import (
	"fmt"
	"io"
	"strings"
)

// Status is the outcome of a single preflight check.
type Status string

const (
	// StatusOK means the check passed; no action needed.
	StatusOK Status = "ok"
	// StatusWarn means the check found a potential issue but is not blocking.
	StatusWarn Status = "warn"
	// StatusFail means a hard blocker was detected; the migration cannot proceed.
	StatusFail Status = "fail"
)

// Result holds the outcome of one preflight check.
type Result struct {
	// Name is a short, human-readable identifier for the check.
	Name string
	// Status is the outcome: ok, warn, or fail.
	Status Status
	// Detail explains what was found and (for warn/fail) what to do about it.
	Detail string
	// Fixable is true when the CLI can interactively offer to fix this issue.
	// Only meaningful for StatusWarn results.
	Fixable bool
}

// icon returns the emoji prefix for a result status.
func icon(s Status) string {
	switch s {
	case StatusOK:
		return "✅"
	case StatusWarn:
		return "⚠️ "
	case StatusFail:
		return "❌"
	default:
		return "  "
	}
}

// PrintSummary writes a formatted preflight summary to w.
// It returns the counts of ok, warn, and fail results.
func PrintSummary(w io.Writer, results []Result) (ok, warn, fail int) {
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "┌─────────────────────────────────────────────────────┐")
	fmt.Fprintln(w, "│            Preflight checks                         │")
	fmt.Fprintln(w, "└─────────────────────────────────────────────────────┘")

	for _, r := range results {
		switch r.Status {
		case StatusOK:
			ok++
		case StatusWarn:
			warn++
		case StatusFail:
			fail++
		}
		// First line: icon + name.
		fmt.Fprintf(w, "  %s %s\n", icon(r.Status), r.Name)
		// Detail lines (indented, word-wrapped at ~72 chars).
		if r.Detail != "" {
			for _, line := range wrapLines(r.Detail, 68) {
				fmt.Fprintf(w, "       %s\n", line)
			}
		}
	}

	fmt.Fprintln(w, "")
	fmt.Fprintf(w, "  Preflight: %d ok", ok)
	if warn > 0 {
		fmt.Fprintf(w, ", %d warning(s)", warn)
	}
	if fail > 0 {
		fmt.Fprintf(w, ", %d blocker(s)", fail)
	}
	fmt.Fprintln(w, "")

	return ok, warn, fail
}

// wrapLines splits text into lines of at most width characters, breaking on
// spaces.  Existing newlines in text are honoured.
func wrapLines(text string, width int) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(para)
		var line strings.Builder
		for _, w := range words {
			if line.Len() > 0 && line.Len()+1+len(w) > width {
				out = append(out, line.String())
				line.Reset()
			}
			if line.Len() > 0 {
				line.WriteByte(' ')
			}
			line.WriteString(w)
		}
		if line.Len() > 0 {
			out = append(out, line.String())
		}
	}
	return out
}
