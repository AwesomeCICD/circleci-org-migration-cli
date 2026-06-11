package extract

import (
	"strings"
	"testing"
)

// Regression: the SSH-key extraction must read the private key with
// `jq --rawfile` (verbatim, preserving the trailing newline), NOT via a
// command substitution which strips it and yields an invalid OpenSSH key.
func TestBuildSSHKeyExtractConfig_UsesRawfileNotCat(t *testing.T) {
	cfg := buildSSHKeyExtractConfig([]SSHKeyInput{{Fingerprint: "abc", Hostname: "github.com"}}, nil)
	if !strings.Contains(cfg, "--rawfile pk") {
		t.Fatalf("SSH extract config must use `jq --rawfile pk` to preserve the trailing newline:\n%s", cfg)
	}
	if strings.Contains(cfg, "privkey=$(cat") {
		t.Fatalf("SSH extract config must NOT use $(cat) (strips trailing newline):\n%s", cfg)
	}
}
