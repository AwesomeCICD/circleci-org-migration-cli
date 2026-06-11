package cmd

// export_usage_test.go exercises the unexported usage-export helpers in the cmd
// package using white-box tests (package cmd, not package cmd_test).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/settings"
)

// ─────────────────────────────────────────────────────────────────────────────
// downloadUsageFile
// ─────────────────────────────────────────────────────────────────────────────

func TestDownloadUsageFile_HappyPath(t *testing.T) {
	const body = "col1,col2\nval1,val2\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	outDir := t.TempDir()
	rawURL := srv.URL + "/usage/usage-2026-05.csv.gz"
	got, err := downloadUsageFile(context.Background(), rawURL, outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, readErr := os.ReadFile(got)
	if readErr != nil {
		t.Fatalf("read written file: %v", readErr)
	}
	if string(data) != body {
		t.Errorf("file content: got %q, want %q", string(data), body)
	}
	// The filename should be derived from the URL path.
	if filepath.Base(got) != "usage-2026-05.csv.gz" {
		t.Errorf("filename: got %q, want usage-2026-05.csv.gz", filepath.Base(got))
	}
}

func TestDownloadUsageFile_WithQueryString_StripsSigFromFilename(t *testing.T) {
	const body = "data"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()

	outDir := t.TempDir()
	rawURL := srv.URL + "/reports/usage-q1.csv.gz?X-Amz-Signature=abc&X-Amz-Expires=3600"
	got, err := downloadUsageFile(context.Background(), rawURL, outDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if filepath.Base(got) != "usage-q1.csv.gz" {
		t.Errorf("filename: got %q, want usage-q1.csv.gz", filepath.Base(got))
	}
}

func TestDownloadUsageFile_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "gone", http.StatusGone)
	}))
	defer srv.Close()

	outDir := t.TempDir()
	_, err := downloadUsageFile(context.Background(), srv.URL+"/usage.csv.gz", outDir)
	if err == nil {
		t.Fatal("expected error for non-2xx response, got nil")
	}
	if !strings.Contains(err.Error(), "HTTP") {
		t.Errorf("expected HTTP error in message; got: %v", err)
	}
}

func TestDownloadUsageFile_FallbackFilename(t *testing.T) {
	// URL with only the root "/" path → should use fallback filename "usage.csv.gz".
	// We test usageFileBase directly since constructing a real server with a root
	// path is equivalent (the filename comes from URL parsing, not the response).
	got := usageFileBase("http://127.0.0.1:12345/")
	if got != "usage.csv.gz" {
		t.Errorf("expected fallback filename usage.csv.gz, got %q", got)
	}

	// A completely empty path also falls back.
	got2 := usageFileBase("http://127.0.0.1:12345")
	if got2 != "usage.csv.gz" {
		t.Errorf("expected fallback filename for empty path, got %q", got2)
	}
}

func TestUsageFileBase_PreservesSegment(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{
			in:   "https://s3.example.com/reports/usage-2026-05.csv.gz?sig=abc",
			want: "usage-2026-05.csv.gz",
		},
		{
			in:   "https://s3.example.com/usage.csv.gz",
			want: "usage.csv.gz",
		},
		{
			in:   "https://s3.example.com/deep/path/report-q1.csv.gz?expires=123",
			want: "report-q1.csv.gz",
		},
	}
	for _, tc := range cases {
		got := usageFileBase(tc.in)
		if got != tc.want {
			t.Errorf("usageFileBase(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// runUsageExport — full poll sequence (create → processing → completed → download)
// ─────────────────────────────────────────────────────────────────────────────

// newUsageTestOrgClient constructs an *org.Client pointing at the given
// httptest.Server for v2 requests (usage export endpoints).
func newUsageTestOrgClient(t *testing.T, srv *httptest.Server) *org.Client {
	t.Helper()
	cfg := &settings.Config{
		Host:       srv.URL,
		HTTPClient: srv.Client(),
	}
	c, err := org.NewClient(cfg, "test-token")
	if err != nil {
		t.Fatalf("org.NewClient: %v", err)
	}
	return c
}

func TestRunUsageExport_HappyPath(t *testing.T) {
	const orgID = "org-uuid-test"
	const jobID = "job-uuid-test"
	pollCount := 0

	// Download server returns a simple CSV body.
	const csvBody = "project,credits\nweb,100\n"
	downloadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = io.WriteString(w, csvBody)
	}))
	defer downloadSrv.Close()

	downloadURL := downloadSrv.URL + "/usage-report.csv.gz"

	// API server handles create + poll.
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/usage_export_job"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"usage_export_job_id": jobID,
				"state":               "created",
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/usage_export_job/"):
			pollCount++
			if pollCount < 2 {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"usage_export_job_id": jobID,
					"state":               "processing",
				})
			} else {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"usage_export_job_id": jobID,
					"state":               "completed",
					"download_urls":       []string{downloadURL},
				})
			}
		default:
			http.Error(w, fmt.Sprintf("unexpected %s %s", r.Method, r.URL.Path), http.StatusNotFound)
		}
	}))
	defer apiSrv.Close()

	// Override poll interval for fast tests.
	origInterval := usagePollInterval
	usagePollInterval = 0
	defer func() { usagePollInterval = origInterval }()

	orgClient := newUsageTestOrgClient(t, apiSrv)
	outDir := t.TempDir()
	var errBuf bytes.Buffer
	runUsageExport(context.Background(), orgClient, orgID, "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z", outDir, 30*time.Second, &errBuf)

	errOut := errBuf.String()

	// Should print the local-baseline note.
	if !strings.Contains(errOut, "does NOT transfer") {
		t.Errorf("expected 'does NOT transfer' note in stderr; got: %q", errOut)
	}
	// Should report the saved file.
	if !strings.Contains(errOut, "Usage data saved:") {
		t.Errorf("expected 'Usage data saved:' in stderr; got: %q", errOut)
	}
	// Should not contain any warning about failure.
	if strings.Contains(errOut, "Warning: usage export job creation failed") {
		t.Errorf("unexpected job creation failure warning; got: %q", errOut)
	}

	// The downloaded file should contain the CSV body.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 file in outDir, got %d", len(entries))
	}
	data, err := os.ReadFile(filepath.Join(outDir, entries[0].Name()))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != csvBody {
		t.Errorf("downloaded content: got %q, want %q", string(data), csvBody)
	}
}

func TestRunUsageExport_CreateFails_Warns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"quota exceeded"}`, http.StatusTooManyRequests)
	}))
	defer srv.Close()

	origInterval := usagePollInterval
	usagePollInterval = 0
	defer func() { usagePollInterval = origInterval }()

	orgClient := newUsageTestOrgClient(t, srv)
	outDir := t.TempDir()
	var errBuf bytes.Buffer
	runUsageExport(context.Background(), orgClient, "org-id", "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z", outDir, time.Second, &errBuf)

	errOut := errBuf.String()
	if !strings.Contains(errOut, "Warning: usage export job creation failed") {
		t.Errorf("expected creation failure warning; got: %q", errOut)
	}
}

func TestRunUsageExport_JobFailed_Warns(t *testing.T) {
	const jobID = "job-failed"
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		callCount++
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"usage_export_job_id": jobID, "state": "created"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"usage_export_job_id": jobID, "state": "failed"})
	}))
	defer srv.Close()

	origInterval := usagePollInterval
	usagePollInterval = 0
	defer func() { usagePollInterval = origInterval }()

	orgClient := newUsageTestOrgClient(t, srv)
	outDir := t.TempDir()
	var errBuf bytes.Buffer
	runUsageExport(context.Background(), orgClient, "org-id", "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z", outDir, time.Second, &errBuf)

	errOut := errBuf.String()
	if !strings.Contains(errOut, "state \"failed\"") {
		t.Errorf("expected 'state \"failed\"' warning; got: %q", errOut)
	}
}

func TestRunUsageExport_Timeout_Warns(t *testing.T) {
	const jobID = "job-timeout"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"usage_export_job_id": jobID, "state": "created"})
			return
		}
		// Always return processing to trigger timeout.
		_ = json.NewEncoder(w).Encode(map[string]any{"usage_export_job_id": jobID, "state": "processing"})
	}))
	defer srv.Close()

	// Use a very short timeout and zero poll interval so the test finishes quickly.
	origInterval := usagePollInterval
	usagePollInterval = 0
	defer func() { usagePollInterval = origInterval }()

	orgClient := newUsageTestOrgClient(t, srv)
	outDir := t.TempDir()
	var errBuf bytes.Buffer
	// 1-nanosecond timeout: guaranteed to expire after the first poll.
	runUsageExport(context.Background(), orgClient, "org-id", "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z", outDir, time.Nanosecond, &errBuf)

	errOut := errBuf.String()
	if !strings.Contains(errOut, "did not complete within") {
		t.Errorf("expected timeout warning; got: %q", errOut)
	}
}

func TestRunUsageExport_NoDownloadURLs_Warns(t *testing.T) {
	const jobID = "job-no-urls"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_ = json.NewEncoder(w).Encode(map[string]any{"usage_export_job_id": jobID, "state": "created"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"usage_export_job_id": jobID,
			"state":               "completed",
			// no download_urls
		})
	}))
	defer srv.Close()

	origInterval := usagePollInterval
	usagePollInterval = 0
	defer func() { usagePollInterval = origInterval }()

	orgClient := newUsageTestOrgClient(t, srv)
	outDir := t.TempDir()
	var errBuf bytes.Buffer
	runUsageExport(context.Background(), orgClient, "org-id", "2026-05-01T00:00:00Z", "2026-05-31T23:59:59Z", outDir, 5*time.Second, &errBuf)

	errOut := errBuf.String()
	if !strings.Contains(errOut, "no download URLs") {
		t.Errorf("expected 'no download URLs' warning; got: %q", errOut)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// --include-usage flag registration (via exported MakeCommands)
// ─────────────────────────────────────────────────────────────────────────────

// TestExportCommand_UsageFlagsRegistered verifies that the four usage-export
// flags are registered on the export sub-command.
func TestExportCommand_UsageFlagsRegistered(t *testing.T) {
	root := MakeCommands()
	root.InitDefaultHelpCmd()
	for _, sub := range root.Commands() {
		if !strings.HasPrefix(sub.Use, "export") {
			continue
		}
		wantFlags := []string{"include-usage", "usage-start", "usage-end", "usage-timeout"}
		for _, name := range wantFlags {
			if sub.Flags().Lookup(name) == nil {
				t.Errorf("export flag --%s not registered", name)
			}
		}
		return
	}
	t.Fatal("export subcommand not found")
}
