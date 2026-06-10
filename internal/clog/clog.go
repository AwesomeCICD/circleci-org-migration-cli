// Package clog provides a small leveled logger for the circleci-migrate CLI.
// It is stdlib-only, concurrent-safe, and writes exclusively to an io.Writer
// (defaulting to os.Stderr so that stdout stays clean for machine-parseable
// command output such as the export manifest).
//
// Levels (low to high): Debug < Info < Warn < Error.
// The default level is Info; passing --debug raises it to Debug.
//
// Debug lines carry a short timestamp and a two-level caller hint so that
// developers can quickly locate the source of a message. Info/Warn/Error lines
// are intentionally concise — they are the "human progress" layer visible to
// operators during normal runs.
package clog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Level represents a logging verbosity level.
type Level int

const (
	// LevelDebug emits all messages including verbose debug traces.
	LevelDebug Level = iota
	// LevelInfo emits informational, warning, and error messages (default).
	LevelInfo
	// LevelWarn emits warning and error messages only.
	LevelWarn
	// LevelError emits error messages only.
	LevelError
)

// Logger is a leveled, concurrent-safe logger that writes to an io.Writer.
type Logger struct {
	mu    sync.Mutex
	w     io.Writer
	level Level
}

// New constructs a Logger that writes to w at the given level.
// w must not be nil; callers typically pass os.Stderr or a *bytes.Buffer
// for tests. The returned Logger is safe for concurrent use.
func New(w io.Writer, level Level) *Logger {
	if w == nil {
		w = io.Discard
	}
	return &Logger{w: w, level: level}
}

// defaultLogger is the package-level logger used by the free functions
// (Debugf, Infof, Warnf, Errorf). It is replaced in cmd/root.go's
// PersistentPreRunE after the --debug flag has been parsed.
var defaultLogger = New(os.Stderr, LevelInfo)

// SetDefault replaces the package-level default logger. This is called once
// in cmd/root.go after flags are parsed, and may also be used in tests to
// capture log output without spawning subprocesses.
func SetDefault(l *Logger) {
	if l == nil {
		return
	}
	defaultLogger = l
}

// Default returns the current package-level logger.
func Default() *Logger { return defaultLogger }

// Debugf emits a debug-level message on the package default logger.
func Debugf(format string, args ...any) { defaultLogger.Debugf(format, args...) }

// Infof emits an info-level message on the package default logger.
func Infof(format string, args ...any) { defaultLogger.Infof(format, args...) }

// Warnf emits a warning-level message on the package default logger.
func Warnf(format string, args ...any) { defaultLogger.Warnf(format, args...) }

// Errorf emits an error-level message on the package default logger.
func Errorf(format string, args ...any) { defaultLogger.Errorf(format, args...) }

// Debugf emits a debug-level message if the logger's level ≤ Debug.
// The message includes a short timestamp (HH:MM:SS.mmm) and a two-level
// caller hint (file:line) so that developers can quickly locate the source.
func (l *Logger) Debugf(format string, args ...any) {
	if l.level > LevelDebug {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	caller := callerHint(2)
	msg := fmt.Sprintf(format, args...)
	l.write(fmt.Sprintf("[debug] %s %s: %s\n", ts, caller, msg))
}

// Infof emits an info-level message if the logger's level ≤ Info.
func (l *Logger) Infof(format string, args ...any) {
	if l.level > LevelInfo {
		return
	}
	l.write(fmt.Sprintf(format+"\n", args...))
}

// Warnf emits a warning-level message if the logger's level ≤ Warn.
func (l *Logger) Warnf(format string, args ...any) {
	if l.level > LevelWarn {
		return
	}
	l.write(fmt.Sprintf("[warn] "+format+"\n", args...))
}

// Errorf emits an error-level message (always emitted regardless of level).
func (l *Logger) Errorf(format string, args ...any) {
	l.write(fmt.Sprintf("[error] "+format+"\n", args...))
}

// write serializes all writes through the mutex so the Logger is safe for
// concurrent use from multiple goroutines.
func (l *Logger) write(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = io.WriteString(l.w, s)
}

// callerHint returns a "file:line" string for the call site skip frames above
// callerHint. It falls back to "unknown" if runtime.Callers cannot determine
// the location (e.g. in stripped binaries).
func callerHint(skip int) string {
	_, file, line, ok := runtime.Caller(skip + 1)
	if !ok {
		return "unknown"
	}
	// Use only the last two path components (package/file.go) to keep lines short.
	dir := filepath.Base(filepath.Dir(file))
	base := filepath.Base(file)
	return fmt.Sprintf("%s/%s:%d", dir, base, line)
}
