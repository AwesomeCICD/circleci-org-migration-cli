package syncer

import (
	"context"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// synthAppProvider is the config/checkout/event provider used for App pipeline
// definitions and triggers synthesized from an OAuth source project.
const synthAppProvider = "github_app"

// synthDefName is the name given to the single pipeline definition synthesized
// for an OAuth-source project onboarded into a GitHub App destination.
const synthDefName = "build-and-test"

// synthTriggerName is the name given to the synthesized push trigger.
const synthTriggerName = "push"

// synthConfigPath is the OAuth implicit-pipeline config path; OAuth projects
// have no pipeline definitions and always read .circleci/config.yml.
const synthConfigPath = ".circleci/config.yml"

// translateEventPreset maps an OAuth project's build flags to the GitHub App
// trigger event_preset.
//
//   - BuildPRsOnly == true  → "only-build-prs"
//   - otherwise             → "all-pushes"
//
// Note: "all-pushes" on a GitHub App trigger skips no-commit branch-creation
// events, a silent behavior difference versus OAuth all-pushes (which builds on
// branch creation). This is the closest available App equivalent.
func translateEventPreset(s *manifest.AdvancedSettings) string {
	if s != nil && s.BuildPRsOnly != nil && *s.BuildPRsOnly {
		return "only-build-prs"
	}
	return "all-pushes"
}

// oauthSourceFullName derives the GitHub repository full_name ("{org}/{repo}")
// from an OAuth project slug of the form "gh/{org}/{repo}" or
// "github/{org}/{repo}".
//
// The second return value is false when the slug is not an OAuth GitHub slug
// (for example a "circleci/..." App-source slug, which must not be synthesized),
// or when the slug is malformed.
func oauthSourceFullName(slug string) (string, bool) {
	provider, org, repo, err := project.SplitSlug(slug)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(provider) {
	case "gh", "github":
		return org + "/" + repo, true
	default:
		return "", false
	}
}

// synthesizedDefinition builds the single "build-and-test" pipeline definition
// (one push trigger, event_preset translated from the project's OAuth build
// flags) that onboards an OAuth-source project — one with no captured pipeline
// definitions — into a GitHub App destination. It carries no repo external_id
// (OAuth has none captured), so creating it always needs the GitHub API and
// therefore a GitHub token; fullName is the "{org}/{repo}" derived from the
// OAuth slug, to which the GH-org mapping is applied at resolve time.
func synthesizedDefinition(p manifest.Project, fullName string) manifest.PipelineDefinition {
	return manifest.PipelineDefinition{
		Name:           synthDefName,
		ConfigSource:   manifest.PipelineSource{Provider: synthAppProvider, RepoFullName: fullName, FilePath: synthConfigPath},
		CheckoutSource: manifest.PipelineSource{Provider: synthAppProvider, RepoFullName: fullName},
		Triggers: []manifest.Trigger{{
			Name:        synthTriggerName,
			EventPreset: translateEventPreset(p.Settings),
			EventSource: manifest.TriggerEventSource{Provider: synthAppProvider, RepoFullName: fullName},
		}},
	}
}

// needsSynthesizedDefinition reports whether onboarding this project into a
// GitHub App org requires a synthesized pipeline definition (and therefore a
// GitHub-resolved repo id): an OAuth-source project with no captured pipeline
// definitions. It is used by sync preflight to demand --github-token up front.
func needsSynthesizedDefinition(p manifest.Project) bool {
	if len(p.PipelineDefinitions) > 0 {
		return false
	}
	_, ok := oauthSourceFullName(p.Slug)
	return ok
}

// CountSynthesizedProjects returns how many manifest projects would need a
// synthesized App pipeline definition (see needsSynthesizedDefinition).
func CountSynthesizedProjects(m *manifest.Manifest) int {
	n := 0
	for _, p := range m.Projects {
		if needsSynthesizedDefinition(p) {
			n++
		}
	}
	return n
}

// SynthesizedProjectNames returns the destination project name (the name sync
// finds-or-creates the App project under) of every manifest project that would
// need a synthesized definition, so a caller can tell which of them already
// exist in the destination.
func SynthesizedProjectNames(m *manifest.Manifest) []string {
	var names []string
	for _, p := range m.Projects {
		if !needsSynthesizedDefinition(p) {
			continue
		}
		names = append(names, appProjectName(p))
	}
	return names
}

// appProjectName is the name an App-destination project is found or created
// under: the manifest name, else the last component of the source slug.
func appProjectName(p manifest.Project) string {
	if p.Name != "" {
		return p.Name
	}
	if _, _, repo, err := project.SplitSlug(p.Slug); err == nil {
		return repo
	}
	return p.Slug
}

// warnOAuthOnlyFlags records "manual" actions for OAuth advanced-settings flags
// that have no GitHub App equivalent, so onboarding does not silently change
// behavior. It emits at most one action per applicable case.
func (s *Syncer) warnOAuthOnlyFlags(ctx context.Context, report *Report, projectName string, set *manifest.AdvancedSettings) {
	if set == nil {
		return
	}
	target := projectName + "/def:" + synthDefName

	if derefBool(set.BuildForkPRs) || derefBool(set.ForksReceiveSecretEnvVars) {
		report.add("project-pipeline-def", target, "manual",
			"GitHub App pipelines never build forked PRs — this OAuth fork-build setting does not transfer")
	}
	if derefBool(set.OSS) {
		report.add("project-pipeline-def", target, "manual",
			"the OSS/Free-and-Open-Source flag is OAuth-only and does not apply to App projects")
	}
	if len(set.PROnlyBranchOverrides) > 0 {
		report.add("project-pipeline-def", target, "manual",
			"pr_only_branch_overrides has no GitHub App equivalent; replicate via per-branch triggers/pipeline-defs if needed")
	}
}

// derefBool returns the dereferenced value of a *bool, treating nil as false.
func derefBool(b *bool) bool {
	return b != nil && *b
}
