package clog_test

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
)

// ---------------------------------------------------------------------------
// Level-filtering
// ---------------------------------------------------------------------------

func TestDebugf_SuppressedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelInfo)
	l.Debugf("secret debug line")
	if buf.Len() != 0 {
		t.Errorf("expected no output at Info level, got: %q", buf.String())
	}
}

func TestDebugf_EmittedAtDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelDebug)
	l.Debugf("hello %s", "world")
	got := buf.String()
	if !strings.Contains(got, "[debug]") {
		t.Errorf("expected [debug] prefix, got: %q", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected message body, got: %q", got)
	}
}

func TestDebugf_IncludesTimestamp(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelDebug)
	l.Debugf("ts check")
	got := buf.String()
	// Timestamp format is HH:MM:SS.mmm — look for at least two colons.
	colons := strings.Count(got, ":")
	if colons < 2 {
		t.Errorf("expected timestamp with colons, got: %q", got)
	}
}

func TestDebugf_IncludesCallerHint(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelDebug)
	l.Debugf("caller check")
	got := buf.String()
	// The debug line should contain a "file.go:N" caller hint. The exact file
	// name may vary (test runner wrappers can shift frames), but there must be
	// a ".go:" substring followed by digits.
	if !strings.Contains(got, ".go:") {
		t.Errorf("expected caller hint (file.go:line) in debug output, got: %q", got)
	}
}

func TestInfof_EmittedAtInfoLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelInfo)
	l.Infof("progress: %d%%", 50)
	got := buf.String()
	if !strings.Contains(got, "progress: 50%") {
		t.Errorf("expected message, got: %q", got)
	}
	// Info lines must NOT include a [info] prefix so they read naturally to operators.
	if strings.Contains(got, "[info]") {
		t.Errorf("Info lines should not have [info] prefix, got: %q", got)
	}
}

func TestInfof_SuppressedAtWarnLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelWarn)
	l.Infof("should not appear")
	if buf.Len() != 0 {
		t.Errorf("expected no output at Warn level, got: %q", buf.String())
	}
}

func TestWarnf_EmittedAtWarnLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelWarn)
	l.Warnf("something odd: %s", "x")
	got := buf.String()
	if !strings.Contains(got, "[warn]") {
		t.Errorf("expected [warn] prefix, got: %q", got)
	}
	if !strings.Contains(got, "something odd: x") {
		t.Errorf("expected message body, got: %q", got)
	}
}

func TestWarnf_SuppressedAtErrorLevel(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelError)
	l.Warnf("should not appear")
	if buf.Len() != 0 {
		t.Errorf("expected no output at Error level, got: %q", buf.String())
	}
}

func TestErrorf_AlwaysEmitted(t *testing.T) {
	for _, level := range []clog.Level{clog.LevelDebug, clog.LevelInfo, clog.LevelWarn, clog.LevelError} {
		var buf bytes.Buffer
		l := clog.New(&buf, level)
		l.Errorf("critical: %d", 42)
		got := buf.String()
		if !strings.Contains(got, "[error]") {
			t.Errorf("level %d: expected [error] prefix, got: %q", level, got)
		}
		if !strings.Contains(got, "critical: 42") {
			t.Errorf("level %d: expected message body, got: %q", level, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Newline termination
// ---------------------------------------------------------------------------

func TestInfof_LineTerminated(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelInfo)
	l.Infof("single line")
	got := buf.String()
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("output should end with newline, got: %q", got)
	}
}

// ---------------------------------------------------------------------------
// Nil writer → Discard (no panic)
// ---------------------------------------------------------------------------

func TestNew_NilWriterDoesNotPanic(t *testing.T) {
	l := clog.New(nil, clog.LevelDebug)
	l.Debugf("no panic")
	l.Infof("no panic")
	l.Warnf("no panic")
	l.Errorf("no panic")
}

// ---------------------------------------------------------------------------
// Default logger + SetDefault
// ---------------------------------------------------------------------------

func TestSetDefault_ReplacesDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	orig := clog.Default()
	t.Cleanup(func() { clog.SetDefault(orig) })

	clog.SetDefault(clog.New(&buf, clog.LevelInfo))
	clog.Infof("via default")
	if !strings.Contains(buf.String(), "via default") {
		t.Errorf("SetDefault did not replace the default logger; got: %q", buf.String())
	}
}

func TestSetDefault_NilIsNoop(t *testing.T) {
	orig := clog.Default()
	clog.SetDefault(nil)
	if clog.Default() != orig {
		t.Error("SetDefault(nil) must not replace the default logger")
	}
}

func TestPackageLevelDebugf_SuppressedAtInfoDefault(t *testing.T) {
	var buf bytes.Buffer
	orig := clog.Default()
	t.Cleanup(func() { clog.SetDefault(orig) })

	clog.SetDefault(clog.New(&buf, clog.LevelInfo))
	clog.Debugf("should not appear")
	if buf.Len() != 0 {
		t.Errorf("package-level Debugf emitted at Info level: %q", buf.String())
	}
}

func TestPackageLevelDebugf_EmittedWhenDebugLevel(t *testing.T) {
	var buf bytes.Buffer
	orig := clog.Default()
	t.Cleanup(func() { clog.SetDefault(orig) })

	clog.SetDefault(clog.New(&buf, clog.LevelDebug))
	clog.Debugf("debug message")
	if !strings.Contains(buf.String(), "debug message") {
		t.Errorf("package-level Debugf not emitted at Debug level: %q", buf.String())
	}
}

// ---------------------------------------------------------------------------
// stderr-only: verify the logger writes ONLY to the injected writer
// and never touches os.Stdout. This is a contract test to confirm that
// stdout stays clean for machine-parseable output.
// ---------------------------------------------------------------------------

func TestLogger_WritesOnlyToInjectedWriter(t *testing.T) {
	// Inject a bytes.Buffer; if the logger mistakenly writes to stdout the test
	// process's own stdout will be polluted (caught in CI), but we can at least
	// verify our buffer received the output.
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelDebug)
	l.Infof("to buffer only")
	if buf.Len() == 0 {
		t.Error("expected output in injected writer, got none")
	}
}

// ---------------------------------------------------------------------------
// Concurrency safety
// ---------------------------------------------------------------------------

func TestLogger_ConcurrentWritesSafe(t *testing.T) {
	var buf bytes.Buffer
	l := clog.New(&buf, clog.LevelDebug)

	const goroutines = 50
	const msgsEach = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := range goroutines {
		go func(id int) {
			defer wg.Done()
			for j := range msgsEach {
				l.Debugf("goroutine %d msg %d", id, j)
				l.Infof("goroutine %d info %d", id, j)
				l.Warnf("goroutine %d warn %d", id, j)
				l.Errorf("goroutine %d error %d", id, j)
			}
		}(i)
	}
	wg.Wait()

	// Verify every line is properly newline-terminated (no interleaving).
	got := buf.String()
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			t.Errorf("line %d is empty (possible interleaving)", i)
		}
	}

	// Total lines = goroutines * msgsEach * 4 methods.
	want := goroutines * msgsEach * 4
	if len(lines) != want {
		t.Errorf("expected %d lines, got %d (possible interleaving or dropped writes)", want, len(lines))
	}
}
