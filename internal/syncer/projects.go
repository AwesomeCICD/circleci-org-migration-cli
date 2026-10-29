package syncer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// dangerProjectFlags are per-project feature flags that are skipped by default
// during sync because writing them can freeze or break the destination project.
//   - drop_all_build_requests: stops ALL builds from being triggered on the
//     destination project — extremely disruptive in a newly-migrated org.
var dangerProjectFlags = map[string]bool{
	"drop_all_build_requests": true,
}

// syncProjectWebhooks creates webhooks that are present in the source manifest
// but absent from the destination project.  It resolves the destination project
// UUID from the already-fetched destProjectID.
//
// Idempotency: an existing webhook with the same name AND url is treated as
// "exists" (no creation attempted).
//
// Safety note: the HMAC signing-secret is never readable from the source, so
// we always emit a "manual" action reminding the operator to set it.
func (s *Syncer) syncProjectWebhooks(ctx context.Context, report *Report, p manifest.Project, dst, destProjectID string, opts Options) {
	if len(p.Webhooks) == 0 {
		return
	}

	// Fetch existing destination webhooks for idempotency checking.
	var existing []project.Webhook
	if destProjectID != "" {
		if hooks, err := s.Projects.ListWebhooks(ctx, destProjectID); err == nil {
			existing = hooks
		}
	}

	for _, wh := range p.Webhooks {
		target := dst + "/webhook:" + wh.Name

		// Check idempotency: skip if same name+url already present.
		if hasWebhook(existing, wh.Name, wh.URL) {
			report.add("project-webhook", target, "exists", fmt.Sprintf("webhook %q already present", wh.Name))
			// Still emit the signing-secret notice even for existing webhooks.
			report.add("project-webhook", target, "manual",
				fmt.Sprintf("webhook %q signing-secret cannot be migrated automatically — set the HMAC secret manually on the destination webhook", wh.Name))
			continue
		}

		if !opts.Apply {
			report.add("project-webhook", target, "set", fmt.Sprintf("would create webhook %q", wh.Name))
			report.add("project-webhook", target, "manual",
				fmt.Sprintf("webhook %q signing-secret cannot be migrated automatically — set the HMAC secret manually on the destination webhook", wh.Name))
			continue
		}

		verifyTLS := wh.VerifyTLS
		if err := s.Projects.CreateWebhook(ctx, destProjectID, project.Webhook{
			Name:      wh.Name,
			URL:       wh.URL,
			Events:    wh.Events,
			VerifyTLS: verifyTLS,
		}); err != nil {
			report.add("project-webhook", target, "error", err.Error())
			continue
		}
		report.add("project-webhook", target, "set", fmt.Sprintf("created webhook %q", wh.Name))
		report.add("project-webhook", target, "manual",
			fmt.Sprintf("webhook %q signing-secret cannot be migrated automatically — set the HMAC secret manually on the destination webhook", wh.Name))
	}
}

// hasWebhook returns true when the slice already contains a webhook with both
// the same name and url.
func hasWebhook(existing []project.Webhook, name, rawURL string) bool {
	for _, e := range existing {
		if e.Name == name && e.URL == rawURL {
			return true
		}
	}
	return false
}

// syncProjectSchedules creates pipeline schedules from the manifest on the
// destination project.
//
// GitHub App ("circleci/" provider) destinations cannot use this endpoint —
// App-org schedules require the Trigger API (a future milestone).  When the
// destination slug has provider "circleci", all schedules are recorded as
// "manual" actions and no API calls are made.
//
// Idempotency: an existing schedule with the same name is treated as "exists".
func (s *Syncer) syncProjectSchedules(ctx context.Context, report *Report, p manifest.Project, dst string, opts Options) {
	if len(p.Schedules) == 0 {
		return
	}

	// Check if the destination is a GitHub App project (circleci/ provider).
	provider, _, _, err := project.SplitSlug(dst)
	if err != nil {
		// Invalid slug — already caught elsewhere; skip silently here.
		return
	}
	if strings.ToLower(provider) == "circleci" {
		for _, sc := range p.Schedules {
			target := dst + "/schedule:" + sc.Name
			report.add("project-schedule", target, "manual",
				fmt.Sprintf("schedule %q cannot be auto-created on a GitHub App destination — App-org schedules require the Trigger API (a future milestone); create manually", sc.Name))
		}
		return
	}

	// Fetch existing destination schedules for idempotency.
	var existing []project.Schedule
	if scheds, err := s.Projects.ListSchedules(ctx, dst); err == nil {
		existing = scheds
	}

	for _, sc := range p.Schedules {
		target := dst + "/schedule:" + sc.Name

		if hasSchedule(existing, sc.Name) {
			report.add("project-schedule", target, "exists", fmt.Sprintf("schedule %q already present", sc.Name))
			continue
		}

		if !opts.Apply {
			report.add("project-schedule", target, "set", fmt.Sprintf("would create schedule %q", sc.Name))
			continue
		}

		if err := s.Projects.CreateSchedule(ctx, dst, sc.Name, sc.Description, "system", sc.Timetable, sc.Parameters); err != nil {
			report.add("project-schedule", target, "error", err.Error())
			continue
		}
		report.add("project-schedule", target, "set", fmt.Sprintf("created schedule %q", sc.Name))
	}
}

// hasSchedule returns true when the slice already contains a schedule with
// the given name.
func hasSchedule(existing []project.Schedule, name string) bool {
	for _, e := range existing {
		if e.Name == name {
			return true
		}
	}
	return false
}

// syncProjectOIDCClaims applies the per-project OIDC audience and TTL to the
// destination when the manifest project has non-empty OIDC fields.
func (s *Syncer) syncProjectOIDCClaims(ctx context.Context, report *Report, p manifest.Project, dst, destOrgID, destProjectID string, opts Options) {
	if len(p.OIDCAudience) == 0 && p.OIDCTTL == "" {
		return
	}
	if destOrgID == "" || destProjectID == "" {
		report.add("project-oidc", dst, "manual",
			"cannot sync OIDC claims: destination org ID or project ID not available")
		return
	}

	target := dst + "/oidc_claims"
	if !opts.Apply {
		report.add("project-oidc", target, "set",
			fmt.Sprintf("would set project OIDC audience=%v ttl=%q", p.OIDCAudience, p.OIDCTTL))
		return
	}

	if err := s.Projects.SetProjectOIDCClaims(ctx, destOrgID, destProjectID, p.OIDCAudience, p.OIDCTTL); err != nil {
		report.add("project-oidc", target, "error", err.Error())
		return
	}
	report.add("project-oidc", target, "set",
		fmt.Sprintf("set project OIDC audience=%v ttl=%q", p.OIDCAudience, p.OIDCTTL))
}

// appProjectFlags are the v1.1 feature flags that still have an effect on a
// GitHub App ("circleci/") project. Everything else the OAuth source carried is
// either rejected by the monolith or a no-op there (see appDroppedFlags).
// drop_all_build_requests is additionally honoured only with --include-danger-flags.
var appProjectFlags = map[string]bool{
	"api_trigger_with_config":       true,
	"disable_ssh":                   true,
	"write_settings_requires_admin": true,
	"setup_workflows":               true,
	"autocancel_builds":             true,
}

// droppedFlag describes a v1.1 flag that is never written to an App project.
type droppedFlag struct {
	// reason is shown to the operator when the source value differs from def.
	reason string
	// def is the value a freshly created project has; a source value equal to it
	// loses nothing, so no manual line is emitted.
	def bool
}

// appDroppedFlags: flags never sent to a "circleci/" destination.
//
// The monolith (project.clj save-feature-flags!) validates ALL submitted flags
// before writing any: enabling forks-receive-secret-env-vars on a standalone
// project is a 403 with an empty body that fails the whole PUT, and "oss" is only
// valid for public repos. The remaining flags are accepted but inert on App
// projects, whose PR/fork/status behavior comes from the trigger's event_preset.
var appDroppedFlags = map[string]droppedFlag{
	"forks_receive_secret_env_vars": {
		reason: "CircleCI rejects it on standalone (GitHub App) projects with HTTP 403 and the rejection fails the whole flag update; secrets are never exposed to fork PRs there",
	},
	"oss": {
		reason: "only valid for public repositories and not supported on GitHub App projects",
	},
	"build_prs_only": {
		reason: "has no effect on GitHub App projects — PR-only behavior is set by the trigger's event_preset (check the recreated trigger)",
	},
	"build_fork_prs": {
		reason: "has no effect on GitHub App projects — fork-PR behavior is set by the trigger's event_preset (check the recreated trigger)",
	},
	"set_github_status": {
		reason: "has no effect on GitHub App projects — status reporting is part of the GitHub App integration",
		def:    true,
	},
}

// syncProjectV11Flags applies per-project v1.1 feature flags from the
// manifest to the destination.
//
//   - drop_all_build_requests: DANGER flag — skipped with a "manual" warning
//     unless --include-danger-flags, mirroring the org-level handling.
//   - GitHub App ("circleci/") destinations: only appProjectFlags are sent; the
//     rest (appDroppedFlags and anything unknown) are reported "manual" with the
//     reason when the source value would be lost. Sending forks_receive_secret_env_vars
//     or oss would make CircleCI reject the whole update.
//   - A flag whose source value already equals the destination's current value
//     is not sent (reported "exists").
//   - If the combined PUT is rejected, each flag is retried on its own so the
//     offending flag is named exactly.
func (s *Syncer) syncProjectV11Flags(ctx context.Context, report *Report, p manifest.Project, dst string, opts Options) {
	if p.Settings == nil {
		return
	}
	settings := p.Settings

	// Collect every captured project feature flag (snake_case keys). The full
	// v1.1 feature-flag map is authoritative and covers all advanced settings
	// (autocancel_builds, build_fork_prs, build_prs_only, set_github_status,
	// disable_ssh, forks_receive_secret_env_vars, setup_workflows,
	// write_settings_requires_admin, …). The two explicit struct fields are
	// merged in for backward-compat with older manifests that predate
	// V11FeatureFlags.
	candidates := map[string]bool{}
	if settings.APITriggerWithConfig != nil {
		candidates["api_trigger_with_config"] = *settings.APITriggerWithConfig
	}
	if settings.DropAllBuildRequests != nil {
		candidates["drop_all_build_requests"] = *settings.DropAllBuildRequests
	}
	for kebabKey, val := range settings.V11FeatureFlags {
		candidates[strings.ReplaceAll(kebabKey, "-", "_")] = val
	}

	provider, _, _, _ := project.SplitSlug(dst)
	isApp := strings.ToLower(provider) == "circleci"

	// Partition into the flags we will write (toSet), danger flags and flags
	// that never apply to an App project (both reported "manual").
	toSet := make(map[string]bool, len(candidates))
	for _, flagKey := range sortedFlagKeys(candidates) {
		val := candidates[flagKey]
		target := dst + "/feature_flag:" + flagKey
		if dangerProjectFlags[flagKey] && !opts.IncludeDangerFlags {
			if val {
				report.add("project-flag", target, "manual",
					fmt.Sprintf("flag %q skipped: source value is true but writing this flag to a new project is unsafe (it can stop all builds). Set it manually once the destination is validated, or re-run with --include-danger-flags.", flagKey))
			}
			continue
		}
		if isApp && !appProjectFlags[flagKey] && !dangerProjectFlags[flagKey] {
			if d, known := appDroppedFlags[flagKey]; known {
				if val != d.def {
					report.add("project-flag", target, "manual",
						fmt.Sprintf("flag %q (source value %v) not set on the GitHub App project: %s", flagKey, val, d.reason))
				}
			} else if val {
				report.add("project-flag", target, "manual",
					fmt.Sprintf("flag %q (source value true) not set on the GitHub App project: it is not applicable to App projects — set it manually if you still need it", flagKey))
			}
			continue
		}
		toSet[flagKey] = val
	}

	// Skip flags that already hold the wanted value on a destination project
	// that exists (read-only GET, so this also refines the dry-run).
	if len(toSet) > 0 && !strings.Contains(dst, "<new>") {
		if cur, err := s.Projects.GetV11ProjectFeatureFlags(ctx, dst); err != nil {
			clog.Debugf("could not read destination v1.1 flags for %s: %v", dst, err)
		} else {
			var same []string
			for _, flagKey := range sortedFlagKeys(toSet) {
				if have, ok := cur[strings.ReplaceAll(flagKey, "_", "-")]; ok && have == toSet[flagKey] {
					same = append(same, flagKey)
					delete(toSet, flagKey)
				}
			}
			if len(same) > 0 {
				report.add("project-flag", dst, "exists",
					fmt.Sprintf("project feature flag(s) already match the source: %s", strings.Join(same, ", ")))
			}
		}
	}
	if len(toSet) == 0 {
		return
	}

	if !opts.Apply {
		for _, flagKey := range sortedFlagKeys(toSet) {
			report.add("project-flag", dst+"/feature_flag:"+flagKey, "set",
				fmt.Sprintf("would set project feature flag %q = %v", flagKey, toSet[flagKey]))
		}
		return
	}

	// Apply in a single PUT (SetV11ProjectFeatureFlags converts snake→kebab).
	if err := s.Projects.SetV11ProjectFeatureFlags(ctx, dst, toSet); err == nil {
		for _, flagKey := range sortedFlagKeys(toSet) {
			report.add("project-flag", dst+"/feature_flag:"+flagKey, "set",
				fmt.Sprintf("set project feature flag %q = %v", flagKey, toSet[flagKey]))
		}
		return
	} else if len(toSet) == 1 {
		flagKey := sortedFlagKeys(toSet)[0]
		report.add("project-flag", dst+"/feature_flag:"+flagKey, "error",
			fmt.Sprintf("setting project feature flag %q = %v: %v", flagKey, toSet[flagKey], err))
		return
	}

	// The monolith validates every flag before writing any, so one rejected
	// flag fails them all. Retry one at a time to isolate the offender.
	for _, flagKey := range sortedFlagKeys(toSet) {
		target := dst + "/feature_flag:" + flagKey
		if err := s.Projects.SetV11ProjectFeatureFlags(ctx, dst, map[string]bool{flagKey: toSet[flagKey]}); err != nil {
			report.add("project-flag", target, "error",
				fmt.Sprintf("setting project feature flag %q = %v was rejected: %v", flagKey, toSet[flagKey], err))
			continue
		}
		report.add("project-flag", target, "set",
			fmt.Sprintf("set project feature flag %q = %v (individually; the combined update was rejected)", flagKey, toSet[flagKey]))
	}
}

// sortedFlagKeys returns the keys of m in sorted order (deterministic reports).
func sortedFlagKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
