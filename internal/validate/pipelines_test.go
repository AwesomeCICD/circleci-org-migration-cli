package validate_test

import (
	"strings"
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
)

const pipelinesSectionName = "Pipeline Definitions & Triggers"

func appDest() *manifest.Manifest {
	m := minimalManifest("circleci/0cbd7e08")
	m.Source.Org.VCSType = "circleci"
	return m
}

func appDef(name, path string, triggers ...manifest.Trigger) manifest.PipelineDefinition {
	return manifest.PipelineDefinition{
		Name:           name,
		ConfigSource:   manifest.PipelineSource{Provider: "github_app", FilePath: path},
		CheckoutSource: manifest.PipelineSource{Provider: "github_app"},
		Triggers:       triggers,
	}
}

func trig(name string, disabled bool) manifest.Trigger {
	return manifest.Trigger{Name: name, Disabled: disabled, EventSource: manifest.TriggerEventSource{Provider: "github_app"}}
}

func section(t *testing.T, r validate.Result, name string) (validate.Section, bool) {
	t.Helper()
	for _, s := range r.Sections {
		if s.Name == name {
			return s, true
		}
	}
	return validate.Section{}, false
}

func itemStatus(sec validate.Section, nameContains string) string {
	for _, it := range sec.Items {
		if strings.Contains(it.Name, nameContains) {
			return it.Status
		}
	}
	return ""
}

func TestPipelines_NoSectionForNonAppDestination(t *testing.T) {
	src := minimalManifest("gh/acme")
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.VCSType = "github"
	r := validate.Compare(src, dst, nil, validate.Options{})
	if _, ok := section(t, r, pipelinesSectionName); ok {
		t.Error("the pipeline section only applies to GitHub App destinations")
	}
}

func TestPipelines_AllMatched(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{
		Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", false), trig("nightly", true))},
	}}
	dst := appDest()
	dst.Projects = []manifest.Project{{
		Slug: "circleci/0cbd7e08/p1", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", false), trig("nightly", true))},
	}}
	r := validate.Compare(src, dst, &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: "circleci/0cbd7e08"}}, validate.Options{})
	sec, ok := section(t, r, pipelinesSectionName)
	if !ok {
		t.Fatal("pipeline section missing for an App destination")
	}
	_, missing, manual := sec.Counts()
	if missing != 0 || manual != 0 {
		t.Errorf("want only matched items, got %d missing / %d manual: %+v", missing, manual, sec.Items)
	}
	if itemStatus(sec, "/def:default") != validate.StatusMatched || itemStatus(sec, "/triggers") != validate.StatusMatched {
		t.Errorf("definition and triggers should match: %+v", sec.Items)
	}
}

func TestPipelines_MissingDefinitionIsMissing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{
		Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", false))},
	}}
	dst := appDest()
	dst.Projects = []manifest.Project{{Slug: "circleci/0cbd7e08/p1", Name: "web"}} // created with --skip-pipelines
	r := validate.Compare(src, dst, nil, validate.Options{})
	sec, _ := section(t, r, pipelinesSectionName)
	if itemStatus(sec, "/def:default") != validate.StatusMissing {
		t.Fatalf("a definition absent on the destination must be missing: %+v", sec.Items)
	}
	if !r.HasMissing() {
		t.Error("HasMissing must be true")
	}
}

func TestPipelines_TriggerCountAndState(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{
		Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", false), trig("pr", false))},
	}}

	// Fewer triggers than the source: missing.
	fewer := appDest()
	fewer.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", false))}}}
	sec, _ := section(t, validate.Compare(src, fewer, nil, validate.Options{}), pipelinesSectionName)
	if itemStatus(sec, "/triggers") != validate.StatusMissing {
		t.Errorf("fewer triggers must be missing: %+v", sec.Items)
	}

	// Same count but still disabled (created disabled, not yet enabled): manual.
	disabled := appDest()
	disabled.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml", trig("push", true), trig("pr", true))}}}
	r := validate.Compare(src, disabled, nil, validate.Options{})
	sec, _ = section(t, r, pipelinesSectionName)
	if itemStatus(sec, "/triggers") != validate.StatusMatched || itemStatus(sec, "/triggers/state") != validate.StatusManual {
		t.Errorf("present-but-disabled triggers: count matched, state manual: %+v", sec.Items)
	}
	if r.HasMissing() {
		t.Error("disabled triggers are a manual item, not a gap")
	}
}

func TestPipelines_DefinitionDetailMismatchIsManual(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml")}}}
	dst := appDest()
	dst.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", "ci/other.yml")}}}
	sec, _ := section(t, validate.Compare(src, dst, nil, validate.Options{}), pipelinesSectionName)
	if itemStatus(sec, "/def:default/source") != validate.StatusManual {
		t.Fatalf("a different config path must be reported: %+v", sec.Items)
	}
}

// An OAuth source project with nothing captured is expected to carry the
// synthesized build-and-test definition on the destination.
func TestPipelines_SynthesizedDefinitionExpectedForOAuthSource(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{Slug: "gh/acme/web", Name: "web"}}

	with := appDest()
	with.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("build-and-test", ".circleci/config.yml", trig("push", false))}}}
	sec, _ := section(t, validate.Compare(src, with, nil, validate.Options{}), pipelinesSectionName)
	if itemStatus(sec, "/def:build-and-test") != validate.StatusMatched {
		t.Errorf("synthesized definition should match: %+v", sec.Items)
	}

	none := appDest()
	none.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web"}}
	sec, _ = section(t, validate.Compare(src, none, nil, validate.Options{}), pipelinesSectionName)
	if itemStatus(sec, "/def:build-and-test") != validate.StatusMissing {
		t.Errorf("no destination definition at all must be missing: %+v", sec.Items)
	}

	own := appDest()
	own.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("custom", ".circleci/config.yml")}}}
	sec, _ = section(t, validate.Compare(src, own, nil, validate.Options{}), pipelinesSectionName)
	if itemStatus(sec, "/def:build-and-test") != validate.StatusMatched {
		t.Errorf("a destination with its own definitions is left alone by sync: %+v", sec.Items)
	}
}

// A source project absent from the destination is the Projects section's finding.
func TestPipelines_ProjectMissingOnDestIsNotDoubleReported(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{Slug: "gh/acme/web", Name: "web",
		PipelineDefinitions: []manifest.PipelineDefinition{appDef("default", ".circleci/config.yml")}}}
	sec, _ := section(t, validate.Compare(src, appDest(), nil, validate.Options{}), pipelinesSectionName)
	if len(sec.Items) != 0 {
		t.Errorf("expected no pipeline items for an absent project, got %+v", sec.Items)
	}
}

// The reported case: the source project's name (helloworld) differs from its
// slug tail (uipath-release-prototype) and the destination project carries the
// name — the project must still be found.
func TestProjects_MatchedByManifestNameWhenSlugTailDiffers(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{{Slug: "gh/acme/uipath-release-prototype", Name: "helloworld"}}
	dst := appDest()
	dst.Projects = []manifest.Project{{Slug: "circleci/o/p", Name: "helloworld"}}
	r := validate.Compare(src, dst, &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/acme", To: "circleci/0cbd7e08"}}, validate.Options{})
	sec, _ := section(t, r, "Projects")
	if itemStatus(sec, "gh/acme/uipath-release-prototype") != validate.StatusMatched {
		t.Errorf("project should match by manifest name: %+v", sec.Items)
	}
}

// ---------------------------------------------------------------------------
// Ignored contexts + informational org flag
// ---------------------------------------------------------------------------

func TestCompare_IgnoreContexts_SkipsTokenContext(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{Name: "migration-secrets", EnvVars: []manifest.ContextEnvVar{{Name: "CIRCLECI_DEST_TOKEN"}}},
		{Name: "deploy"},
	}
	dst := appDest()
	dst.Contexts = []manifest.Context{{Name: "deploy"}}

	missing := validate.Compare(src, dst, nil, validate.Options{})
	if !missing.HasMissing() {
		t.Fatal("precondition: without --ignore the token context is reported missing")
	}
	r := validate.Compare(src, dst, nil, validate.Options{IgnoreContexts: []string{"migration-secrets"}})
	if r.HasMissing() {
		t.Errorf("an ignored context must not be reported: %+v", r.Sections)
	}
	sec, _ := section(t, r, "Contexts")
	for _, it := range sec.Items {
		if strings.Contains(it.Name, "migration-secrets") {
			t.Errorf("ignored context still present in results: %+v", it)
		}
	}
}

func TestCompare_APITriggerFlagDifferenceIsInformational(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{FeatureFlags: map[string]bool{
		"allow_api_trigger_with_config": false,
		"other_flag":                    true,
	}}
	dst := appDest()
	dst.Source.Org.Settings = &manifest.OrgSettings{FeatureFlags: map[string]bool{
		"allow_api_trigger_with_config": true,
		"other_flag":                    true,
	}}
	r := validate.Compare(src, dst, nil, validate.Options{})
	sec, _ := section(t, r, "Org Settings")
	if got := itemStatus(sec, "feature-flag/allow_api_trigger_with_config"); got != validate.StatusInfo {
		t.Fatalf("status = %q, want info: %+v", got, sec.Items)
	}
	if r.HasMissing() {
		t.Error("an informational flag difference must not fail validation")
	}
	m, mi, mn := sec.Counts()
	if mi != 0 || mn != 0 || m != 1 || sec.InfoCount() != 1 {
		t.Errorf("counts: matched=%d missing=%d manual=%d info=%d; want 1/0/0/1", m, mi, mn, sec.InfoCount())
	}
	if !strings.Contains(r.TotalsLine(), "ℹ 1 info") {
		t.Errorf("totals line should mention the info item: %q", r.TotalsLine())
	}
}

// Other flags still fail when they differ.
func TestCompare_OtherFlagDifferenceStillMissing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{FeatureFlags: map[string]bool{"other_flag": true}}
	dst := appDest()
	dst.Source.Org.Settings = &manifest.OrgSettings{FeatureFlags: map[string]bool{"other_flag": false}}
	if r := validate.Compare(src, dst, nil, validate.Options{}); !r.HasMissing() {
		t.Error("a differing ordinary flag must still be missing")
	}
}
