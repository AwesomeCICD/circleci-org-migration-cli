package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestTokensNotLeakedInHelp is a security regression test: token values present
// in the environment must NEVER appear in --help output. Previously the token
// flags used the env value as their cobra default, which printed the secret in
// help text (and our own docs tell users to paste --help/--debug output into
// issues). Canary values are low-entropy and prefix-free so this test file does
// not itself trip the secret scanner.
func TestTokensNotLeakedInHelp(t *testing.T) {
	canaries := map[string]string{
		"CIRCLECI_CLI_TOKEN":    "canary-cli-token-DONOTLEAK",
		"CIRCLECI_SOURCE_TOKEN": "canary-src-token-DONOTLEAK",
		"CIRCLECI_DEST_TOKEN":   "canary-dst-token-DONOTLEAK",
		"GITHUB_TOKEN":          "canary-gh-token-DONOTLEAK",
	}
	for k, v := range canaries {
		t.Setenv(k, v)
	}

	for _, args := range [][]string{
		{"--help"},
		{"migrate", "--help"},
		{"sync", "--help"},
		{"export", "--help"},
	} {
		root := MakeCommands()
		var buf bytes.Buffer
		root.SetOut(&buf)
		root.SetErr(&buf)
		root.SetArgs(args)
		_ = root.Execute()

		out := buf.String()
		for env, secret := range canaries {
			if strings.Contains(out, secret) {
				t.Errorf("`%s` help leaked %s value %q into --help output", strings.Join(args, " "), env, secret)
			}
		}
	}
}
