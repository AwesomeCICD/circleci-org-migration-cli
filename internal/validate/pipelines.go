package validate

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// Pipeline sections only apply to a GitHub App ("circleci") destination, where
// every project is built through pipeline definitions + triggers.

const pipelinesSection = "Pipeline Definitions & Triggers"

// The definition sync synthesizes for an OAuth-source project that has no
// captured pipeline definitions (mirrors internal/syncer/synthesize.go; this
// package is deliberately free of the syncer's dependencies).
const (
	synthDefName    = "build-and-test"
	synthConfigPath = ".circleci/config.yml"
	synthProvider   = "github_app"
)

// isAppOrg reports whether m describes a GitHub App / standalone org.
func isAppOrg(m *manifest.Manifest) bool {
	return strings.EqualFold(m.Source.Org.VCSType, "circleci") ||
		strings.HasPrefix(m.Source.Org.Slug, "circleci/")
}

// projectIndex finds the destination counterpart of a source project: by the
// mapped slug, else by project name (the only way for a GitHub App destination,
// whose slugs carry UUIDs).
type projectIndex struct {
	bySlug map[string]manifest.Project
	byName map[string]manifest.Project
}

func newProjectIndex(dst *manifest.Manifest) projectIndex {
	ix := projectIndex{
		bySlug: make(map[string]manifest.Project, len(dst.Projects)),
		byName: make(map[string]manifest.Project, len(dst.Projects)),
	}
	for _, p := range dst.Projects {
		ix.bySlug[p.Slug] = p
		if p.Name != "" {
			ix.byName[p.Name] = p
		}
	}
	return ix
}

// find returns the destination project and the expected destination slug.
func (ix projectIndex) find(sp manifest.Project, mapping *manifest.Mapping) (manifest.Project, string, bool) {
	destSlug := sp.Slug
	if mapping != nil {
		if mapped, ok := mapping.ResolveProjectSlug(sp.Slug); ok {
			destSlug = mapped
		}
	}
	if dp, ok := ix.bySlug[destSlug]; ok {
		return dp, destSlug, true
	}
	// By name: the manifest name is what sync creates an App project under; the
	// slug tail is the fallback (and the only key for a renamed-org OAuth slug).
	for _, name := range []string{sp.Name, slugLastComponent(destSlug)} {
		if name == "" {
			continue
		}
		if dp, ok := ix.byName[name]; ok {
			return dp, destSlug, true
		}
	}
	return manifest.Project{}, destSlug, false
}

// expectedDefinitions is the set of definitions a destination project should
// have for source project sp: its captured definitions, or — for an OAuth
// project with none — the single definition sync synthesizes. synth is true in
// the latter case (the destination may carry its own definitions instead).
func expectedDefinitions(sp manifest.Project) (defs []manifest.PipelineDefinition, synth bool) {
	if len(sp.PipelineDefinitions) > 0 {
		return sp.PipelineDefinitions, false
	}
	if !strings.HasPrefix(sp.Slug, "gh/") && !strings.HasPrefix(sp.Slug, "github/") {
		return nil, false // nothing was captured and nothing would be synthesized
	}
	return []manifest.PipelineDefinition{{
		Name:         synthDefName,
		ConfigSource: manifest.PipelineSource{Provider: synthProvider, FilePath: synthConfigPath},
		Triggers:     []manifest.Trigger{{Name: "push"}},
	}}, true
}

// appProvider translates a captured provider to the one a GitHub App
// destination carries (github_oauth and empty become github_app).
func appProvider(p string) string {
	if p == "" || p == "github_oauth" {
		return "github_app"
	}
	return p
}

// comparePipelines checks, for every source project present on the destination,
// that each expected pipeline definition exists (matched by name; its config
// file path and provider are compared) and that its triggers are there (count,
// and how many are enabled). A project missing from the destination is reported
// by the Projects section, not here.
func comparePipelines(src, dst *manifest.Manifest, mapping *manifest.Mapping) Section {
	sec := Section{Name: pipelinesSection}
	ix := newProjectIndex(dst)

	add := func(status, name, detail string) {
		sec.Items = append(sec.Items, Item{Status: status, Section: pipelinesSection, Name: name, Detail: detail})
	}

	for _, sp := range src.Projects {
		dp, _, ok := ix.find(sp, mapping)
		if !ok {
			continue
		}
		want, synth := expectedDefinitions(sp)
		for _, wd := range want {
			target := sp.Slug + "/def:" + wd.Name
			dd, found := findDefinition(dp.PipelineDefinitions, wd.Name)
			if !found && synth && len(dp.PipelineDefinitions) > 0 {
				// Sync leaves a destination project that has its own definitions alone.
				add(StatusMatched, target, fmt.Sprintf("destination has its own %d pipeline definition(s); none synthesized", len(dp.PipelineDefinitions)))
				continue
			}
			if !found {
				add(StatusMissing, target, fmt.Sprintf("project %q: pipeline definition %q is missing on the destination — run sync --apply (without --skip-pipelines)", sp.Slug, wd.Name))
				continue
			}
			add(StatusMatched, target, "pipeline definition present")

			if d := definitionDiff(wd, dd); d != "" {
				add(StatusManual, target+"/source", fmt.Sprintf("project %q: pipeline definition %q differs from the source (%s) — align it manually", sp.Slug, wd.Name, d))
			}
			compareTriggers(sp.Slug, wd, dd, target, add)
		}
	}
	return sec
}

func findDefinition(defs []manifest.PipelineDefinition, name string) (manifest.PipelineDefinition, bool) {
	for _, d := range defs {
		if d.Name == name {
			return d, true
		}
	}
	return manifest.PipelineDefinition{}, false
}

// definitionDiff describes how the destination definition's config file path and
// config provider differ from the source's ("" when they agree).
func definitionDiff(want, got manifest.PipelineDefinition) string {
	var diffs []string
	if want.ConfigSource.FilePath != "" && got.ConfigSource.FilePath != want.ConfigSource.FilePath {
		diffs = append(diffs, fmt.Sprintf("config file %q → %q", want.ConfigSource.FilePath, orDash(got.ConfigSource.FilePath)))
	}
	if wp, gp := appProvider(want.ConfigSource.Provider), got.ConfigSource.Provider; gp != "" && gp != wp {
		diffs = append(diffs, fmt.Sprintf("config provider %q → %q", wp, gp))
	}
	return strings.Join(diffs, "; ")
}

// compareTriggers compares trigger count and enabled state for one definition.
// Triggers are created disabled by sync and enabled in a later step, so fewer
// enabled triggers than the source is a manual item (enable them), while fewer
// triggers than the source is missing.
func compareTriggers(slug string, want, got manifest.PipelineDefinition, target string, add func(status, name, detail string)) {
	if len(want.Triggers) == 0 {
		return
	}
	name := target + "/triggers"
	if len(got.Triggers) < len(want.Triggers) {
		add(StatusMissing, name, fmt.Sprintf("project %q definition %q: %d trigger(s) on source but only %d on destination",
			slug, want.Name, len(want.Triggers), len(got.Triggers)))
		return
	}
	add(StatusMatched, name, fmt.Sprintf("%d trigger(s) present", len(got.Triggers)))

	wantOn, gotOn := enabledTriggers(want.Triggers), enabledTriggers(got.Triggers)
	switch {
	case gotOn < wantOn:
		add(StatusManual, name+"/state", fmt.Sprintf("project %q definition %q: %d of %d source trigger(s) are enabled on the destination (%d enabled) — enable them when ready (sync --apply --yes --enable-existing-triggers)",
			slug, want.Name, wantOn, len(want.Triggers), gotOn))
	default:
		add(StatusMatched, name+"/state", fmt.Sprintf("%d enabled trigger(s) (source has %d)", gotOn, wantOn))
	}
}

func enabledTriggers(ts []manifest.Trigger) int {
	n := 0
	for _, t := range ts {
		if !t.Disabled {
			n++
		}
	}
	return n
}
