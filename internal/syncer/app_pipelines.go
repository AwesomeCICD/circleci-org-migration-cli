package syncer

import (
	"context"
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/project"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// syncAppPipelines makes a GitHub App destination project's pipeline
// definitions and triggers match the manifest.
//
// It serves both a freshly-created project (existing == false: nothing to list,
// every captured definition/trigger is created) and a project that already
// exists in the destination (existing == true: the destination's definitions and
// triggers are listed and only what is missing is created — a re-run therefore
// repairs a half-created project and reports everything already present as
// "exists"). Triggers created disabled by this tool, and existing-but-disabled
// triggers whose source trigger was enabled, are queued for the enable-builds
// step so `sync --apply --yes` enables them.
//
// An App-source project (circleci/ slug) or an OAuth project with GitHub App
// pipelines recreates its captured definitions, translating provider
// github_oauth to github_app. A project with zero captured definitions (a plain
// OAuth project) gets one synthesized "build-and-test" definition + trigger.
//
// projectID is empty during the dry-run / preflight resolve pass for a project
// that does not exist yet, in which case no API calls are made and the planned
// actions are recorded instead.
func (s *Syncer) syncAppPipelines(
	ctx context.Context,
	report *Report,
	p manifest.Project,
	projectName, projectID, destSlug string,
	mapping *manifest.Mapping,
	opts Options,
	existing bool,
) {
	defs := p.PipelineDefinitions
	synth := len(defs) == 0
	if synth {
		fullName, ok := oauthSourceFullName(p.Slug)
		if !ok {
			report.add("project-pipeline-def", projectName, "manual",
				fmt.Sprintf("cannot derive a GitHub repo from OAuth slug %q — create the App pipeline definition manually", p.Slug))
			return
		}
		defs = []manifest.PipelineDefinition{synthesizedDefinition(p, fullName)}
	}

	var destDefs []project.PipelineDefinition
	if existing && projectID != "" {
		var err error
		destDefs, err = s.Projects.ListPipelineDefinitions(ctx, projectID)
		if err != nil {
			report.add("project-pipeline-def", projectName, "error",
				fmt.Sprintf("list destination pipeline definitions: %v", err))
			return
		}
	}

	if synth {
		// A destination project that already carries its own definitions (and not
		// ours) is left alone — never stack a synthesized one on top.
		if len(destDefs) > 0 && findDestDef(destDefs, synthDefName) == nil {
			report.add("project-pipeline-def", projectName, "exists",
				fmt.Sprintf("destination already has %d pipeline definition(s); not synthesizing %q", len(destDefs), synthDefName))
			return
		}
		// Emit data-loss / behavior-change warnings before creation so they
		// surface even when the repo resolves and the definition is created.
		if findDestDef(destDefs, synthDefName) == nil {
			s.warnOAuthOnlyFlags(ctx, report, projectName, p.Settings)
		}
	}

	for _, def := range defs {
		s.syncAppPipelineDefinition(ctx, report, projectName, projectID, def, destSlug, mapping, opts,
			synth, findDestDef(destDefs, def.Name))
	}
}

// findDestDef returns the destination definition with the given name, or nil.
func findDestDef(defs []project.PipelineDefinition, name string) *project.PipelineDefinition {
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

// extIDResolver resolves (and memoizes per repo) the external id for a source.
type extIDResolver func(target, fullName, capturedID string) (string, bool)

// newExtIDResolver wraps resolveExternalID so each distinct repo is resolved —
// and reported — once per definition even though the config source, checkout
// source and every trigger usually point at the same repository.
func (s *Syncer) newExtIDResolver(ctx context.Context, report *Report, mapping *manifest.Mapping, opts Options) extIDResolver {
	type result struct {
		id string
		ok bool
	}
	cache := map[string]result{}
	return func(target, fullName, capturedID string) (string, bool) {
		key := fullName + "|" + capturedID
		if r, hit := cache[key]; hit {
			return r.id, r.ok
		}
		id, ok := s.resolveExternalID(ctx, report, target, fullName, capturedID, mapping, opts)
		cache[key] = result{id, ok}
		return id, ok
	}
}

// translateSourceProvider maps a captured config/checkout/trigger-repo provider
// to the provider created on a GitHub App destination: github_oauth (and an
// empty provider) become github_app; github_app and github_server are kept.
func translateSourceProvider(provider string) (string, bool) {
	switch provider {
	case "", "github_oauth", "github_app":
		return "github_app", true
	case "github_server":
		return "github_server", true
	}
	return "", false
}

// syncAppPipelineDefinition creates one pipeline definition plus its triggers,
// or — when dest is non-nil (the definition already exists in the destination) —
// reconciles just its triggers.
func (s *Syncer) syncAppPipelineDefinition(
	ctx context.Context,
	report *Report,
	projectName, projectID string,
	def manifest.PipelineDefinition,
	destSlug string,
	mapping *manifest.Mapping,
	opts Options,
	synth bool,
	dest *project.PipelineDefinition,
) {
	defTarget := projectName + "/def:" + def.Name
	resolve := s.newExtIDResolver(ctx, report, mapping, opts)

	if dest != nil {
		report.add("project-pipeline-def", defTarget, "exists", "pipeline definition already exists")
		var destTriggers []project.Trigger
		if len(def.Triggers) > 0 && projectID != "" {
			var err error
			destTriggers, err = s.Projects.ListTriggers(ctx, projectID, dest.ID)
			if err != nil {
				report.add("project-trigger", defTarget, "error", fmt.Sprintf("list destination triggers: %v", err))
				return
			}
		}
		s.syncAppTriggers(ctx, report, projectName, projectID, def, dest.ID, destTriggers, resolve, opts, synth)
		return
	}

	// Translate providers for the destination. The config source may also be the
	// repo-less "circleci" provider (config stored in CircleCI).
	configProvider := def.ConfigSource.Provider
	if configProvider != "circleci" {
		var ok bool
		if configProvider, ok = translateSourceProvider(def.ConfigSource.Provider); !ok {
			report.add("project-pipeline-def", defTarget, "manual",
				fmt.Sprintf("config source provider %q cannot be recreated on a GitHub App destination — create the pipeline definition manually", def.ConfigSource.Provider))
			return
		}
	}
	checkoutProvider, ok := translateSourceProvider(def.CheckoutSource.Provider)
	if !ok {
		report.add("project-pipeline-def", defTarget, "manual",
			fmt.Sprintf("checkout source provider %q cannot be recreated on a GitHub App destination — create the pipeline definition manually", def.CheckoutSource.Provider))
		return
	}

	// Resolve the external_id for config and checkout sources.
	var configExtID string
	if configProvider != "circleci" {
		if configExtID, ok = resolve(defTarget+"/config",
			def.ConfigSource.RepoFullName, def.ConfigSource.RepoExternalID); !ok {
			return
		}
	}
	checkoutExtID, ok := resolve(defTarget+"/checkout",
		def.CheckoutSource.RepoFullName, def.CheckoutSource.RepoExternalID)
	if !ok {
		return
	}

	// Preserve a custom config file_path; only default when none was captured.
	filePath := def.ConfigSource.FilePath
	if filePath == "" {
		filePath = synthConfigPath
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
		if synth {
			report.add("project-pipeline-def", defTarget, "created",
				fmt.Sprintf("would synthesize App pipeline definition %q (file %s, repo ext-id %s) for OAuth source",
					def.Name, filePath, configExtID))
		} else {
			report.add("project-pipeline-def", defTarget, "created",
				fmt.Sprintf("would create pipeline definition (config ext-id %s, checkout ext-id %s)", configExtID, checkoutExtID))
		}
		// Still preview trigger actions.
		s.syncAppTriggers(ctx, report, projectName, projectID, def, "", nil, resolve, opts, synth)
		return
	}

	defID, err := s.Projects.CreatePipelineDefinition(ctx, projectID, spec)
	if err != nil {
		// "Installation does not have access to repository" (and similar
		// access/installation errors) mean the dest GitHub App installation
		// cannot see the repo — this is an operator action, not a code bug.
		// Record as "manual" with a clear remediation note so the run continues;
		// re-running sync after connecting the repo repairs the project.
		if isRepoAccessError(err) {
			report.add("project-pipeline-def", defTarget, "manual",
				fmt.Sprintf("create pipeline definition: %v — "+
					"the destination GitHub org must have the repository connected to the CircleCI GitHub App; "+
					"verify the App installation has access to the repo, then re-run sync to create it "+
					"(use --github-token to resolve a moved repo's id)", err))
		} else {
			report.add("project-pipeline-def", defTarget, "error",
				fmt.Sprintf("create pipeline definition: %v", err))
		}
		return
	}
	if synth {
		report.add("project-pipeline-def", defTarget, "created",
			fmt.Sprintf("synthesized App pipeline definition %q (file %s) for OAuth source", def.Name, filePath))
	} else {
		report.add("project-pipeline-def", defTarget, "created", "created pipeline definition")
	}

	s.syncAppTriggers(ctx, report, projectName, projectID, def, defID, nil, resolve, opts, synth)
}

// syncAppTriggers reconciles every captured trigger of a definition against the
// destination's existing triggers (destTriggers; empty for a new definition).
func (s *Syncer) syncAppTriggers(
	ctx context.Context,
	report *Report,
	projectName, projectID string,
	def manifest.PipelineDefinition,
	defID string,
	destTriggers []project.Trigger,
	resolve extIDResolver,
	opts Options,
	synth bool,
) {
	used := make([]bool, len(destTriggers))
	for _, trig := range def.Triggers {
		target := projectName + "/def:" + def.Name + "/trigger:" + trig.Name

		provider, ok := translateTriggerProvider(trig.EventSource.Provider)
		if !ok {
			report.add("project-trigger", target, "manual",
				fmt.Sprintf("trigger %q has unsupported provider %q — recreate manually", trig.Name, trig.EventSource.Provider))
			continue
		}

		if i := matchDestTrigger(trig, provider, destTriggers, used); i >= 0 {
			used[i] = true
			s.reconcileExistingTrigger(report, projectID, target, trig, destTriggers[i], opts)
			continue
		}

		spec, ok := buildTriggerSpec(report, target, trig, provider, resolve)
		if !ok {
			continue
		}

		// Dry-run or no real definition yet (preflight resolve pass): record intent only.
		if !opts.Apply || defID == "" {
			report.add("project-trigger", target, "created", wouldCreateTriggerDetail(synth, trig, spec))
			continue
		}

		trigID, err := s.Projects.CreateTrigger(ctx, projectID, defID, spec)
		if err != nil {
			report.add("project-trigger", target, "error", fmt.Sprintf("create trigger: %v", err))
			continue
		}
		report.add("project-trigger", target, "created", createdTriggerDetail(synth, trig, spec))
		if provider == "webhook" {
			report.add("project-trigger", target+"/webhook-secret", "manual",
				fmt.Sprintf("webhook trigger %q was recreated with a NEW URL and secret — fetch it from the destination "+
					"project's trigger and redistribute it to the sender%s", trig.Name, senderSuffix(trig)))
		}

		// Only triggers that were enabled in the source are enabled at the
		// enable-builds step; a trigger disabled in the source stays disabled.
		if !trig.Disabled {
			report.PendingEnable = append(report.PendingEnable, EnableTarget{
				Kind:      "trigger",
				ProjectID: projectID,
				TriggerID: trigID,
			})
		}
	}
}

// reconcileExistingTrigger records an already-present trigger and, when the
// source trigger was enabled but the destination's is still disabled (created
// disabled by an earlier run that never reached the enable step), queues it for
// the enable-builds step.
func (s *Syncer) reconcileExistingTrigger(report *Report, projectID, target string, trig manifest.Trigger, dest project.Trigger, opts Options) {
	if !dest.Disabled || trig.Disabled {
		report.add("project-trigger", target, "exists", "trigger already exists")
		return
	}
	if !opts.Apply {
		report.add("project-trigger", target, "exists",
			"trigger exists but is disabled — would be queued for enable-builds on --apply")
		return
	}
	report.add("project-trigger", target, "exists", "trigger exists but is disabled — queuing for enable-builds")
	report.PendingEnable = append(report.PendingEnable, EnableTarget{
		Kind:      "trigger",
		ProjectID: projectID,
		TriggerID: dest.ID,
	})
}

// translateTriggerProvider maps a captured trigger event-source provider to the
// provider created on a GitHub App destination.
func translateTriggerProvider(provider string) (string, bool) {
	switch provider {
	case "github_oauth", "github_app":
		return "github_app", true
	case "github_server", "webhook", "schedule":
		return provider, true
	}
	return "", false
}

// matchDestTrigger finds the unused destination trigger corresponding to src.
// A trigger's name is assigned by CircleCI (the create API takes none), so it is
// tried first only among triggers of the same provider; otherwise the trigger's
// defining fields are compared (preset/refs for repo triggers, event name +
// sender for webhooks, event name + cron for schedules). Returns -1 when none.
func matchDestTrigger(src manifest.Trigger, provider string, dest []project.Trigger, used []bool) int {
	if src.Name != "" {
		for i, d := range dest {
			if !used[i] && d.EventSource.Provider == provider && d.Name == src.Name {
				return i
			}
		}
	}
	for i, d := range dest {
		if used[i] || d.EventSource.Provider != provider {
			continue
		}
		switch provider {
		case "webhook":
			if d.EventName == src.EventName && d.EventSource.Webhook.Sender == src.EventSource.WebhookSender {
				return i
			}
		case "schedule":
			if d.EventName == src.EventName && d.EventSource.Schedule.CronExpression == src.EventSource.ScheduleCron {
				return i
			}
		default:
			if d.EventPreset == src.EventPreset && d.CheckoutRef == src.CheckoutRef && d.ConfigRef == src.ConfigRef {
				return i
			}
		}
	}
	return -1
}

// buildTriggerSpec builds the create request for a captured trigger, carrying
// every field the v2 createTrigger API accepts. The trigger is always created
// disabled; the enable-builds step turns it on. It records a "manual" action and
// returns ok=false when a required field was not captured or the repo id cannot
// be resolved.
func buildTriggerSpec(report *Report, target string, trig manifest.Trigger, provider string, resolve extIDResolver) (project.TriggerSpec, bool) {
	switch provider {
	case "webhook", "schedule":
		var missing []string
		if trig.EventName == "" {
			missing = append(missing, "event_name")
		}
		if trig.CheckoutRef == "" {
			missing = append(missing, "checkout_ref")
		}
		if trig.ConfigRef == "" {
			missing = append(missing, "config_ref")
		}
		if provider == "schedule" && trig.EventSource.ScheduleCron == "" {
			missing = append(missing, "cron_expression")
		}
		if len(missing) > 0 {
			report.add("project-trigger", target, "manual",
				fmt.Sprintf("%s trigger %q is missing required field(s) %v in the manifest — recreate manually", provider, trig.Name, missing))
			return project.TriggerSpec{}, false
		}
		spec := project.TriggerSpec{
			Provider:    provider,
			EventName:   trig.EventName,
			CheckoutRef: trig.CheckoutRef,
			ConfigRef:   trig.ConfigRef,
			Disabled:    true,
		}
		if provider == "webhook" {
			spec.WebhookSender = trig.EventSource.WebhookSender
		} else {
			spec.CronExpression = trig.EventSource.ScheduleCron
			spec.AttributionActor = scheduleActor(trig.EventSource.ScheduleActor)
			spec.Parameters = trig.Parameters
		}
		return spec, true
	}

	extID, ok := resolve(target, trig.EventSource.RepoFullName, trig.EventSource.RepoExternalID)
	if !ok {
		return project.TriggerSpec{}, false
	}
	return project.TriggerSpec{
		Provider:    provider,
		ExternalID:  extID,
		EventPreset: trig.EventPreset,
		CheckoutRef: trig.CheckoutRef,
		ConfigRef:   trig.ConfigRef,
		Disabled:    true,
	}, true
}

// scheduleActor returns the attribution_actor to create a schedule trigger with.
// The API accepts only "current" or "system"; a captured user id (the response
// form) cannot be replayed, so it falls back to "system" — the same attribution
// the classic schedule sync uses.
func scheduleActor(captured string) string {
	if captured == "current" || captured == "system" {
		return captured
	}
	return "system"
}

func senderSuffix(trig manifest.Trigger) string {
	if trig.EventSource.WebhookSender == "" {
		return ""
	}
	return fmt.Sprintf(" (%s)", trig.EventSource.WebhookSender)
}

func wouldCreateTriggerDetail(synth bool, trig manifest.Trigger, spec project.TriggerSpec) string {
	if synth {
		return fmt.Sprintf("would synthesize trigger (event_preset %q, disabled — not yet enabled)", spec.EventPreset)
	}
	return "would create trigger (" + triggerStateNote(trig) + ")"
}

func createdTriggerDetail(synth bool, trig manifest.Trigger, spec project.TriggerSpec) string {
	if synth {
		return fmt.Sprintf("synthesized trigger (event_preset %q, disabled — not yet enabled)", spec.EventPreset)
	}
	return "created trigger (" + triggerStateNote(trig) + ")"
}

func triggerStateNote(trig manifest.Trigger) string {
	if trig.Disabled {
		return "disabled — as in source"
	}
	return "disabled — not yet enabled"
}
