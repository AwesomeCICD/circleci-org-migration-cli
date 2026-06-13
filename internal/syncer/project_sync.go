package syncer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/github"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// SyncProjects creates and configures destination projects from the manifest.
//
// Destination org type detection: the first time a project needs creation, the
// syncer calls GetOrganization on the destination slug to read its vcs_type.
// If vcs_type == "circleci" (GitHub App), the App creation path is used for all
// projects; otherwise the existing OAuth/Bitbucket path is used.
//
// App path per project: find by name in the dest org via ListOrgProjects; if
// found reuse the existing project; if not found create it with CreateAppProject,
// then create pipeline definitions and (disabled) triggers, queuing each trigger
// as an App EnableTarget for the enable-builds step.
func (s *Syncer) SyncProjects(ctx context.Context, m *manifest.Manifest, bundle *manifest.SecretBundle, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	// Resolve the destination org ID once — needed for OIDC PATCH calls.
	destOrgID, err := s.Org.ResolveOrgID(ctx, destSlug)
	if err != nil {
		return nil, fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID

	s.logf("Syncing %d project(s)%s", len(m.Projects), dryRunSuffix(opts.Apply))

	// Detect destination org type once (lazy: only when a project actually needs
	// to be looked up/created by name).
	var destVCSType string // "circleci" for App; other values for OAuth
	destTypeLoaded := false

	// destOrgProjectsByName is the name→OrgProject map for App destinations,
	// built once on first use.
	var destOrgProjectsByName map[string]project.OrgProject
	destOrgProjectsLoaded := false

	loadDestType := func() error {
		if destTypeLoaded {
			return nil
		}
		destTypeLoaded = true
		o, err := s.Org.GetOrganization(ctx, destSlug)
		if err != nil {
			return fmt.Errorf("get destination org %q: %w", destSlug, err)
		}
		destVCSType = strings.ToLower(o.VCSType)
		return nil
	}

	loadDestOrgProjects := func() error {
		if destOrgProjectsLoaded {
			return nil
		}
		destOrgProjectsLoaded = true
		projs, err := s.Projects.ListOrgProjects(ctx, destOrgID)
		if err != nil {
			return fmt.Errorf("list destination org projects: %w", err)
		}
		destOrgProjectsByName = make(map[string]project.OrgProject, len(projs))
		for _, p := range projs {
			destOrgProjectsByName[p.Name] = p
		}
		return nil
	}

	for _, p := range m.Projects {
		// --- determine destination slug / path ---
		dst, ok := mapping.ResolveProjectSlug(p.Slug)
		if !ok {
			// No explicit mapping: need to detect dest org type to decide.
			if err := loadDestType(); err != nil {
				report.add("project", p.Slug, "error", err.Error())
				continue
			}
			if destVCSType == "circleci" {
				// App org: route through App path (dst will be derived after create).
				s.syncAppProject(ctx, report, p, destOrgID, destSlug, bundle, mapping, opts,
					loadDestOrgProjects, &destOrgProjectsByName)
				continue
			}
			// OAuth: no mapping → manual.
			report.add("project", p.Slug, "manual", "no destination mapping (a GitHub App destination needs an explicit project mapping)")
			continue
		}

		// Explicit mapping provided — detect dest type to decide path.
		if err := loadDestType(); err != nil {
			report.add("project", dst, "error", err.Error())
			continue
		}

		if destVCSType == "circleci" {
			// App org with explicit mapping: still go through App path.
			s.syncAppProject(ctx, report, p, destOrgID, destSlug, bundle, mapping, opts,
				loadDestOrgProjects, &destOrgProjectsByName)
			continue
		}

		// --- OAuth / Bitbucket path (existing behaviour) ---
		destProj, err := s.Projects.GetProject(ctx, dst)
		if err != nil {
			provider, orgName, repo, splitErr := project.SplitSlug(dst)
			if splitErr != nil {
				report.add("project", dst, "error", fmt.Sprintf("invalid destination slug: %v", splitErr))
				continue
			}
			if strings.ToLower(provider) == "circleci" {
				// App-org slug given explicitly via mapping but dest org is not
				// a circleci type (rare/misconfigured) — surface as manual.
				report.add("project", dst, "manual", "project not found in destination — App-org project creation requires a circleci-type destination org; create/follow it, then re-run")
				continue
			}

			// Create the project shell (paused — not followed).
			if !opts.Apply {
				report.add("project", dst, "created", "would create project (paused — not followed)")
			} else {
				created, createErr := s.Projects.CreateProjectShell(ctx, provider, orgName, repo)
				if createErr != nil {
					report.add("project", dst, "error", fmt.Sprintf("create project shell: %v", createErr))
					continue
				}
				destProj = created
				report.add("project", dst, "created", "created (paused — not followed)")
			}

			// Queue for the enable-builds step regardless of dry-run.
			report.PendingEnable = append(report.PendingEnable, EnableTarget{
				Kind:    "follow",
				Slug:    dst,
				VCSType: provider,
				Org:     orgName,
				Repo:    repo,
			})

			// For apply mode, re-fetch so we have a real project ID.
			if opts.Apply && (destProj == nil || destProj.ID == "") {
				if fetched, fetchErr := s.Projects.GetProject(ctx, dst); fetchErr == nil {
					destProj = fetched
				} else {
					s.logf("warning: could not re-fetch %q after create (webhooks skipped): %v", dst, fetchErr)
					destProj = &project.Project{Slug: dst}
				}
			}
			if !opts.Apply {
				destProj = &project.Project{Slug: dst}
			}
		} else {
			// Project already exists in destination — record it so re-runs are
			// visible in the report summary.
			report.add("project", dst, "exists", "reusing existing project")
		}

		s.syncProjectSettings(ctx, report, p, dst, opts)
		s.syncProjectVars(ctx, report, p, bundle, dst, opts)
		s.syncProjectSSHKeys(ctx, report, p, bundle, dst, opts)
		s.syncProjectWebhooks(ctx, report, p, dst, destProj.ID, opts)
		s.syncProjectSchedules(ctx, report, p, dst, opts)
		s.syncProjectOIDCClaims(ctx, report, p, dst, destOrgID, destProj.ID, opts)
		s.syncProjectV11Flags(ctx, report, p, dst, opts)
		s.syncProjectAPITokens(ctx, report, p, dst, opts)
	}
	return report, nil
}

// syncAppProject handles project sync when the destination org is a circleci-type
// (GitHub App) org. It finds or creates the project by name, then creates pipeline
// definitions and disabled triggers, queuing enable targets.
func (s *Syncer) syncAppProject(
	ctx context.Context,
	report *Report,
	p manifest.Project,
	destOrgID, destSlug string,
	bundle *manifest.SecretBundle,
	mapping *manifest.Mapping,
	opts Options,
	loadDestOrgProjects func() error,
	destOrgProjectsByName *map[string]project.OrgProject,
) {
	name := p.Name
	if name == "" {
		// Fall back to the last component of the source slug.
		_, _, repoName, err := project.SplitSlug(p.Slug)
		if err != nil {
			report.add("project", p.Slug, "error", fmt.Sprintf("project name and slug both invalid: %v", err))
			return
		}
		name = repoName
	}

	// Load existing dest-org projects once.
	if err := loadDestOrgProjects(); err != nil {
		report.add("project", name, "error", err.Error())
		return
	}

	existing, exists := (*destOrgProjectsByName)[name]

	if exists {
		// Reuse the existing project — record it so re-runs are visible in the
		// report summary, then configure settings/vars with the real slug.
		dst := existing.Slug
		report.add("project", dst, "exists", "reusing existing project")
		s.syncProjectSettings(ctx, report, p, dst, opts)
		s.syncProjectVars(ctx, report, p, bundle, dst, opts)
		s.syncProjectSSHKeys(ctx, report, p, bundle, dst, opts)
		s.syncProjectWebhooks(ctx, report, p, dst, existing.ID, opts)
		s.syncProjectSchedules(ctx, report, p, dst, opts)
		s.syncProjectOIDCClaims(ctx, report, p, dst, destOrgID, existing.ID, opts)
		s.syncProjectV11Flags(ctx, report, p, dst, opts)
		s.syncProjectAPITokens(ctx, report, p, dst, opts)
		return
	}

	// Project does not exist — create it.
	//
	// An OAuth-source project (gh//github slug) has no captured pipeline
	// definitions — OAuth uses an implicit pipeline — so we synthesize one App
	// pipeline definition + trigger, translating its build flags. An App-source
	// project (circleci/ slug) carries its own definitions and is recreated.
	synthesize := len(p.PipelineDefinitions) == 0
	nDefs := len(p.PipelineDefinitions)
	if !opts.Apply {
		if synthesize {
			report.add("project", name, "created",
				"would create App project + 1 synthesized pipeline-definition (OAuth source)")
		} else {
			report.add("project", name, "created",
				fmt.Sprintf("would create App project + %d pipeline-definition(s)", nDefs))
		}
		// Run resolve checks so the dry-run preview accurately shows which repos
		// are found in the destination GH org and which will be skipped.
		// syncAppPipelineDefinition is read-only at resolve time (no API writes).
		drySlug := "circleci/" + destOrgID + "/<new>"
		if synthesize {
			s.synthesizeOAuthPipelineDefinition(ctx, report, name, "", p, mapping, opts)
		}
		for _, def := range p.PipelineDefinitions {
			s.syncAppPipelineDefinition(ctx, report, name, "", def, destSlug, mapping, opts)
		}
		s.syncProjectSettings(ctx, report, p, drySlug, opts)
		s.syncProjectVars(ctx, report, p, bundle, drySlug, opts)
		s.syncProjectSSHKeys(ctx, report, p, bundle, drySlug, opts)
		s.syncProjectWebhooks(ctx, report, p, drySlug, "", opts)
		s.syncProjectSchedules(ctx, report, p, drySlug, opts)
		s.syncProjectOIDCClaims(ctx, report, p, drySlug, destOrgID, "", opts)
		s.syncProjectV11Flags(ctx, report, p, drySlug, opts)
		s.syncProjectAPITokens(ctx, report, p, drySlug, opts)
		return
	}

	// Apply: create the project.
	clog.Debugf("CreateAppProject dest_org_id=%s name=%s", destOrgID, name)
	created, err := s.Projects.CreateAppProject(ctx, destOrgID, name)
	if err != nil {
		report.add("project", name, "error", fmt.Sprintf("create App project: %v", err))
		return
	}
	report.add("project", name, "created", "created App project")
	newSlug := created.Slug
	newProjectID := created.ID

	// Update the local cache so subsequent projects (if re-run) see this one.
	(*destOrgProjectsByName)[name] = project.OrgProject{
		ID:   newProjectID,
		Slug: newSlug,
		Name: name,
	}

	// Create pipeline definitions and triggers. OAuth-source projects (no
	// captured definitions) get one synthesized App pipeline-def + trigger;
	// App-source projects recreate their captured definitions.
	if synthesize {
		s.synthesizeOAuthPipelineDefinition(ctx, report, name, newProjectID, p, mapping, opts)
	}
	for _, def := range p.PipelineDefinitions {
		s.syncAppPipelineDefinition(ctx, report, name, newProjectID, def, destSlug, mapping, opts)
	}

	// Configure settings, vars, etc. on the new slug.
	s.syncProjectSettings(ctx, report, p, newSlug, opts)
	s.syncProjectVars(ctx, report, p, bundle, newSlug, opts)
	s.syncProjectSSHKeys(ctx, report, p, bundle, newSlug, opts)
	s.syncProjectWebhooks(ctx, report, p, newSlug, newProjectID, opts)
	s.syncProjectSchedules(ctx, report, p, newSlug, opts)
	s.syncProjectOIDCClaims(ctx, report, p, newSlug, destOrgID, newProjectID, opts)
	s.syncProjectV11Flags(ctx, report, p, newSlug, opts)
	s.syncProjectAPITokens(ctx, report, p, newSlug, opts)
}

// syncAppPipelineDefinition creates one pipeline definition plus its triggers
// for a freshly-created App project.
func (s *Syncer) syncAppPipelineDefinition(
	ctx context.Context,
	report *Report,
	projectName, projectID string,
	def manifest.PipelineDefinition,
	destSlug string,
	mapping *manifest.Mapping,
	opts Options,
) {
	// Resolve the external_id for config and checkout sources.
	configExtID, configOK := s.resolveExternalID(ctx, report, projectName+"/def:"+def.Name+"/config",
		def.ConfigSource.RepoFullName, def.ConfigSource.RepoExternalID, mapping, opts)
	if !configOK {
		return
	}
	checkoutExtID, checkoutOK := s.resolveExternalID(ctx, report, projectName+"/def:"+def.Name+"/checkout",
		def.CheckoutSource.RepoFullName, def.CheckoutSource.RepoExternalID, mapping, opts)
	if !checkoutOK {
		return
	}

	filePath := def.ConfigSource.FilePath
	if filePath == "" {
		filePath = ".circleci/config.yml"
	}
	configProvider := def.ConfigSource.Provider
	if configProvider == "" {
		configProvider = "github_app"
	}
	checkoutProvider := def.CheckoutSource.Provider
	if checkoutProvider == "" {
		checkoutProvider = "github_app"
	}

	spec := project.PipelineDefinitionSpec{
		Name:               def.Name,
		Description:        def.Description,
		ConfigProvider:     configProvider,
		ConfigExternalID:   configExtID,
		ConfigFilePath:     filePath,
		CheckoutProvider:   checkoutProvider,
		CheckoutExternalID: checkoutExtID,
	}

	// Dry-run or no real project yet (preflight resolve pass): record intent only.
	if !opts.Apply || projectID == "" {
		report.add("project-pipeline-def", projectName+"/def:"+def.Name, "created",
			fmt.Sprintf("would create pipeline definition (config ext-id %s, checkout ext-id %s)", configExtID, checkoutExtID))
		// Still preview trigger actions.
		for _, trig := range def.Triggers {
			s.syncAppTrigger(ctx, report, projectName, projectID, def.Name, "", trig, mapping, opts)
		}
		return
	}

	defID, err := s.Projects.CreatePipelineDefinition(ctx, projectID, spec)
	if err != nil {
		// "Installation does not have access to repository" (and similar
		// access/installation errors) mean the dest GitHub App installation
		// cannot see the repo — this is an operator action, not a code bug.
		// Record as "manual" with a clear remediation note so the run continues.
		if isRepoAccessError(err) {
			report.add("project-pipeline-def", projectName+"/def:"+def.Name, "manual",
				fmt.Sprintf("create pipeline definition: %v — "+
					"the destination GitHub org must have the repository connected to the CircleCI GitHub App; "+
					"verify the App installation has access to the repo, and that the external_id resolves to a "+
					"repository the dest installation can access (use --github-token to resolve a moved repo's id)", err))
		} else {
			report.add("project-pipeline-def", projectName+"/def:"+def.Name, "error",
				fmt.Sprintf("create pipeline definition: %v", err))
		}
		return
	}
	report.add("project-pipeline-def", projectName+"/def:"+def.Name, "created",
		"created pipeline definition")

	// Create triggers.
	for _, trig := range def.Triggers {
		s.syncAppTrigger(ctx, report, projectName, projectID, def.Name, defID, trig, mapping, opts)
	}
}

// syncAppTrigger creates one trigger (disabled) on a pipeline definition and
// queues an App EnableTarget for later enablement.
func (s *Syncer) syncAppTrigger(
	ctx context.Context,
	report *Report,
	projectName, projectID, defName, defID string,
	trig manifest.Trigger,
	mapping *manifest.Mapping,
	opts Options,
) {
	target := projectName + "/def:" + defName + "/trigger:" + trig.Name
	provider := trig.EventSource.Provider

	// Only github_app / github_server triggers are recreated automatically.
	// webhook and schedule triggers must be handled manually.
	switch provider {
	case "github_app", "github_server":
		// Continue below.
	case "webhook":
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has provider %q (webhook secret cannot be migrated automatically) — recreate manually", trig.Name, provider))
		return
	case "schedule":
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has provider %q (schedule-trigger creation is a follow-on) — recreate manually", trig.Name, provider))
		return
	default:
		report.add("project-trigger", target, "manual",
			fmt.Sprintf("trigger %q has unsupported provider %q — recreate manually", trig.Name, provider))
		return
	}

	extID, ok := s.resolveExternalID(ctx, report, target,
		trig.EventSource.RepoFullName, trig.EventSource.RepoExternalID, mapping, opts)
	if !ok {
		return
	}

	// Dry-run or no real definition yet (preflight resolve pass): record intent only.
	if !opts.Apply || defID == "" {
		report.add("project-trigger", target, "created", "would create trigger (disabled — not yet enabled)")
		return
	}

	trigSpec := project.TriggerSpec{
		Provider:    provider,
		ExternalID:  extID,
		EventPreset: trig.EventPreset,
		Disabled:    true,
	}

	trigID, err := s.Projects.CreateTrigger(ctx, projectID, defID, trigSpec)
	if err != nil {
		report.add("project-trigger", target, "error",
			fmt.Sprintf("create trigger: %v", err))
		return
	}
	report.add("project-trigger", target, "created", "created trigger (disabled — not yet enabled)")

	// Queue for the enable-builds step.
	report.PendingEnable = append(report.PendingEnable, EnableTarget{
		Kind:      "trigger",
		ProjectID: projectID,
		TriggerID: trigID,
	})
}

// resolveExternalID returns the external_id to use for a pipeline-definition or
// trigger source, implementing the repo-move scenario logic:
//
//  1. Compute destFullName: apply GitHubOrg mapping from the Mapping (if set),
//     then fall back to DestGitHubOrg option, then leave unchanged.
//
//  2. If GitHubToken != "" and fullName != "": call ResolveRepoID(destFullName).
//     - success → use the new external_id (repo found in dest GH org).
//     - ErrRepoNotFound → emit "manual" with remediation note; return ok=false
//     (skip project creation — repo not accessible to dest App installation).
//     - other error → emit "error" action; return ok=false.
//
//  3. If GitHubToken == "":
//     - destFullName == fullName (GH org unchanged) → reuse capturedID silently
//     (current same-org behaviour).
//     - destFullName != fullName (org changed, no token) → emit "manual"; return
//     ok=false (cannot resolve without a token).
//
//  4. If capturedID is non-empty and no name to resolve → reuse capturedID.
//
//  5. No external_id at all → emit "manual"; return ok=false.
func (s *Syncer) resolveExternalID(ctx context.Context, report *Report, target, fullName, capturedID string, mapping *manifest.Mapping, opts Options) (string, bool) {
	// Step 1: compute destination full-name by applying the GH-org mapping.
	destFullName := s.mapRepoFullName(ctx, fullName, mapping, opts)

	if opts.GitHubToken != "" && destFullName != "" {
		// Step 2: token available — call the GitHub API.
		id, err := resolveRepoID(ctx, destFullName, opts.GitHubToken, "")
		if err != nil {
			if isNotFound(err) {
				// Repo missing in dest GH org → skip onboarding, emit manual.
				report.add("project-ext-id", target, "manual",
					fmt.Sprintf("repo %s not found in the destination GitHub org — "+
						"move/connect it to the dest CircleCI GitHub App, then re-run", destFullName))
				return "", false
			}
			// Other error → surface as error action.
			report.add("project-ext-id", target, "error",
				fmt.Sprintf("GitHub repo ID resolution failed for %q: %v", destFullName, err))
			return "", false
		}
		// Success: log that the repo was found (helpful in dry-run preview too).
		// Use "set" (not "resolved") so the action appears in the summary counts.
		report.add("project-ext-id", target, "set",
			fmt.Sprintf("repo %s found in destination GitHub org (id %s)", destFullName, id))
		return id, true
	}

	// Step 3: no token.
	if destFullName != "" && destFullName != fullName {
		// GH org changed but no token to verify — must skip.
		report.add("project-ext-id", target, "manual",
			fmt.Sprintf("cannot resolve %s without --github-token; "+
				"provide a GitHub token (repo read) to verify/resolve repos in the new GitHub org", destFullName))
		return "", false
	}

	// Step 4: same GH org (or no full-name) → reuse captured id.
	if capturedID != "" {
		return capturedID, true
	}

	// Step 5: nothing available.
	report.add("project-ext-id", target, "manual",
		"no external_id available (no GitHub token and no captured id) — create pipeline definition manually")
	return "", false
}

// mapRepoFullName applies the GH-org mapping to a source repo full-name.
// Precedence: Mapping.GitHubOrg > opts.DestGitHubOrg > unchanged.
func (s *Syncer) mapRepoFullName(ctx context.Context, sourceFullName string, mapping *manifest.Mapping, opts Options) string {
	if sourceFullName == "" {
		return sourceFullName
	}
	// Explicit mapping wins.
	if mapping != nil && mapping.GitHubOrg != nil {
		return mapping.MapRepoFullName(sourceFullName)
	}
	// DestGitHubOrg convenience option.
	if opts.DestGitHubOrg != "" {
		// Replace the owner portion (everything before the first "/").
		slash := strings.Index(sourceFullName, "/")
		if slash > 0 {
			return opts.DestGitHubOrg + sourceFullName[slash:]
		}
	}
	return sourceFullName
}

// isNotFound returns true when err wraps or equals github.ErrRepoNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, github.ErrRepoNotFound)
}

// EnableBuilds enables builds for a project that was previously created paused.
//
// For OAuth projects (Kind == "follow" or legacy empty Kind): calls FollowProject
// which installs the webhook and may trigger an initial build.
//
// For App trigger targets (Kind == "trigger"): calls EnableTrigger to set
// disabled=false on the specified trigger.
//
// In dry-run mode (apply=false) no API call is made and the returned Action has
// status "manual" with a detail explaining what would happen.  In apply mode the
// appropriate API is called and the Action status reflects the result.
func (s *Syncer) EnableBuilds(ctx context.Context, t EnableTarget, apply bool) (Action, error) {
	switch t.Kind {
	case "trigger":
		target := t.ProjectID + "/trigger/" + t.TriggerID
		if !apply {
			return Action{
				Kind:   "project",
				Target: target,
				Status: "manual",
				Detail: "would enable trigger (set disabled=false)",
			}, nil
		}
		if err := s.Projects.EnableTrigger(ctx, t.ProjectID, t.TriggerID); err != nil {
			return Action{
				Kind:   "project",
				Target: target,
				Status: "error",
				Detail: fmt.Sprintf("enable trigger: %v", err),
			}, err
		}
		return Action{
			Kind:   "project",
			Target: target,
			Status: "set",
			Detail: "enabled trigger (disabled=false)",
		}, nil
	default: // "follow" or legacy empty Kind
		if !apply {
			return Action{
				Kind:   "project",
				Target: t.Slug,
				Status: "manual",
				Detail: "would enable builds (follow)",
			}, nil
		}
		_, err := s.Projects.FollowProject(ctx, t.VCSType, t.Org, t.Repo)
		if err != nil {
			return Action{
				Kind:   "project",
				Target: t.Slug,
				Status: "error",
				Detail: fmt.Sprintf("enable builds (follow): %v", err),
			}, err
		}
		return Action{
			Kind:   "project",
			Target: t.Slug,
			Status: "set",
			Detail: "enabled builds (followed)",
		}, nil
	}
}

func (s *Syncer) syncProjectSettings(ctx context.Context, report *Report, p manifest.Project, dst string, opts Options) {
	if p.Settings == nil {
		return
	}
	provider, org, proj, err := project.SplitSlug(dst)
	if err != nil {
		report.add("project-settings", dst, "error", err.Error())
		return
	}

	sourceOSS := p.Settings.OSS != nil && *p.Settings.OSS

	if !opts.Apply {
		report.add("project-settings", dst, "set", "would update advanced settings")
		if sourceOSS {
			// Preview the OSS best-effort attempt so operators know it will run.
			report.add("project-oss", dst, "set", "would attempt to set OSS flag (best-effort)")
		}
		return
	}

	settings := toProjectSettings(p.Settings)

	// GitHub App (circleci/ provider) projects reject OAuth-only fork/PR fields
	// with "Unexpected field 'advanced.oss'" (observed in live App→App migration).
	// App projects never build fork PRs, so these fields are not applicable.
	// Strip them from the PATCH so the write succeeds; do not mutate the manifest.
	if strings.ToLower(provider) == "circleci" {
		settings = stripOAuthOnlySettings(settings)
	}

	if err := s.Projects.UpdateSettings(ctx, provider, org, proj, settings); err != nil {
		report.add("project-settings", dst, "error", err.Error())
		return
	}
	report.add("project-settings", dst, "set", "updated advanced settings")

	// Best-effort OSS apply: only when the source project had oss=true.
	// This is a SEPARATE PATCH so a rejection (e.g. "Unexpected field
	// 'advanced.oss'" from GitHub App projects) never fails the main sync.
	if sourceOSS {
		s.applyOSSBestEffort(ctx, report, provider, org, proj, dst)
	}
}

// applyOSSBestEffort attempts to set oss=true on the destination project via
// a dedicated, isolated PATCH.  It records:
//   - "set"    when the API confirmed oss=true.
//   - "manual" when the field was not applied (GitHub App auto-detects from
//     repo visibility; OAuth no-op on private repos) with a clear remediation
//     note so the operator knows what to do next.
//
// The main project sync always continues regardless of the outcome here.
func (s *Syncer) applyOSSBestEffort(ctx context.Context, report *Report, provider, org, proj, dst string) {
	applied, err := s.Projects.SetOSS(ctx, provider, org, proj)
	if err != nil {
		// Genuine transport or API error — log as manual so the operator is
		// alerted, but do NOT fail the project sync.
		report.add("project-oss", dst, "manual",
			fmt.Sprintf("OSS flag could not be set (API error: %v) — "+
				ossManualNote, err))
		return
	}
	if !applied {
		// API accepted the call but did not echo oss=true back (GitHub App
		// auto-detects from repo visibility; private OAuth repos no-op).
		report.add("project-oss", dst, "manual", ossManualNote)
		return
	}
	report.add("project-oss", dst, "set", "OSS flag enabled on destination project")
}

// ossManualNote is the standard remediation message emitted when the OSS flag
// could not be applied automatically.
const ossManualNote = "OSS status not set automatically — " +
	"public repos under the GitHub App are auto-detected from repo visibility; " +
	"otherwise enable 'Free and Open Source' in the destination project's Advanced settings."

func (s *Syncer) syncProjectVars(ctx context.Context, report *Report, p manifest.Project, bundle *manifest.SecretBundle, dst string, opts Options) {
	values := map[string]string{}
	if bundle != nil {
		values = bundle.ProjectSecrets[p.Slug] // keyed by the SOURCE slug
	}
	// Project env vars are not idempotent (no upsert), so skip names that
	// already exist in the destination.
	// Guard with Apply: in dry-run mode the project slug may be a placeholder
	// (e.g. "circleci/<org-id>/<new>") that would cause a doomed HTTP call.
	existing := map[string]bool{}
	if opts.Apply {
		if vars, err := s.Projects.ListEnvVars(ctx, dst); err == nil {
			for _, v := range vars {
				existing[v.Name] = true
			}
		}
	}
	for _, v := range p.EnvVars {
		target := dst + "/" + v.Name
		if existing[v.Name] {
			report.add("project-var", target, "exists", "variable already present")
			continue
		}
		val, ok := values[v.Name]
		if !ok {
			if opts.MissingSecrets == MissingPlaceholder {
				if err := s.createVar(ctx, dst, v.Name, opts.placeholder(), opts.Apply); err != nil {
					report.add("project-var", target, "error", err.Error())
					continue
				}
				report.add("project-var", target, "set", "placeholder — value not captured; replace manually")
			} else {
				report.add("project-var", target, "manual", "value not captured; set manually")
			}
			continue
		}
		if err := s.createVar(ctx, dst, v.Name, val, opts.Apply); err != nil {
			report.add("project-var", target, "error", err.Error())
			continue
		}
		report.add("project-var", target, "set", "value set from bundle")
	}
}

func (s *Syncer) createVar(ctx context.Context, slug, name, value string, apply bool) error {
	if !apply {
		return nil
	}
	return s.Projects.CreateEnvVar(ctx, slug, name, value)
}

func toProjectSettings(s *manifest.AdvancedSettings) *project.AdvancedSettings {
	return &project.AdvancedSettings{
		AutocancelBuilds:           s.AutocancelBuilds,
		BuildForkPRs:               s.BuildForkPRs,
		BuildPRsOnly:               s.BuildPRsOnly,
		DisableSSH:                 s.DisableSSH,
		ForksReceiveSecretEnvVars:  s.ForksReceiveSecretEnvVars,
		OSS:                        s.OSS,
		SetGithubStatus:            s.SetGitHubStatus,
		SetupWorkflows:             s.SetupWorkflows,
		WriteSettingsRequiresAdmin: s.WriteSettingsRequiresAdmin,
		PROnlyBranchOverrides:      s.PROnlyBranchOverrides,
	}
}

// stripOAuthOnlySettings returns a copy of s with the four OAuth-only / fork-PR
// fields zeroed out.  GitHub App projects reject these fields with
// "Unexpected field 'advanced.oss'" because App never builds fork PRs.
//
// Stripped fields: OSS, BuildForkPRs, ForksReceiveSecretEnvVars, PROnlyBranchOverrides.
// All other fields (autocancel_builds, set_github_status, setup_workflows, etc.)
// are valid for App and are preserved.
func stripOAuthOnlySettings(s *project.AdvancedSettings) *project.AdvancedSettings {
	cp := *s // shallow copy — all pointer fields stay independent
	cp.OSS = nil
	cp.BuildForkPRs = nil
	cp.ForksReceiveSecretEnvVars = nil
	cp.PROnlyBranchOverrides = nil
	return &cp
}

// isRepoAccessError returns true when err indicates that the GitHub App
// installation does not have access to the target repository.  These errors
// require the operator to connect the repo to the CircleCI App; they are not
// transient or code bugs, so callers record them as "manual" rather than "error".
func isRepoAccessError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "does not have access to repository") ||
		strings.Contains(msg, "installation does not have access")
}

// syncProjectAPITokens handles project API token sync for a single project.
//
// Default behaviour (opts.CreateProjectTokens == false): emit a "manual" action
// for each captured token so the operator knows to recreate them.
//
// Optional behaviour (opts.CreateProjectTokens == true AND opts.Apply == true):
// for each captured token, check whether a token with the same label+scope
// already exists on the destination project (idempotent skip if so), then call
// CreateProjectToken and print the NEW plaintext value to s.Out (stderr) with a
// "save these now" header. Values are NEVER written to any log, stdout, or JSON
// output stream.
func (s *Syncer) syncProjectAPITokens(ctx context.Context, report *Report, p manifest.Project, dst string, opts Options) {
	if len(p.APITokens) == 0 {
		return
	}

	if !opts.CreateProjectTokens {
		// Default: flag each token as requiring manual recreation.
		for _, t := range p.APITokens {
			target := dst + "/api-token:" + t.Label
			report.add("project-api-token", target, "manual",
				fmt.Sprintf("API token %q (scope %s) must be recreated manually on the destination project and every consumer repointed — values are not recoverable", t.Label, t.Scope))
		}
		return
	}

	// --create-project-tokens is set.
	// Check which tokens already exist on the destination (idempotent).
	existingByLabelScope := map[string]bool{}
	if opts.Apply {
		existing, lerr := s.Projects.ListProjectTokens(ctx, dst)
		if lerr != nil {
			clog.Debugf("could not list existing project tokens on %s: %v", dst, lerr)
			// Do not abort — proceed and let create calls fail/succeed individually.
		} else {
			for _, ex := range existing {
				existingByLabelScope[ex.Label+"\x00"+ex.Scope] = true
			}
		}
	}

	var createdTokens []string // label:value pairs, only in apply mode

	for _, t := range p.APITokens {
		target := dst + "/api-token:" + t.Label
		key := t.Label + "\x00" + t.Scope

		if existingByLabelScope[key] {
			report.add("project-api-token", target, "exists",
				fmt.Sprintf("API token %q (scope %s) already exists on destination — skipping (idempotent)", t.Label, t.Scope))
			continue
		}

		if !opts.Apply {
			report.add("project-api-token", target, "created",
				fmt.Sprintf("would create API token %q (scope %s) — new value printed to stderr once on apply", t.Label, t.Scope))
			continue
		}

		plaintext, cerr := s.Projects.CreateProjectToken(ctx, dst, t.Scope, t.Label)
		if cerr != nil {
			report.add("project-api-token", target, "error",
				fmt.Sprintf("create API token %q (scope %s): %v", t.Label, t.Scope, cerr))
			continue
		}
		report.add("project-api-token", target, "created",
			fmt.Sprintf("created API token %q (scope %s) — new plaintext value printed to stderr", t.Label, t.Scope))
		createdTokens = append(createdTokens, fmt.Sprintf("  project %s | label: %s | scope: %s | token: %s", dst, t.Label, t.Scope, plaintext))
	}

	// Print plaintext values to the operator output stream (stderr) ONCE.
	// Never to stdout / JSON; never to a log file.
	if len(createdTokens) > 0 && s.Out != nil {
		fmt.Fprintf(s.Out, "\n*** SAVE THESE PROJECT API TOKEN VALUES NOW — they cannot be re-read ***\n")
		fmt.Fprintf(s.Out, "*** Repoint every consumer of each source token to the new value.    ***\n\n")
		for _, line := range createdTokens {
			fmt.Fprintln(s.Out, line)
		}
		fmt.Fprintf(s.Out, "\n*** End of new project API token values ***\n\n")
	}
}
