package cmd_test

import (
	"bytes"
	"path/filepath"
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
	if err := c.Execute(); err == nil {
		t.Fatal("expected error when no bundle files are given")
	}
}
