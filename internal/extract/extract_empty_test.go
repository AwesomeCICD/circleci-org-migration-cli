package extract

import (
	"strings"
	"testing"
)

// Regression: buildExtractConfig must not emit an empty `( )` subshell when
// there are no env-var names (bash: "syntax error near unexpected token ')'").
func TestBuildExtractConfig_EmptyEnvNames_NoEmptySubshell(t *testing.T) {
	cfg := buildExtractConfig(nil, nil, nil)
	if strings.Contains(cfg, "(\n") && strings.Contains(cfg, ") | jq") {
		// Only fail if an empty subshell pattern with no printf lines is present.
		if !strings.Contains(cfg, "printf ") {
			t.Fatalf("empty env names produced an empty subshell:\n%s", cfg)
		}
	}
	if !strings.Contains(cfg, "echo '{}'") {
		t.Fatalf("empty env names should write an empty JSON object via echo '{}':\n%s", cfg)
	}
}
