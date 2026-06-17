package cmd

// Internal (white-box) tests for the unexported Prompter methods in prompt.go.
// These exercise the error/EOF and edge branches that the external cmd_test
// package cannot reach because the methods are unexported.

import (
	"context"
	"io"
	"strings"
	"testing"
)

// newTestPrompter builds a Prompter reading from input with output discarded.
func newTestPrompter(input string) *Prompter {
	return NewPrompter(strings.NewReader(input), io.Discard)
}

// TestReadLine_EOFWithContent covers the "last line, no trailing newline"
// branch of readLine, which returns the content with a nil error.
func TestReadLine_EOFWithContent(t *testing.T) {
	p := newTestPrompter("trailing-no-newline")
	got, err := p.readLine()
	if err != nil {
		t.Fatalf("readLine err = %v, want nil", err)
	}
	if got != "trailing-no-newline" {
		t.Errorf("readLine = %q, want %q", got, "trailing-no-newline")
	}
}

// TestAskWithDefault_Error covers the error-return branch when the reader is
// already exhausted (empty input → EOF).
func TestAskWithDefault_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askWithDefault("Label", "def"); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskWithDefault_NoDefaultPrompt covers the no-default prompt formatting
// branch and returns the typed value.
func TestAskWithDefault_NoDefaultPrompt(t *testing.T) {
	p := newTestPrompter("value\n")
	got, err := p.askWithDefault("Label", "")
	if err != nil {
		t.Fatalf("askWithDefault err = %v", err)
	}
	if got != "value" {
		t.Errorf("got %q, want value", got)
	}
}

// TestAskRequired_Error covers the error-return branch (EOF before any value).
func TestAskRequired_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askRequired("Label", ""); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskRequired_NoHint covers the no-hint prompt formatting branch.
func TestAskRequired_NoHint(t *testing.T) {
	p := newTestPrompter("answer\n")
	got, err := p.askRequired("Label", "")
	if err != nil {
		t.Fatalf("askRequired err = %v", err)
	}
	if got != "answer" {
		t.Errorf("got %q, want answer", got)
	}
}

// TestAskSecretRequired_NonTTY_RepromptThenValue covers the askSecretRequired
// reprompt-on-empty loop on the non-TTY path (plain-line read).
func TestAskSecretRequired_NonTTY_RepromptThenValue(t *testing.T) {
	// First line blank → reprompt; second line provides the secret.
	p := newTestPrompter("\nsecret-token\n")
	got, err := p.askSecretRequired("Token")
	if err != nil {
		t.Fatalf("askSecretRequired err = %v", err)
	}
	if got != "secret-token" {
		t.Errorf("got %q, want secret-token", got)
	}
}

// TestAskSecretRequired_Error covers the error-return branch.
func TestAskSecretRequired_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askSecretRequired("Token"); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskBool_Error covers the error-return branch.
func TestAskBool_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askBool("Proceed?", true); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskBool_InvalidThenValid covers the invalid-input reprompt branch and the
// "no" and default branches.
func TestAskBool_InvalidThenValid(t *testing.T) {
	p := newTestPrompter("maybe\nn\n")
	got, err := p.askBool("Proceed?", true)
	if err != nil {
		t.Fatalf("askBool err = %v", err)
	}
	if got {
		t.Error("expected false after 'n'")
	}
}

// TestAskBool_DefaultNo covers the default=false ([y/N]) formatting branch.
func TestAskBool_DefaultNo(t *testing.T) {
	p := newTestPrompter("\n")
	got, err := p.askBool("Proceed?", false)
	if err != nil {
		t.Fatalf("askBool err = %v", err)
	}
	if got {
		t.Error("expected default false")
	}
}

// TestAskChoice_NoOptions covers the no-options guard.
func TestAskChoice_NoOptions(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askChoice("Pick", nil); err == nil {
		t.Error("expected error for empty options")
	}
}

// TestAskChoice_Error covers the error-return branch (EOF mid-loop).
func TestAskChoice_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askChoice("Pick", []string{"a", "b"}); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskChoice_TextMatchAndReprompt covers the text-match branch and the
// invalid-number reprompt branch.
func TestAskChoice_TextMatchAndReprompt(t *testing.T) {
	// "99" is out of range → reprompt; "Beta" matches by text (case-insensitive).
	p := newTestPrompter("99\nbeta\n")
	got, err := p.askChoice("Pick", []string{"Alpha", "Beta"})
	if err != nil {
		t.Fatalf("askChoice err = %v", err)
	}
	if got != "Beta" {
		t.Errorf("got %q, want Beta", got)
	}
}

// TestAskChoice_DefaultFirst covers the empty-input default branch.
func TestAskChoice_DefaultFirst(t *testing.T) {
	p := newTestPrompter("\n")
	got, err := p.askChoice("Pick", []string{"Alpha", "Beta"})
	if err != nil {
		t.Fatalf("askChoice err = %v", err)
	}
	if got != "Alpha" {
		t.Errorf("got %q, want Alpha (default)", got)
	}
}

// TestAskMultiSelectWithDefault_EmptyOptions covers the empty-options guard.
func TestAskMultiSelectWithDefault_EmptyOptions(t *testing.T) {
	p := newTestPrompter("")
	got, err := p.askMultiSelectWithDefault("Pick", nil, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got != nil {
		t.Errorf("got %v, want nil for empty options", got)
	}
}

// TestAskMultiSelectWithDefault_Error covers the error-return branch.
func TestAskMultiSelectWithDefault_Error(t *testing.T) {
	p := newTestPrompter("")
	if _, err := p.askMultiSelectWithDefault("Pick", []string{"a", "b"}, []string{"a"}); err == nil {
		t.Error("expected error on exhausted reader")
	}
}

// TestAskMultiSelectWithDefault_NoneDefault covers the "none" default-hint
// branch (empty defaultSelected) and the comma-separated numeric selection
// parse path.
func TestAskMultiSelectWithDefault_NoneDefault(t *testing.T) {
	p := newTestPrompter("1,3\n")
	got, err := p.askMultiSelectWithDefault("Pick", []string{"a", "b", "c"}, []string{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0] != "a" || got[1] != "c" {
		t.Errorf("got %v, want [a c]", got)
	}
}

// TestAskMultiSelectWithDefault_AllKeyword covers the "all" keyword branch.
func TestAskMultiSelectWithDefault_AllKeyword(t *testing.T) {
	p := newTestPrompter("all\n")
	got, err := p.askMultiSelectWithDefault("Pick", []string{"a", "b"}, []string{"a"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 {
		t.Errorf("got %v, want both options for 'all'", got)
	}
}

// TestAskMultiSelectWithDefault_NoneKeyword covers the "none" keyword branch.
func TestAskMultiSelectWithDefault_NoneKeyword(t *testing.T) {
	p := newTestPrompter("none\n")
	got, err := p.askMultiSelectWithDefault("Pick", []string{"a", "b"}, []string{"a"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty for 'none'", got)
	}
}

// TestAskMultiSelectWithDefault_PartialDefault covers the "default" hint branch
// (defaultSelected is a strict subset of options) and the empty-input return of
// a copy of defaultSelected.
func TestAskMultiSelectWithDefault_PartialDefault(t *testing.T) {
	p := newTestPrompter("\n")
	got, err := p.askMultiSelectWithDefault("Pick", []string{"a", "b", "c"}, []string{"b"})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 1 || got[0] != "b" {
		t.Errorf("got %v, want [b]", got)
	}
}

// ---------------------------------------------------------------------------
// Context cancellation (F: Ctrl+C must abort interactive prompts)
// ---------------------------------------------------------------------------

// TestPrompter_CtxCancelled_ReturnsCtxErr verifies that a Prompter whose
// context is already cancelled returns ctx.Err() from readLine without
// hanging, even when the underlying reader has no data.
func TestPrompter_CtxCancelled_ReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Use a pipe with no data written — the goroutine inside readLine will
	// block on ReadString, but the ctx.Done() branch must win.
	pr, pw := io.Pipe()
	_ = pw // writer never writes

	p := NewPrompterCtx(ctx, pr, io.Discard)
	_, gotErr := p.readLine()
	if gotErr == nil {
		t.Fatal("expected an error from readLine with cancelled context, got nil")
	}
	if gotErr != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", gotErr)
	}
	_ = pr.Close()
	_ = pw.Close()
}

// TestPrompter_AskRequired_CtxCancelled verifies that askRequired surfaces
// context cancellation and does not loop forever.
func TestPrompter_AskRequired_CtxCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pr, pw := io.Pipe()
	p := NewPrompterCtx(ctx, pr, io.Discard)
	_, err := p.askRequired("Label", "")
	if err == nil {
		t.Fatal("expected error from askRequired with cancelled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
	_ = pr.Close()
	_ = pw.Close()
}

// TestNewPrompterCtx_BackgroundCtx verifies that NewPrompterCtx with a
// non-cancelled context behaves identically to NewPrompter for normal I/O.
func TestNewPrompterCtx_BackgroundCtx(t *testing.T) {
	p := NewPrompterCtx(context.Background(), strings.NewReader("hello\n"), io.Discard)
	got, err := p.readLine()
	if err != nil {
		t.Fatalf("readLine err = %v, want nil", err)
	}
	if got != "hello" {
		t.Errorf("got %q, want %q", got, "hello")
	}
}
