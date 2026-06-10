package syncer

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
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
func (s *Syncer) syncProjectWebhooks(report *Report, p manifest.Project, dst, destProjectID string, opts Options) {
	if len(p.Webhooks) == 0 {
		return
	}

	// Fetch existing destination webhooks for idempotency checking.
	var existing []project.Webhook
	if destProjectID != "" {
		if hooks, err := s.Projects.ListWebhooks(destProjectID); err == nil {
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
		if err := s.Projects.CreateWebhook(destProjectID, project.Webhook{
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
func (s *Syncer) syncProjectSchedules(report *Report, p manifest.Project, dst string, opts Options) {
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
	if scheds, err := s.Projects.ListSchedules(dst); err == nil {
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

		if err := s.Projects.CreateSchedule(dst, sc.Name, sc.Description, "system", sc.Timetable, sc.Parameters); err != nil {
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
func (s *Syncer) syncProjectOIDCClaims(report *Report, p manifest.Project, dst, destOrgID, destProjectID string, opts Options) {
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

	if err := s.Projects.SetProjectOIDCClaims(destOrgID, destProjectID, p.OIDCAudience, p.OIDCTTL); err != nil {
		report.add("project-oidc", target, "error", err.Error())
		return
	}
	report.add("project-oidc", target, "set",
		fmt.Sprintf("set project OIDC audience=%v ttl=%q", p.OIDCAudience, p.OIDCTTL))
}

// syncProjectV11Flags applies per-project v1.1 feature flags from the
// manifest to the destination.
//
//   - api_trigger_with_config: synced normally (status "set").
//   - drop_all_build_requests: DANGER flag — skipped with a "manual" warning,
//     mirroring the org-level danger-flag handling in orgsettings.go.
func (s *Syncer) syncProjectV11Flags(report *Report, p manifest.Project, dst string, opts Options) {
	if p.Settings == nil {
		return
	}
	settings := p.Settings

	// Build the map of flags to sync (snake_case keys).
	flags := map[string]*bool{
		"api_trigger_with_config": settings.APITriggerWithConfig,
		"drop_all_build_requests": settings.DropAllBuildRequests,
	}

	for flagKey, val := range flags {
		if val == nil {
			continue
		}
		target := dst + "/feature_flag:" + flagKey

		if dangerProjectFlags[flagKey] {
			report.add("project-flag", target, "manual",
				fmt.Sprintf("flag %q skipped: writing this flag to a new project is unsafe (it can stop all builds). Set manually after validating the destination project is ready.", flagKey))
			continue
		}

		if !opts.Apply {
			report.add("project-flag", target, "set",
				fmt.Sprintf("would set project feature flag %q = %v", flagKey, *val))
			continue
		}

		if err := s.Projects.SetV11ProjectFeatureFlags(dst, map[string]bool{flagKey: *val}); err != nil {
			report.add("project-flag", target, "error", err.Error())
			continue
		}
		report.add("project-flag", target, "set",
			fmt.Sprintf("set project feature flag %q = %v", flagKey, *val))
	}
}
