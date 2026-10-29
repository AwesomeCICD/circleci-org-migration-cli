package syncer

import (
	"context"
	"fmt"
	"reflect"
	"strings"

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
	if opts.SkipPipelines {
		reportSkippedPipelines(report, p, projectName)
		return
	}
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

// skipPipelinesDetail is the detail recorded for every definition and trigger
// left uncreated by --skip-pipelines.
const skipPipelinesDetail = "skipped: --skip-pipelines (re-run sync without it to create it)"

// reportSkippedPipelines records, per pipeline definition and per trigger that a
// normal run would reconcile, a "skipped" action. A project with no captured
// definitions (a plain OAuth project) would get one synthesized definition and
// trigger, so those are reported too.
func reportSkippedPipelines(report *Report, p manifest.Project, projectName string) {
	defs := p.PipelineDefinitions
	if len(defs) == 0 {
		fullName, ok := oauthSourceFullName(p.Slug)
		if !ok {
			return // nothing a later run could synthesize
		}
		defs = []manifest.PipelineDefinition{synthesizedDefinition(p, fullName)}
	}
	for _, def := range defs {
		defTarget := projectName + "/def:" + def.Name
		report.add("project-pipeline-def", defTarget, "skipped", skipPipelinesDetail)
		for _, trig := range def.Triggers {
			report.add("project-trigger", defTarget+"/trigger:"+trig.Name, "skipped", skipPipelinesDetail)
		}
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
		// The definition is matched by name only, so verify it really is the
		// definition the manifest describes before treating it as "exists" and
		// reconciling its triggers.
		if spec, ok := s.buildDefinitionSpec(ctx, report, defTarget, def, resolve, mapping, opts, true); ok {
			if diffs := diffDestDefinition(*dest, spec); len(diffs) > 0 {
				report.add("project-pipeline-def", defTarget, "manual", fmt.Sprintf(
					"pipeline definition %q already exists in the destination but differs from the manifest (%s); it was NOT modified "+
						"and its triggers were NOT reconciled — align it manually (or delete it) and re-run sync",
					def.Name, strings.Join(diffs, "; ")))
				return
			}
		}
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

	spec, ok := s.buildDefinitionSpec(ctx, report, defTarget, def, resolve, mapping, opts, false)
	if !ok {
		return
	}
	configExtID, checkoutExtID, filePath := spec.ConfigExternalID, spec.CheckoutExternalID, spec.ConfigFilePath

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

// buildDefinitionSpec translates a captured definition into the create request:
// providers are translated for the GitHub App destination and the config and
// checkout repo external ids are resolved.
//
// soft is the compare-only mode used for a definition that already exists in the
// destination: nothing is recorded for an untranslatable provider (the caller
// just skips the comparison), and an external id that cannot be obtained without
// a GitHub token is left empty (unknown) instead of recording a "manual" action —
// a re-run for existing projects must not demand a token it does not need. In the
// strict (create) mode a failure records the action and returns ok=false.
func (s *Syncer) buildDefinitionSpec(
	ctx context.Context,
	report *Report,
	defTarget string,
	def manifest.PipelineDefinition,
	resolve extIDResolver,
	mapping *manifest.Mapping,
	opts Options,
	soft bool,
) (project.PipelineDefinitionSpec, bool) {
	// Translate providers for the destination. The config source may also be the
	// repo-less "circleci" provider (config stored in CircleCI).
	configProvider := def.ConfigSource.Provider
	if configProvider != "circleci" {
		var ok bool
		if configProvider, ok = translateSourceProvider(def.ConfigSource.Provider); !ok {
			if !soft {
				report.add("project-pipeline-def", defTarget, "manual",
					fmt.Sprintf("config source provider %q cannot be recreated on a GitHub App destination — create the pipeline definition manually", def.ConfigSource.Provider))
			}
			return project.PipelineDefinitionSpec{}, false
		}
	}
	checkoutProvider, ok := translateSourceProvider(def.CheckoutSource.Provider)
	if !ok {
		if !soft {
			report.add("project-pipeline-def", defTarget, "manual",
				fmt.Sprintf("checkout source provider %q cannot be recreated on a GitHub App destination — create the pipeline definition manually", def.CheckoutSource.Provider))
		}
		return project.PipelineDefinitionSpec{}, false
	}

	// Resolve the external_id for config and checkout sources.
	resolveID := func(sub string, src manifest.PipelineSource) (string, bool) {
		if !soft {
			return resolve(defTarget+sub, src.RepoFullName, src.RepoExternalID)
		}
		// Compare-only: reuse a captured id when the GitHub org is unchanged;
		// otherwise ask GitHub only when a token is available.
		if src.RepoExternalID != "" && s.mapRepoFullName(ctx, src.RepoFullName, mapping, opts) == src.RepoFullName {
			return src.RepoExternalID, true
		}
		if opts.GitHubToken == "" {
			return "", true // unknown: skip the comparison
		}
		id, ok := resolve(defTarget+sub, src.RepoFullName, src.RepoExternalID)
		if !ok {
			return "", true
		}
		return id, true
	}

	var configExtID string
	if configProvider != "circleci" {
		if configExtID, ok = resolveID("/config", def.ConfigSource); !ok {
			return project.PipelineDefinitionSpec{}, false
		}
	}
	checkoutExtID, ok := resolveID("/checkout", def.CheckoutSource)
	if !ok {
		return project.PipelineDefinitionSpec{}, false
	}

	// Preserve a custom config file_path; only default when none was captured.
	filePath := def.ConfigSource.FilePath
	if filePath == "" {
		filePath = synthConfigPath
	}

	return project.PipelineDefinitionSpec{
		Name:               def.Name,
		Description:        def.Description,
		ConfigProvider:     configProvider,
		ConfigExternalID:   configExtID,
		ConfigFilePath:     filePath,
		CheckoutProvider:   checkoutProvider,
		CheckoutExternalID: checkoutExtID,
	}, true
}

// diffDestDefinition lists how an existing destination definition differs from
// the definition the manifest would create (want): provider, repo external id
// and config file_path of the config and checkout sources. An external id that
// is unknown on either side is not compared. Empty result: they match.
func diffDestDefinition(dest project.PipelineDefinition, want project.PipelineDefinitionSpec) []string {
	var diffs []string
	add := func(what, got, exp string) {
		diffs = append(diffs, fmt.Sprintf("%s: destination %q, expected %q", what, got, exp))
	}
	if dest.ConfigSource.Provider != want.ConfigProvider {
		add("config source provider", dest.ConfigSource.Provider, want.ConfigProvider)
	}
	if want.ConfigProvider != "circleci" {
		if want.ConfigExternalID != "" && dest.ConfigSource.Repo.ExternalID != "" &&
			dest.ConfigSource.Repo.ExternalID != want.ConfigExternalID {
			add("config source repo external_id", dest.ConfigSource.Repo.ExternalID, want.ConfigExternalID)
		}
		if dest.ConfigSource.FilePath != want.ConfigFilePath {
			add("config file_path", dest.ConfigSource.FilePath, want.ConfigFilePath)
		}
	}
	if dest.CheckoutSource.Provider != want.CheckoutProvider {
		add("checkout source provider", dest.CheckoutSource.Provider, want.CheckoutProvider)
	}
	if want.CheckoutExternalID != "" && dest.CheckoutSource.Repo.ExternalID != "" &&
		dest.CheckoutSource.Repo.ExternalID != want.CheckoutExternalID {
		add("checkout source repo external_id", dest.CheckoutSource.Repo.ExternalID, want.CheckoutExternalID)
	}
	return diffs
}

// syncAppTriggers reconciles every captured trigger of a definition against the
// destination's existing triggers (destTriggers; empty for a new definition).
//
// By default only triggers CREATED in this run (and enabled in the source) are
// queued for the enable-builds step. An existing destination trigger is queued
// only with Options.EnableExistingTriggers, so a re-run never re-enables a
// trigger an operator deliberately disabled.
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
	providers := make([]string, len(def.Triggers))
	for i, trig := range def.Triggers {
		providers[i], _ = translateTriggerProvider(trig.EventSource.Provider)
	}
	matches := matchDestTriggers(def.Triggers, providers, destTriggers)

	for i, trig := range def.Triggers {
		target := projectName + "/def:" + def.Name + "/trigger:" + trig.Name

		provider := providers[i]
		if provider == "" {
			report.add("project-trigger", target, "manual",
				fmt.Sprintf("trigger %q has unsupported provider %q — recreate manually", trig.Name, trig.EventSource.Provider))
			continue
		}

		if m := matches[i]; m >= 0 {
			s.reconcileExistingTrigger(report, projectID, projectName, def.Name, target, trig, destTriggers[m], opts)
			continue
		}

		spec, ok := buildTriggerSpec(report, target, trig, provider, resolve)
		if !ok {
			continue
		}

		// Dry-run or no real definition yet (preflight resolve pass): record intent only.
		if !opts.Apply || defID == "" {
			report.add("project-trigger", target, "created", wouldCreateTriggerDetail(synth, trig, spec))
			noteScheduleParams(report, target, trig)
			continue
		}

		var trigID, webhookURL, webhookSender string
		var err error
		if dc, ok := s.Projects.(triggerDetailCreator); ok && provider == "webhook" {
			var ct *project.CreatedTrigger
			if ct, err = dc.CreateTriggerDetailed(ctx, projectID, defID, spec); err == nil {
				trigID, webhookURL, webhookSender = ct.ID, ct.WebhookURL, ct.WebhookSender
			}
		} else {
			trigID, err = s.Projects.CreateTrigger(ctx, projectID, defID, spec)
		}
		if err != nil {
			report.add("project-trigger", target, "error", fmt.Sprintf("create trigger: %v", err))
			continue
		}
		report.add("project-trigger", target, "created", createdTriggerDetail(synth, trig, spec))
		if provider == "webhook" {
			s.noteInboundWebhook(report, target, projectName, def.Name, trig, trigID, webhookURL, webhookSender, opts)
		}
		unverified := noteScheduleParams(report, target, trig)

		// Only triggers that were enabled in the source are enabled at the
		// enable-builds step; a trigger disabled in the source stays disabled. A
		// schedule trigger whose parameters were not captured is never
		// auto-enabled: it could run with the wrong pipeline parameters.
		if !trig.Disabled && !unverified {
			report.PendingEnable = append(report.PendingEnable, EnableTarget{
				Kind:        "trigger",
				ProjectID:   projectID,
				ProjectName: projectName,
				DefName:     def.Name,
				TriggerID:   trigID,
			})
		}
	}
}

// noteInboundWebhook handles a recreated inbound custom-webhook trigger. CircleCI
// generates a NEW URL (it embeds a secret) and returns it unmasked only in the
// create response; every later read redacts the secret. With
// Options.WebhookSecretsOut the URL is recorded for the secrets file (never
// printed); otherwise the manual line says how to get a usable URL.
func (s *Syncer) noteInboundWebhook(report *Report, target, projectName, defName string, trig manifest.Trigger, trigID, webhookURL, sender string, opts Options) {
	who := fmt.Sprintf("webhook trigger %q%s in %s (definition %q)", trig.Name, senderSuffix(trig), projectName, defName)
	if sender == "" {
		sender = trig.EventSource.WebhookSender
	}
	switch {
	case opts.WebhookSecretsOut != "" && webhookURL != "":
		s.webhookSecrets.InboundTriggers = append(s.webhookSecrets.InboundTriggers, InboundWebhookTrigger{
			Project: projectName, Definition: defName, Trigger: trigID, EventName: trig.EventName, Sender: sender, URL: webhookURL,
		})
		report.add("project-trigger", target+"/webhook-secret", "manual", fmt.Sprintf(
			"%s was recreated with a NEW URL and secret, saved to %s (never printed): give that URL to the sender so it posts to the destination", who, opts.WebhookSecretsOut))
	case opts.WebhookSecretsOut != "":
		report.add("project-trigger", target+"/webhook-secret", "manual", fmt.Sprintf(
			"%s was recreated, but CircleCI returned no URL for it, so nothing could be saved to %s — delete and recreate the trigger in the destination project to obtain its URL, then give it to the sender", who, opts.WebhookSecretsOut))
	default:
		report.add("project-trigger", target+"/webhook-secret", "manual", fmt.Sprintf(
			"%s was recreated with a NEW URL and secret. CircleCI shows the secret URL only in the create response (later reads redact it as secret=**REDACTED**), "+
				"so it was not kept: delete the destination trigger and recreate it (or re-run sync on a fresh project with --webhook-secrets-out <file> to save the URL), then give the new URL to the sender", who))
	}
}

// noteScheduleParams records a "manual" note for a schedule trigger whose
// pipeline parameters are absent from the manifest (an older export, or a
// schedule that really has none — the manifest cannot tell them apart) and
// reports whether it did. Such a trigger is created, but never auto-enabled.
func noteScheduleParams(report *Report, target string, trig manifest.Trigger) bool {
	if trig.EventSource.Provider != "schedule" || len(trig.Parameters) > 0 {
		return false
	}
	report.add("project-trigger", target+"/parameters", "manual", fmt.Sprintf(
		"schedule trigger %q has no pipeline parameters in the manifest (an older export may not have captured them) — "+
			"verify the destination trigger's parameters against the source schedule, then enable it by hand; "+
			"it is not enabled automatically", trig.Name))
	return true
}

// reconcileExistingTrigger records an already-present trigger. When the source
// trigger was enabled but the destination's is disabled, it is queued for the
// enable-builds step ONLY when Options.EnableExistingTriggers is set — otherwise
// the destination's state is left alone (an operator may have disabled it on
// purpose) and the report says how to enable it.
func (s *Syncer) reconcileExistingTrigger(report *Report, projectID, projectName, defName, target string, trig manifest.Trigger, dest project.Trigger, opts Options) {
	if !dest.Disabled || trig.Disabled {
		report.add("project-trigger", target, "exists", "trigger already exists")
		return
	}
	which := fmt.Sprintf("trigger %s (definition %q)", dest.ID, defName)
	if !opts.EnableExistingTriggers {
		report.add("project-trigger", target, "exists",
			which+" exists but is disabled in the destination — left disabled; pass --enable-existing-triggers to enable it")
		return
	}
	if noteScheduleParams(report, target, trig) {
		report.add("project-trigger", target, "exists",
			which+" exists but is disabled — not queued for enable (schedule parameters unverified)")
		return
	}
	if !opts.Apply {
		report.add("project-trigger", target, "exists",
			which+" exists but is disabled — would be queued for enable-builds on --apply (--enable-existing-triggers)")
		return
	}
	report.add("project-trigger", target, "exists",
		which+" exists but is disabled — queuing for enable-builds (--enable-existing-triggers)")
	report.PendingEnable = append(report.PendingEnable, EnableTarget{
		Kind:        "trigger",
		ProjectID:   projectID,
		ProjectName: projectName,
		DefName:     defName,
		TriggerID:   dest.ID,
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

// matchDestTriggers pairs each captured trigger with the destination trigger it
// corresponds to (an index into dest, or -1 when none). A trigger's name is
// assigned by CircleCI (the create API takes none), so matching is by content,
// in two passes so a re-run never swaps look-alike triggers or duplicates one:
//
//  1. EXACT: provider, preset, event name, checkout_ref, config_ref, cron,
//     schedule parameters, webhook sender and disabled state all agree;
//  2. LOOSE, for the leftovers only: same name (and the same provider and
//     preset), else the same defining fields ignoring disabled state and
//     parameters (an operator may have toggled it).
//
// An empty source checkout_ref / config_ref / parameters is a wildcard: the API
// may echo defaults the manifest did not capture. providers[i] is the translated
// provider of src[i], or "" when unsupported (never matched).
func matchDestTriggers(src []manifest.Trigger, providers []string, dest []project.Trigger) []int {
	match := make([]int, len(src))
	for i := range match {
		match[i] = -1
	}
	used := make([]bool, len(dest))

	pass := func(equal func(s manifest.Trigger, provider string, d project.Trigger) bool) {
		for i, s := range src {
			if match[i] >= 0 || providers[i] == "" {
				continue
			}
			for j, d := range dest {
				if used[j] || d.EventSource.Provider != providers[i] || !equal(s, providers[i], d) {
					continue
				}
				match[i], used[j] = j, true
				break
			}
		}
	}

	pass(func(s manifest.Trigger, provider string, d project.Trigger) bool {
		return triggerFieldsMatch(s, provider, d) && s.Disabled == d.Disabled &&
			(len(s.Parameters) == 0 || parametersEqual(s.Parameters, d.Parameters))
	})
	pass(func(s manifest.Trigger, _ string, d project.Trigger) bool {
		return s.Name != "" && d.Name == s.Name && d.EventPreset == s.EventPreset
	})
	pass(triggerFieldsMatch)
	return match
}

// triggerFieldsMatch compares a captured trigger's defining fields with a
// destination trigger of the same provider: preset and refs for repo triggers,
// event name, sender and refs for webhooks, event name, cron and refs for
// schedules. Empty source refs are wildcards.
func triggerFieldsMatch(s manifest.Trigger, provider string, d project.Trigger) bool {
	if !refMatches(s.CheckoutRef, d.CheckoutRef) || !refMatches(s.ConfigRef, d.ConfigRef) {
		return false
	}
	switch provider {
	case "webhook":
		return d.EventName == s.EventName && d.EventSource.Webhook.Sender == s.EventSource.WebhookSender
	case "schedule":
		return d.EventName == s.EventName && d.EventSource.Schedule.CronExpression == s.EventSource.ScheduleCron
	default:
		return d.EventPreset == s.EventPreset
	}
}

// refMatches reports whether a destination ref satisfies a source ref; an empty
// source ref matches anything (the server may fill in a default).
func refMatches(src, dest string) bool {
	return src == "" || src == dest
}

// parametersEqual compares pipeline-parameter maps, treating nil and empty alike.
func parametersEqual(a, b map[string]any) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	return reflect.DeepEqual(a, b)
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
