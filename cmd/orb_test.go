package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/CircleCI-Public/circleci-org-migration-cli/cmd"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return p
}

// sampleConfig is a minimal .circleci/config.yml that references two orbs:
// foo (in namespace "myns") and node (in namespace "circleci").
const sampleConfig = `version: 2.1

orbs:
  foo: myns/foo@1.2.3
  node: circleci/node@5.0.0

workflows:
  main:
    jobs:
      - foo/build
      - node/install
`

// fooOrbSource is a minimal orb YAML returned by the fake fetchOrbSource for
// "myns/foo@1.2.3".  It includes metadata keys (version, description) that
// must be stripped, plus the retained keys (commands, jobs).
const fooOrbSource = `version: 2
description: "A fake orb for testing"
display:
  home_url: https://example.com

commands:
  greet:
    steps:
      - run: echo hello

jobs:
  build:
    machine: true
    steps:
      - greet
`

// nodeOrbSource is a minimal orb YAML for "circleci/node@5.0.0".
const nodeOrbSource = `version: 2
commands:
  install:
    steps:
      - run: npm install
`

// ---------------------------------------------------------------------------
// Fake fetchOrbSource injection
// ---------------------------------------------------------------------------

// withFakeOrbSource replaces the package-level fetchOrbSource variable with a
// fake that returns canned responses, then restores the original on cleanup.
func withFakeOrbSource(t *testing.T, responses map[string]string) {
	t.Helper()
	original := cmd.ExposedFetchOrbSource()
	cmd.SetFetchOrbSource(func(_, _, ref string) (string, error) {
		// Strip "@volatile" so lookups work for refs without an explicit version.
		key := strings.TrimSuffix(ref, "@volatile")
		if src, ok := responses[key]; ok {
			return src, nil
		}
		if src, ok := responses[ref]; ok {
			return src, nil
		}
		return "", nil
	})
	t.Cleanup(func() { cmd.SetFetchOrbSource(original) })
}

// ---------------------------------------------------------------------------
// orb inline — basic inlining
// ---------------------------------------------------------------------------

// TestOrbInline_InlinesAllOrbs verifies that without --namespace both orbs
// in the sample config are inlined (scalar refs replaced with inline mappings).
func TestOrbInline_InlinesAllOrbs(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")
	withFakeOrbSource(t, map[string]string{
		"myns/foo@1.2.3":      fooOrbSource,
		"circleci/node@5.0.0": nodeOrbSource,
	})

	cfgPath := writeConfig(t, sampleConfig)
	out, _, err := runCmd(t, "orb", "inline", "--config", cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both orbs should now be inline mappings, not plain strings.
	assertOrbIsInline(t, out, "foo")
	assertOrbIsInline(t, out, "node")

	// Metadata keys must NOT appear inside the inline orb.
	assertNotContains(t, out, "description:")
	assertNotContains(t, out, "display:")
}

// ---------------------------------------------------------------------------
// orb inline — namespace filter
// ---------------------------------------------------------------------------

// TestOrbInline_NamespaceFilter verifies that with --namespace myns only the
// "foo" orb is inlined and "node" remains as a scalar reference.
func TestOrbInline_NamespaceFilter(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")
	withFakeOrbSource(t, map[string]string{
		"myns/foo@1.2.3":      fooOrbSource,
		"circleci/node@5.0.0": nodeOrbSource,
	})

	cfgPath := writeConfig(t, sampleConfig)
	out, _, err := runCmd(t, "orb", "inline", "--config", cfgPath, "--namespace", "myns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// foo must be inlined.
	assertOrbIsInline(t, out, "foo")

	// node must remain as a scalar ref string.
	assertOrbIsScalar(t, out, "node")
}

// ---------------------------------------------------------------------------
// orb inline — retained orb keys
// ---------------------------------------------------------------------------

// TestOrbInline_KeepsCommandsAndJobs verifies that "commands" and "jobs"
// keys from the orb source appear inside the inlined orb body.
func TestOrbInline_KeepsCommandsAndJobs(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")
	withFakeOrbSource(t, map[string]string{
		"myns/foo@1.2.3": fooOrbSource,
	})

	cfgPath := writeConfig(t, sampleConfig)
	out, _, err := runCmd(t, "orb", "inline", "--config", cfgPath, "--namespace", "myns")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out, "greet:") {
		t.Errorf("output does not contain the 'greet' command from the inlined orb:\n%s", out)
	}
	if !strings.Contains(out, "build:") {
		t.Errorf("output does not contain the 'build' job from the inlined orb:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// orb inline — required flags
// ---------------------------------------------------------------------------

// TestOrbInline_RequiresConfig verifies that omitting --config returns an error.
func TestOrbInline_RequiresConfig(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")
	_, _, err := runCmd(t, "orb", "inline")
	if err == nil {
		t.Fatal("expected error when --config is omitted, got nil")
	}
	if !strings.Contains(err.Error(), "--config") {
		t.Errorf("error %q does not mention '--config'", err.Error())
	}
}

// TestOrbInline_RequiresToken verifies that omitting the token returns an error.
func TestOrbInline_RequiresToken(t *testing.T) {
	// Clear all token env vars to ensure no token is available.
	t.Setenv("CIRCLECI_CLI_TOKEN", "")
	t.Setenv("CIRCLECI_SOURCE_TOKEN", "")

	cfgPath := writeConfig(t, sampleConfig)
	_, _, err := runCmd(t, "orb", "inline", "--config", cfgPath)
	if err == nil {
		t.Fatal("expected error when token is not set, got nil")
	}
	if !strings.Contains(err.Error(), "token") {
		t.Errorf("error %q does not mention 'token'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// orb inline — output file
// ---------------------------------------------------------------------------

// TestOrbInline_WritesOutputFile verifies that when --output is specified the
// rewritten config is written to that file rather than stdout.
func TestOrbInline_WritesOutputFile(t *testing.T) {
	t.Setenv("CIRCLECI_CLI_TOKEN", "fake-token")
	withFakeOrbSource(t, map[string]string{
		"myns/foo@1.2.3":      fooOrbSource,
		"circleci/node@5.0.0": nodeOrbSource,
	})

	cfgPath := writeConfig(t, sampleConfig)
	outPath := filepath.Join(t.TempDir(), "out.yml")

	stdout, _, err := runCmd(t, "orb", "inline", "--config", cfgPath, "-o", outPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Stdout should be empty; the content goes to the file.
	if stdout != "" {
		t.Errorf("stdout should be empty when -o is used, got: %q", stdout)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("reading output file: %v", err)
	}
	assertOrbIsInline(t, string(raw), "foo")
}

// ---------------------------------------------------------------------------
// assert helpers
// ---------------------------------------------------------------------------

// assertOrbIsInline parses the YAML in out and verifies that the orbs entry
// named alias is a YAML mapping (inline orb), not a scalar string.
func assertOrbIsInline(t *testing.T, out, alias string) {
	t.Helper()
	orbsMap := parseOrbsSection(t, out)
	val, ok := orbsMap[alias]
	if !ok {
		t.Errorf("orb %q not found in orbs section of output:\n%s", alias, out)
		return
	}
	if _, isMap := val.(map[string]any); !isMap {
		t.Errorf("orb %q: expected inline mapping, got %T (%v)", alias, val, val)
	}
}

// assertOrbIsScalar parses the YAML in out and verifies that the orbs entry
// named alias is a plain string (non-inline orb reference).
func assertOrbIsScalar(t *testing.T, out, alias string) {
	t.Helper()
	orbsMap := parseOrbsSection(t, out)
	val, ok := orbsMap[alias]
	if !ok {
		t.Errorf("orb %q not found in orbs section of output:\n%s", alias, out)
		return
	}
	if _, isStr := val.(string); !isStr {
		t.Errorf("orb %q: expected scalar string ref, got %T (%v)", alias, val, val)
	}
}

// assertNotContains fails the test if s contains substr.
func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("output should not contain %q but does:\n%s", substr, s)
	}
}

// parseOrbsSection unmarshals the top-level "orbs" map from a config YAML string.
func parseOrbsSection(t *testing.T, configYAML string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := yaml.Unmarshal([]byte(configYAML), &doc); err != nil {
		t.Fatalf("parsing output YAML: %v", err)
		return nil
	}
	orbs, ok := doc["orbs"]
	if !ok {
		t.Fatal("output YAML has no 'orbs' key")
		return nil
	}
	m, ok := orbs.(map[string]any)
	if !ok {
		t.Fatalf("orbs value is %T, want map[string]any", orbs)
		return nil
	}
	return m
}
