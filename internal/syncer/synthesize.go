package syncer

import (
	"fmt"
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

// synthesizeOAuthPipelineDefinition onboards an OAuth-source project (one with
// no captured pipeline definitions) into a GitHub App destination by creating a
// single "build-and-test" pipeline definition plus one disabled trigger whose
// event_preset is translated from the project's OAuth build flags.
//
// The repo external_id is resolved via the shared resolveExternalID helper using
// the full_name derived from the OAuth slug (which applies the GH-org mapping and
// --github-token logic, returning found/missing/manual). A 404 (repo missing in
// the destination GitHub org) records a "manual" action and skips creation.
//
// Data-loss / behavior-change warnings are emitted for OAuth flags that have no
// GitHub App equivalent (fork builds, OSS, pr_only_branch_overrides).
//
// projectID is the destination project UUID; it is empty during the dry-run /
// preflight resolve pass, in which case no API writes occur and the planned
// actions are recorded instead.
func (s *Syncer) synthesizeOAuthPipelineDefinition(
	report *Report,
	projectName, projectID string,
	p manifest.Project,
	mapping *manifest.Mapping,
	opts Options,
) {
	fullName, ok := oauthSourceFullName(p.Slug)
	if !ok {
		report.add("project-pipeline-def", projectName, "manual",
			fmt.Sprintf("cannot derive a GitHub repo from OAuth slug %q — create the App pipeline definition manually", p.Slug))
		return
	}

	// Emit data-loss / behavior-change warnings before creation so they surface
	// even when the repo resolves and the definition is created.
	s.warnOAuthOnlyFlags(report, projectName, p.Settings)

	target := projectName + "/def:" + synthDefName
	extID, ok := s.resolveExternalID(report, target+"/config", fullName, "", mapping, opts)
	if !ok {
		// resolveExternalID already recorded a "manual"/"error" action (e.g. the
		// repo was not found in the destination GitHub org). Skip onboarding.
		return
	}

	preset := translateEventPreset(p.Settings)

	// Dry-run or no real project yet (preflight resolve pass): record intent only.
	if !opts.Apply || projectID == "" {
		report.add("project-pipeline-def", target, "created",
			fmt.Sprintf("would synthesize App pipeline definition %q (file %s, repo ext-id %s) for OAuth source",
				synthDefName, synthConfigPath, extID))
		report.add("project-trigger", target+"/trigger:push", "created",
			fmt.Sprintf("would synthesize trigger (event_preset %q, disabled — not yet enabled)", preset))
		return
	}

	spec := project.PipelineDefinitionSpec{
		Name:               synthDefName,
		ConfigProvider:     synthAppProvider,
		ConfigExternalID:   extID,
		ConfigFilePath:     synthConfigPath,
		CheckoutProvider:   synthAppProvider,
		CheckoutExternalID: extID,
	}

	defID, err := s.Projects.CreatePipelineDefinition(projectID, spec)
	if err != nil {
		if isRepoAccessError(err) {
			report.add("project-pipeline-def", target, "manual",
				fmt.Sprintf("create pipeline definition: %v — "+
					"the destination GitHub org must have the repository connected to the CircleCI GitHub App; "+
					"verify the App installation has access to the repo", err))
		} else {
			report.add("project-pipeline-def", target, "error",
				fmt.Sprintf("create pipeline definition: %v", err))
		}
		return
	}
	report.add("project-pipeline-def", target, "created",
		fmt.Sprintf("synthesized App pipeline definition %q (file %s) for OAuth source", synthDefName, synthConfigPath))

	trigSpec := project.TriggerSpec{
		Provider:    synthAppProvider,
		ExternalID:  extID,
		EventPreset: preset,
		Disabled:    true,
	}

	trigID, err := s.Projects.CreateTrigger(projectID, defID, trigSpec)
	if err != nil {
		report.add("project-trigger", target+"/trigger:push", "error",
			fmt.Sprintf("create trigger: %v", err))
		return
	}
	report.add("project-trigger", target+"/trigger:push", "created",
		fmt.Sprintf("synthesized trigger (event_preset %q, disabled — not yet enabled)", preset))

	// Queue for the enable-builds step (mirrors syncAppTrigger).
	report.PendingEnable = append(report.PendingEnable, EnableTarget{
		Kind:      "trigger",
		ProjectID: projectID,
		TriggerID: trigID,
	})
}

// warnOAuthOnlyFlags records "manual" actions for OAuth advanced-settings flags
// that have no GitHub App equivalent, so onboarding does not silently change
// behavior. It emits at most one action per applicable case.
func (s *Syncer) warnOAuthOnlyFlags(report *Report, projectName string, set *manifest.AdvancedSettings) {
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
