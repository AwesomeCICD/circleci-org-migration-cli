package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/cmd"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func TestSecretsMerge(t *testing.T) {
	dir := t.TempDir()

	b1 := manifest.NewSecretBundle()
	b1.SetContextSecret("a", "A1", "av")
	p1 := filepath.Join(dir, "b1.json")
	if err := b1.Save(p1); err != nil {
		t.Fatal(err)
	}

	b2 := manifest.NewSecretBundle()
	b2.SetContextSecret("b", "B1", "bv")
	b2.SetProjectSecret("gh/o/p", "X", "xv")
	p2 := filepath.Join(dir, "b2.json")
	if err := b2.Save(p2); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "merged.json")
	c := cmd.MakeCommands()
	c.SetArgs([]string{"secrets", "merge", "-o", out, p1, p2})
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	if err := c.Execute(); err != nil {
		t.Fatalf("merge: %v", err)
	}

	merged, err := manifest.LoadSecretBundle(out)
	if err != nil {
		t.Fatalf("load merged: %v", err)
	}
	if merged.ContextSecrets["a"]["A1"] != "av" || merged.ContextSecrets["b"]["B1"] != "bv" {
		t.Errorf("context secrets not merged: %+v", merged.ContextSecrets)
	}
	if merged.ProjectSecrets["gh/o/p"]["X"] != "xv" {
		t.Errorf("project secrets not merged: %+v", merged.ProjectSecrets)
	}
}

func TestSecretsMerge_RequiresArgs(t *testing.T) {
	c := cmd.MakeCommands()
	c.SetArgs([]string{"secrets", "merge", "-o", "out.json"})
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})
	err := c.Execute()
	if err == nil {
		t.Fatal("expected error when no bundle files are given")
	}
	// Error must name the positional argument and point to --help.
	if !strings.Contains(err.Error(), "bundle.json") {
		t.Errorf("error %q does not mention '<bundle.json>'", err.Error())
	}
	if !strings.Contains(err.Error(), "--help") {
		t.Errorf("error %q does not mention '--help'", err.Error())
	}
}

func TestSecretsDecrypt_RequiresArg(t *testing.T) {
	_, _, err := runCmd(t, "secrets", "decrypt", "--identity-file", "key.age")
	if err == nil {
		t.Fatal("expected error when no bundle.age path is given")
	}
	// Error must name the positional argument and point to --help.
	if !strings.Contains(err.Error(), "bundle.age") {
		t.Errorf("error %q does not mention '<bundle.age>'", err.Error())
	}
	if !strings.Contains(err.Error(), "--help") {
		t.Errorf("error %q does not mention '--help'", err.Error())
	}
}

// TestSecretsMerge_WrongFieldName verifies that a bundle file using the wrong
// top-level field name (e.g. "contexts" instead of "context_secrets") is
// rejected with an error that names the offending field, rather than silently
// merging to "0 context(s), 0 value(s)".
func TestSecretsMerge_WrongFieldName(t *testing.T) {
	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.json")
	// "contexts" is the wrong field name — the correct name is "context_secrets".
	raw := `{"schema_version":"1","contexts":{"myctx":{"KEY":"val"}}}`
	if err := os.WriteFile(badPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	out := filepath.Join(dir, "merged.json")
	var outBuf, errBuf bytes.Buffer
	c := cmd.MakeCommands()
	c.SetArgs([]string{"secrets", "merge", "-o", out, badPath})
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	err := c.Execute()
	if err == nil {
		t.Fatal("expected error for bundle with unknown field 'contexts', got nil")
	}
	// The error must mention the offending field so the user knows what went wrong.
	if !strings.Contains(err.Error(), "contexts") {
		t.Errorf("error %q does not mention the offending field 'contexts'", err.Error())
	}
}

// TestSecretsMerge_EmptyBundle_Warning verifies that when an input bundle is
// valid but contains zero context secrets and zero project secrets, a warning
// is printed to stderr. This catches the case where a correctly-formed but
// empty bundle is passed to merge.
func TestSecretsMerge_EmptyBundle_Warning(t *testing.T) {
	dir := t.TempDir()
	// Save an empty-but-valid bundle (no context or project secrets).
	empty := manifest.NewSecretBundle()
	emptyPath := filepath.Join(dir, "empty.json")
	if err := empty.Save(emptyPath); err != nil {
		t.Fatalf("Save empty bundle: %v", err)
	}

	out := filepath.Join(dir, "merged.json")
	var outBuf, errBuf bytes.Buffer
	c := cmd.MakeCommands()
	c.SetArgs([]string{"secrets", "merge", "-o", out, emptyPath})
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	if err := c.Execute(); err != nil {
		t.Fatalf("merge of empty bundle: %v", err)
	}

	// A WARNING must appear on stderr for the empty input bundle.
	if !strings.Contains(errBuf.String(), "WARNING") {
		t.Errorf("expected WARNING in stderr for empty bundle, got: %q", errBuf.String())
	}
	if !strings.Contains(errBuf.String(), "0 context") {
		t.Errorf("stderr %q does not mention '0 context'", errBuf.String())
	}
}

// TestSecretsMerge_CorrectBundle_NoWrongFieldWarning verifies that a
// correctly-shaped bundle (using the proper field names) merges successfully
// without any wrong-field-name warning.
func TestSecretsMerge_CorrectBundle_NoWrongFieldWarning(t *testing.T) {
	dir := t.TempDir()
	b := manifest.NewSecretBundle()
	b.SetContextSecret("ctx-a", "KEY", "val")
	p := filepath.Join(dir, "b.json")
	if err := b.Save(p); err != nil {
		t.Fatalf("Save: %v", err)
	}

	out := filepath.Join(dir, "merged.json")
	var outBuf, errBuf bytes.Buffer
	c := cmd.MakeCommands()
	c.SetArgs([]string{"secrets", "merge", "-o", out, p})
	c.SetOut(&outBuf)
	c.SetErr(&errBuf)
	if err := c.Execute(); err != nil {
		t.Fatalf("merge: %v", err)
	}

	// No wrong-field-name warning must appear.
	if strings.Contains(errBuf.String(), "wrong field names") {
		t.Errorf("unexpected wrong-field-names warning in stderr: %q", errBuf.String())
	}

	// Verify the merged output has the expected values.
	merged, err := manifest.LoadSecretBundle(out)
	if err != nil {
		t.Fatalf("load merged: %v", err)
	}
	if merged.ContextSecrets["ctx-a"]["KEY"] != "val" {
		t.Errorf("ContextSecrets[ctx-a][KEY] = %q; want %q", merged.ContextSecrets["ctx-a"]["KEY"], "val")
	}
}
