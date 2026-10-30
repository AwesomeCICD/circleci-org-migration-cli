package cmd

// White-box tests for validate's ignored contexts and informational items.

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
)

func TestIgnoredContexts_MergesAndDedupes(t *testing.T) {
	got := ignoredContexts("migration-secrets", []string{"scratch", "migration-secrets", ""})
	if strings.Join(got, ",") != "migration-secrets,scratch" {
		t.Errorf("ignoredContexts = %v", got)
	}
	if got := ignoredContexts("", nil); len(got) != 0 {
		t.Errorf("no input must yield no ignores, got %v", got)
	}
}

func TestValidateFlags_DestTokenContextAndIgnoreContext(t *testing.T) {
	root := MakeCommands()
	sub, _, err := root.Find([]string{"validate"})
	if err != nil || sub.Name() != "validate" {
		t.Fatalf("validate not found: %v", err)
	}
	for _, name := range []string{"dest-token-context", "ignore-context"} {
		if sub.Flags().Lookup(name) == nil {
			t.Errorf("validate: --%s not registered", name)
		}
	}
}

func infoResult() validate.Result {
	return validate.Result{
		SourceOrg: "gh/acme", DestOrg: "circleci/o",
		Sections: []validate.Section{{
			Name: "Org Settings",
			Items: []validate.Item{
				{Status: validate.StatusMatched, Section: "Org Settings", Name: "a", Detail: "a ok"},
				{Status: validate.StatusInfo, Section: "Org Settings", Name: "feature-flag/allow_api_trigger_with_config",
					Detail: "org flag allow_api_trigger_with_config differs (source=false, dest=true) — informational, not a gap"},
			},
		}},
	}
}

func TestPrintValidateReport_InfoShownButNotNeedsAttention(t *testing.T) {
	var b strings.Builder
	printValidateReport(infoResult(), &b)
	out := b.String()
	if !strings.Contains(out, "ℹ 1 info") || !strings.Contains(out, "informational, not a gap") {
		t.Errorf("info item should be shown:\n%s", out)
	}
	if strings.Contains(out, "NEEDS ATTENTION") {
		t.Errorf("an info item must not appear under NEEDS ATTENTION:\n%s", out)
	}
	if !strings.Contains(out, "No missing items") && !strings.Contains(out, "appears complete") {
		t.Errorf("verdict should be clean:\n%s", out)
	}
}

func TestValidateJSON_InfoCounted(t *testing.T) {
	out := buildValidateJSONOutput(infoResult())
	if out.HasGaps {
		t.Error("info must not set has_gaps")
	}
	if out.Totals.Info != 1 || out.Sections[0].Counts.Info != 1 || out.Totals.Matched != 1 || out.Totals.Missing != 0 {
		t.Errorf("totals = %+v section counts = %+v", out.Totals, out.Sections[0].Counts)
	}
	if out.Sections[0].Items[1].Status != "info" {
		t.Errorf("item status = %q, want info", out.Sections[0].Items[1].Status)
	}
}
