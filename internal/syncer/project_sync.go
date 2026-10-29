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

			// Issue #273: if the existing project is not followed (no webhook /
			// deploy key), queue it for the enable-builds step so a re-run is
			// idempotent for projects that were created but never followed.
			provider2, orgName2, repo2, splitErr2 := project.SplitSlug(dst)
			if splitErr2 == nil && strings.ToLower(provider2) != "circleci" {
				followed, followErr := s.Projects.IsProjectFollowed(ctx, provider2, orgName2, repo2)
				if followErr != nil {
					// Non-fatal: warn and skip the check; don't block the sync.
					s.logf("warning: could not check follow state of %q: %v — skipping auto-queue", dst, followErr)
				} else if !followed {
					report.add("project", dst, "exists", "not followed — queuing for enable-builds")
					report.PendingEnable = append(report.PendingEnable, EnableTarget{
						Kind:    "follow",
						Slug:    dst,
						VCSType: provider2,
						Org:     orgName2,
						Repo:    repo2,
					})
				}
			}
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

		// Reconcile pipeline definitions + triggers against the destination so a
		// re-run repairs a half-created project (e.g. the repo was not yet
		// connected to the GitHub App) and re-queues disabled triggers for
		// enable-builds. Idempotent: everything already present is "exists".
		s.syncAppPipelines(ctx, report, p, name, existing.ID, destSlug, mapping, opts, true)
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
		// syncAppPipelines is read-only at resolve time (no API writes).
		drySlug := "circleci/" + destOrgID + "/<new>"
		s.syncAppPipelines(ctx, report, p, name, "", destSlug, mapping, opts, false)
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
	s.syncAppPipelines(ctx, report, p, name, newProjectID, destSlug, mapping, opts, false)

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

// resolveExternalID returns the external_id to use for a pipeline-definition or
// trigger source, implementing the repo-move scenario logic:
//
//  1. Compute destFullName: apply GitHubOrg mapping from the Mapping (if set),
//     then fall back to DestGitHubOrg option, then leave unchanged.
//
//  2. GitHub org unchanged and an id was captured → reuse the captured id. No
//     GitHub token and no API call are needed (OAuth and App repos share the
//     same numeric GitHub repository id).
//
//  3. Otherwise (org changed, or no id was captured — i.e. an OAuth-source
//     definition being synthesized) the id must come from the GitHub API:
//     - token available → ResolveRepoID(destFullName).
//     success → use the new external_id; ErrRepoNotFound → "manual"
//     (repo missing in the dest GH org), ok=false; other error → "error".
//     - no token → "manual" asking for --github-token, ok=false.
//
//  4. Nothing to resolve from (no captured id, no full name) → "manual".
func (s *Syncer) resolveExternalID(ctx context.Context, report *Report, target, fullName, capturedID string, mapping *manifest.Mapping, opts Options) (string, bool) {
	// Step 1: compute destination full-name by applying the GH-org mapping.
	destFullName := s.mapRepoFullName(ctx, fullName, mapping, opts)
	orgChanged := destFullName != fullName

	// Step 2: same GitHub org and an id was captured → reuse it, no token needed.
	if capturedID != "" && !orgChanged {
		return capturedID, true
	}

	// Step 3: the id must be looked up via the GitHub API.
	if destFullName != "" {
		if opts.GitHubToken == "" {
			if orgChanged {
				report.add("project-ext-id", target, "manual",
					fmt.Sprintf("cannot resolve %s without --github-token; "+
						"provide a GitHub token (repo read) to verify/resolve repos in the new GitHub org", destFullName))
			} else {
				report.add("project-ext-id", target, "manual",
					fmt.Sprintf("no repo external_id was captured for %s and no --github-token (or $GITHUB_TOKEN) is set; "+
						"provide a GitHub token (repo read) so the id can be looked up, then re-run", destFullName))
			}
			return "", false
		}
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
		// Use "set" (not "resolved") so the action appears in the summary counts.
		report.add("project-ext-id", target, "set",
			fmt.Sprintf("repo %s found in destination GitHub org (id %s)", destFullName, id))
		return id, true
	}

	// Step 4: nothing available.
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
