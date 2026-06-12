// Package terraform generates Terraform HCL and tfvars files from a circleci-migrate
// manifest. It is a pure transformation — no network calls, no API clients.
//
// Target provider: CircleCI-Public/circleci (Terraform Registry), v0.3.x.
//
// Design:
//   - Uses text/template (stdlib, no extra dependencies) to emit HCL.
//   - Reuses internal/manifest.Mapping for slug/ID remapping so there is no
//     duplicate remap logic.
//   - All exported types and functions are pure (manifest in → files out).
//
// M2 resources added (org-type gating verified against provider CircleCI-Public/circleci v0.3.x):
//
//	circleci_context_restriction   — project + expression: both org types;
//	                                  group: OAuth ONLY (provider rejects on standalone)
//	circleci_pipeline              — standalone/App ONLY (provider schema rejects github_oauth)
//	circleci_trigger               — standalone/App ONLY (tied to pipeline_id)
//	circleci_webhook               — both org types
//	circleci_runner_resource_class — both org types
//	circleci_runner_token          — both org types
//
// NOTE: circleci_organization is NOT generated — the destination org pre-exists.
package terraform

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// OrgType identifies the CircleCI destination org authentication model.
// It controls which provider attributes are emitted in the generated HCL.
type OrgType int

const (
	// OrgTypeUnknown means no type was specified; it is inferred from the manifest.
	OrgTypeUnknown OrgType = iota
	// OrgTypeOAuth represents a GitHub-OAuth org (slug prefix "gh/").
	// The circleci_project advanced-settings attributes are NOT available for
	// these orgs; GetSettings/UpdateSettings is standalone-only per the provider.
	// circleci_pipeline and circleci_trigger are also NOT available for OAuth orgs.
	OrgTypeOAuth
	// OrgTypeStandalone represents a GitHub App / GitLab / standalone org
	// (slug prefix "circleci/"). All circleci_project attributes are available,
	// and circleci_pipeline / circleci_trigger are supported.
	OrgTypeStandalone
)

// ParseOrgType parses a human-supplied org-type string into an OrgType.
// Accepted values (case-insensitive):
//
//	oauth, gh, github       → OrgTypeOAuth
//	standalone, app, github_app → OrgTypeStandalone
//
// Returns an error for unrecognised values.
func ParseOrgType(s string) (OrgType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "oauth", "gh", "github":
		return OrgTypeOAuth, nil
	case "standalone", "app", "github_app":
		return OrgTypeStandalone, nil
	default:
		return OrgTypeUnknown, fmt.Errorf("unrecognised --dest-org-type %q: accepted values are oauth|gh|github or standalone|app|github_app", s)
	}
}

// InferOrgType determines the OrgType from a manifest's source org slug.
// A slug starting with "gh/" implies OrgTypeOAuth; "circleci/" implies
// OrgTypeStandalone. Returns OrgTypeUnknown if the prefix is not recognised.
func InferOrgType(m *manifest.Manifest) OrgType {
	slug := m.Source.Org.Slug
	switch {
	case strings.HasPrefix(slug, "gh/") || strings.HasPrefix(slug, "bb/"):
		return OrgTypeOAuth
	case strings.HasPrefix(slug, "circleci/"):
		return OrgTypeStandalone
	default:
		return OrgTypeUnknown
	}
}

// ExistingIDs holds destination resource IDs from a previous sync --json run.
// It is the input to the --import-existing feature: when provided, the
// generator emits Terraform 1.5+ import {} blocks for resources that already
// exist in the destination org so they can be adopted into state without
// re-creating them.
//
// All fields are optional maps; missing keys cause the import block to be
// omitted for that resource (it will be created fresh by terraform apply).
//
// Design: the operator runs `sync --json > sync-result.json` and passes that
// file via `terraform generate --existing sync-result.json`. The generator
// reads the resource_ids section — a sub-object added to the sync JSON output
// (see cmd/sync.go) that records the destination IDs of already-existing
// resources.
type ExistingIDs struct {
	// Contexts maps context name → destination context UUID.
	Contexts map[string]string `json:"contexts,omitempty"`
	// ContextEnvVars maps "contextName/varName" → destination env-var ID.
	// CircleCI does not return a stable ID for context env-vars; this is
	// typically left empty and the resource is adopted by name+context_id.
	ContextEnvVars map[string]string `json:"context_env_vars,omitempty"`
	// Projects maps repo name → destination project UUID.
	Projects map[string]string `json:"projects,omitempty"`
	// Webhooks maps "projectRepoName/webhookName" → destination webhook ID.
	Webhooks map[string]string `json:"webhooks,omitempty"`
	// RunnerResourceClasses maps "namespace/class-name" → resource class ID.
	RunnerResourceClasses map[string]string `json:"runner_resource_classes,omitempty"`
}

// SyncJSONWithIDs is a minimal parse target for `sync --json` output that
// includes the optional resource_ids extension added by cmd/sync.go for the
// --existing import path. Only the resource_ids field is used here.
type SyncJSONWithIDs struct {
	ResourceIDs *ExistingIDs `json:"resource_ids,omitempty"`
}

// LoadExistingIDs reads a sync --json output file and extracts the resource_ids
// section.  Returns nil (not an error) when the file lacks a resource_ids
// section — callers treat nil as "no existing IDs, skip import blocks".
func LoadExistingIDs(path string) (*ExistingIDs, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("terraform generate: reading --existing file %s: %w", path, err)
	}
	var s SyncJSONWithIDs
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("terraform generate: parsing --existing file %s: %w", path, err)
	}
	return s.ResourceIDs, nil
}

// Options controls what the generator produces.
type Options struct {
	// DestOrgID is the destination organization UUID (required).
	DestOrgID string
	// Host is the CircleCI host URL. When empty defaults to "https://circleci.com".
	Host string
	// Mapping is the optional slug/ID remap table. When nil an identity mapping
	// is used (same-org rename).
	Mapping *manifest.Mapping
	// SecretsBundle is the optional plaintext secret bundle. When non-nil, a
	// secrets.auto.tfvars.json is emitted and a plaintext warning is printed.
	SecretsBundle *manifest.SecretBundle
	// Placeholders, when true, emits secrets.auto.tfvars.json with empty-string
	// values plus a fill-in workbook (SECRETS_WORKBOOK.md). Mutually exclusive
	// with SecretsBundle.
	Placeholders bool
	// OutDir is the directory where all generated files are written.
	OutDir string
	// DestOrgType controls whether advanced project settings are emitted.
	// When OrgTypeUnknown (the zero value) the generator infers the type from
	// the manifest's source org slug and prints which type it assumed.
	DestOrgType OrgType
	// DestRunnerNamespace is the destination runner namespace. When set,
	// circleci_runner_resource_class resources use this namespace; when empty
	// the source namespace from the manifest is used as-is.
	DestRunnerNamespace string
	// ExistingIDs, when non-nil, causes the generator to emit Terraform 1.5+
	// import {} blocks for resources that already exist in the destination.
	// Obtain this by running `sync --json` and passing the result file via
	// `terraform generate --existing <file>`.
	ExistingIDs *ExistingIDs
	// ImportExisting, when true, enables import block emission.
	// ExistingIDs must be non-nil for import blocks to actually appear.
	ImportExisting bool
}

// Generate writes all Terraform files for m into opts.OutDir.
// It creates the directory (and any missing parents) if it does not exist.
//
// When opts.DestOrgType is OrgTypeUnknown the function infers the type from
// the manifest's source org slug (gh/ → oauth; circleci/ → standalone) and
// prints a notice to stderr so the caller knows which type was assumed and how
// to override it. The notice is written to os.Stderr directly because Generate
// is a pure file-writing function without access to cobra's IO handles; the
// cmd layer may optionally pass the type explicitly to suppress inference.
func Generate(m *manifest.Manifest, opts Options) error {
	if opts.OutDir == "" {
		return fmt.Errorf("terraform generate: --out directory is required")
	}
	if opts.DestOrgID == "" {
		return fmt.Errorf("terraform generate: --dest-org-id is required")
	}

	// Resolve dest org type — infer if not supplied.
	destOrgType := opts.DestOrgType
	if destOrgType == OrgTypeUnknown {
		destOrgType = InferOrgType(m)
		switch destOrgType {
		case OrgTypeOAuth:
			fmt.Fprintf(os.Stderr,
				"Note: --dest-org-type not set; inferred \"oauth\" from source slug %q.\n"+
					"      Advanced project settings will be OMITTED (not supported for OAuth orgs).\n"+
					"      Pipeline/trigger resources will be OMITTED (provider rejects github_oauth).\n"+
					"      To override, pass --dest-org-type standalone.\n",
				m.Source.Org.Slug)
		case OrgTypeStandalone:
			fmt.Fprintf(os.Stderr,
				"Note: --dest-org-type not set; inferred \"standalone\" from source slug %q.\n"+
					"      Advanced project settings and pipeline/trigger resources will be included.\n"+
					"      To override, pass --dest-org-type oauth.\n",
				m.Source.Org.Slug)
		default:
			// Cannot infer — fall back to standalone (safest: includes all attrs).
			destOrgType = OrgTypeStandalone
			fmt.Fprintf(os.Stderr,
				"Note: --dest-org-type not set and could not be inferred from source slug %q.\n"+
					"      Defaulting to \"standalone\" (advanced settings included).\n"+
					"      Pass --dest-org-type oauth if your destination is a GitHub OAuth org.\n",
				m.Source.Org.Slug)
		}
	}

	if err := os.MkdirAll(opts.OutDir, 0o755); err != nil {
		return fmt.Errorf("terraform generate: creating output directory: %w", err)
	}

	host := opts.Host
	if host == "" {
		host = "https://circleci.com"
	}

	mp := opts.Mapping
	if mp == nil {
		mp = manifest.IdentityMapping(m.Source.Org.Slug)
	}

	// Build the intermediate model — resolved slugs, deduplicated names.
	model, err := buildModel(m, mp, opts.DestOrgID, destOrgType, opts.DestRunnerNamespace)
	if err != nil {
		return err
	}

	// Emit static HCL files.
	if err := writeFile(opts.OutDir, "versions.tf", versionsTemplate, nil); err != nil {
		return err
	}
	if err := writeFile(opts.OutDir, "providers.tf", providersTemplate, map[string]any{
		"Host":  host,
		"OrgID": opts.DestOrgID,
	}); err != nil {
		return err
	}
	if err := writeFile(opts.OutDir, "contexts.tf", contextsTemplate, nil); err != nil {
		return err
	}
	// projects.tf varies by dest org type: OAuth orgs cannot use advanced settings.
	projectsTmpl := projectsTemplate
	if destOrgType == OrgTypeOAuth {
		projectsTmpl = projectsOAuthTemplate
	}
	if err := writeFile(opts.OutDir, "projects.tf", projectsTmpl, nil); err != nil {
		return err
	}

	// restrictions.tf — context restrictions (M2).
	if err := writeFile(opts.OutDir, "restrictions.tf", restrictionsTemplate, nil); err != nil {
		return err
	}

	// webhooks.tf — project webhooks (both org types) (M2).
	if err := writeFile(opts.OutDir, "webhooks.tf", webhooksTemplate, nil); err != nil {
		return err
	}

	// runners.tf — self-hosted runner resource classes + tokens (both org types) (M2).
	if err := writeFile(opts.OutDir, "runners.tf", runnersTemplate, nil); err != nil {
		return err
	}

	// pipelines.tf — circleci_pipeline + circleci_trigger (standalone ONLY) (M2).
	// For OAuth destinations: omit the file entirely (provider schema rejects github_oauth).
	if destOrgType != OrgTypeOAuth {
		if err := writeFile(opts.OutDir, "pipelines.tf", pipelinesTemplate, nil); err != nil {
			return err
		}
	}

	// Emit migration.auto.tfvars.json (non-secret values).
	tfvars := buildTFVars(model, destOrgType)
	if err := writeJSON(opts.OutDir, "migration.auto.tfvars.json", tfvars); err != nil {
		return err
	}

	// Emit secrets.auto.tfvars.json when requested.
	if opts.SecretsBundle != nil {
		secretsVars := buildSecretsVars(model, opts.SecretsBundle)
		if err := writeJSON(opts.OutDir, "secrets.auto.tfvars.json", secretsVars); err != nil {
			return err
		}
	} else if opts.Placeholders {
		placeholderVars := buildPlaceholderVars(model)
		if err := writeJSON(opts.OutDir, "secrets.auto.tfvars.json", placeholderVars); err != nil {
			return err
		}
		if err := writeSecretsWorkbook(opts.OutDir, model); err != nil {
			return err
		}
	}

	// Emit imports.tf when --import-existing is set and IDs are provided.
	if opts.ImportExisting && opts.ExistingIDs != nil {
		if err := writeImportsFile(opts.OutDir, model, opts.ExistingIDs, destOrgType); err != nil {
			return err
		}
	}

	// Emit GAPS.md.
	if err := writeGapsFile(opts.OutDir, m, model, destOrgType); err != nil {
		return err
	}

	return nil
}

// ----------------------------------------------------------------------------
// Intermediate model
// ----------------------------------------------------------------------------

// tfContext is a context with resolved destination names.
type tfContext struct {
	// TFKey is the Terraform resource key (safe HCL identifier).
	TFKey string
	// Name is the destination context name (same as source for now).
	Name string
	// EnvVarKeys lists the env-var names (no values).
	EnvVarKeys []string
	// Restrictions holds context restrictions to emit.
	Restrictions []tfRestriction
}

// tfRestriction is one context restriction, resolved for the destination.
type tfRestriction struct {
	// TFKey is the Terraform resource key (safe HCL identifier).
	TFKey string
	// ContextTFKey is the parent context's TF key.
	ContextTFKey string
	// Type is "project" | "expression" | "group".
	Type string
	// Value is the restriction value — project UUID, expression string, or group UUID.
	// For project restrictions this is the source project UUID (resolved to dest UUID via project resource ref).
	Value string
	// Name is the human-readable name (for comments in HCL).
	Name string
	// ProjectRepoName is the destination project repo name (for project restrictions).
	// Empty for expression and group restrictions.
	ProjectRepoName string
}

// tfProject is a project with resolved destination info.
type tfProject struct {
	TFKey        string
	DestSlug     string
	RepoName     string
	AdvSettings  map[string]bool // attribute name → value
	EnvVarKeys   []string
	Webhooks     []tfWebhook
	PipelineDefs []tfPipelineDef
}

// tfWebhook is a project webhook, resolved for the destination.
type tfWebhook struct {
	// TFKey is the Terraform resource key (safe HCL identifier).
	TFKey string
	// ProjectTFKey is the parent project's TF key.
	ProjectTFKey string
	// Name is the webhook name.
	Name string
	// URL is the webhook URL.
	URL string
	// Events is the list of event types.
	Events []string
	// VerifyTLS indicates whether TLS verification is enabled.
	VerifyTLS bool
}

// tfPipelineDef is an App pipeline definition, resolved for the destination.
type tfPipelineDef struct {
	// TFKey is the Terraform resource key.
	TFKey string
	// ProjectTFKey is the parent project's TF key.
	ProjectTFKey string
	// Name is the pipeline definition name.
	Name string
	// Description is the pipeline definition description.
	Description string
	// ConfigProvider is the config source provider ("github_app" | "github_server").
	ConfigProvider string
	// ConfigRepoExternalID is the config source repo external ID.
	ConfigRepoExternalID string
	// ConfigFilePath is the config file path.
	ConfigFilePath string
	// CheckoutProvider is the checkout source provider.
	CheckoutProvider string
	// CheckoutRepoExternalID is the checkout source repo external ID.
	CheckoutRepoExternalID string
	// Triggers is the list of triggers for this pipeline definition.
	Triggers []tfTrigger
}

// tfTrigger is a pipeline trigger, resolved for the destination.
type tfTrigger struct {
	// TFKey is the Terraform resource key.
	TFKey string
	// PipelineTFKey is the parent pipeline definition's TF key.
	PipelineTFKey string
	// ProjectTFKey is the parent project's TF key.
	ProjectTFKey string
	// Name is the trigger name.
	Name string
	// EventPreset is the event preset (e.g. "all-pushes").
	EventPreset string
	// EventSourceProvider is the event source provider
	// ("github_app" | "github_server" | "webhook" | "schedule").
	EventSourceProvider string
	// EventSourceRepoExternalID is the event source repo external ID
	// (only for github_app/github_server providers).
	EventSourceRepoExternalID string
	// ScheduleCron is the cron expression (only for schedule provider).
	ScheduleCron string
	// Disabled indicates whether the trigger is disabled.
	Disabled bool
}

// tfRunnerClass is a self-hosted runner resource class.
type tfRunnerClass struct {
	// TFKey is the Terraform resource key.
	TFKey string
	// ResourceClass is the full resource class name (namespace/name).
	ResourceClass string
	// Description is the human-readable description.
	Description string
}

// model holds the resolved intermediate representation.
type model struct {
	DestOrgID     string
	DestOrgType   OrgType
	Contexts      []tfContext
	Projects      []tfProject
	RunnerClasses []tfRunnerClass
}

// buildModel resolves the manifest into the intermediate model.
func buildModel(m *manifest.Manifest, mp *manifest.Mapping, destOrgID string, destOrgType OrgType, destRunnerNamespace string) (*model, error) {
	mo := &model{DestOrgID: destOrgID, DestOrgType: destOrgType}

	// Build a map from source project slug → destination project TF key and UUID
	// for resolving context restriction project IDs.
	srcSlugToDestInfo := buildSrcSlugToDestInfo(m, mp)

	// Contexts: sort stable by name for deterministic output.
	ctxs := make([]manifest.Context, len(m.Contexts))
	copy(ctxs, m.Contexts)
	sort.SliceStable(ctxs, func(i, j int) bool { return ctxs[i].Name < ctxs[j].Name })

	for _, c := range ctxs {
		key := TFIdentifier(c.Name)
		vars := make([]string, 0, len(c.EnvVars))
		for _, ev := range c.EnvVars {
			vars = append(vars, ev.Name)
		}
		sort.Strings(vars)

		// Build restrictions — applying org-type gating.
		restrictions := buildRestrictions(c, key, srcSlugToDestInfo, destOrgType)

		mo.Contexts = append(mo.Contexts, tfContext{
			TFKey:        key,
			Name:         c.Name,
			EnvVarKeys:   vars,
			Restrictions: restrictions,
		})
	}

	// Projects: sort stable by slug.
	projs := make([]manifest.Project, len(m.Projects))
	copy(projs, m.Projects)
	sort.SliceStable(projs, func(i, j int) bool { return projs[i].Slug < projs[j].Slug })

	for _, p := range projs {
		destSlug, ok := mp.ResolveProjectSlug(p.Slug)
		if !ok {
			destSlug = p.Slug // best-effort fallback
		}

		// Repo name is the last path component of the slug.
		parts := strings.Split(destSlug, "/")
		repoName := parts[len(parts)-1]

		key := TFIdentifier(repoName)

		advSettings := make(map[string]bool)
		// Advanced settings (GetSettings/UpdateSettings) are only available for
		// standalone (GitHub App / GitLab) projects. For OAuth (gh/) destinations
		// these provider attributes cause terraform apply to fail; omit them.
		if s := p.Settings; s != nil && destOrgType != OrgTypeOAuth {
			// Manifest field → provider attribute mapping (schema-verified):
			//   AutocancelBuilds   → auto_cancel_builds     (note: different casing)
			//   BuildForkPRs       → build_fork_prs
			//   BuildPRsOnly       → no direct boolean; omitted (use pr_only_branch_overrides in M2)
			//   DisableSSH         → disable_ssh
			//   ForksReceiveSecretEnvVars → forks_receive_secret_env_vars
			//   OSS                → no provider attribute; omitted
			//   SetGitHubStatus    → set_github_status
			//   SetupWorkflows     → setup_workflows
			//   WriteSettingsRequiresAdmin → write_settings_requires_admin (IS present in v0.3.x)
			if s.AutocancelBuilds != nil {
				advSettings["auto_cancel_builds"] = *s.AutocancelBuilds
			}
			if s.BuildForkPRs != nil {
				advSettings["build_fork_prs"] = *s.BuildForkPRs
			}
			// BuildPRsOnly: no boolean provider attr in v0.3.x; omitted.
			if s.DisableSSH != nil {
				advSettings["disable_ssh"] = *s.DisableSSH
			}
			if s.ForksReceiveSecretEnvVars != nil {
				advSettings["forks_receive_secret_env_vars"] = *s.ForksReceiveSecretEnvVars
			}
			// OSS: no provider attribute in v0.3.x; omitted.
			if s.SetGitHubStatus != nil {
				advSettings["set_github_status"] = *s.SetGitHubStatus
			}
			if s.SetupWorkflows != nil {
				advSettings["setup_workflows"] = *s.SetupWorkflows
			}
			// WriteSettingsRequiresAdmin IS present in the v0.3.x provider schema.
			if s.WriteSettingsRequiresAdmin != nil {
				advSettings["write_settings_requires_admin"] = *s.WriteSettingsRequiresAdmin
			}
		}

		envVarKeys := make([]string, 0, len(p.EnvVars))
		for _, ev := range p.EnvVars {
			envVarKeys = append(envVarKeys, ev.Name)
		}
		sort.Strings(envVarKeys)

		// Build webhooks.
		webhooks := buildWebhooks(p, key)

		// Build pipeline definitions (standalone only; OAuth provider rejects these).
		var pipelineDefs []tfPipelineDef
		if destOrgType == OrgTypeStandalone {
			pipelineDefs = buildPipelineDefs(p, key)
		}

		mo.Projects = append(mo.Projects, tfProject{
			TFKey:        key,
			DestSlug:     destSlug,
			RepoName:     repoName,
			AdvSettings:  advSettings,
			EnvVarKeys:   envVarKeys,
			Webhooks:     webhooks,
			PipelineDefs: pipelineDefs,
		})
	}

	// Runner resource classes (both org types).
	destNS := destRunnerNamespace
	if destNS == "" {
		destNS = m.RunnerNamespace
	}
	for _, rc := range m.RunnerResourceClasses {
		// Translate namespace: "srcNs/name" → "destNs/name".
		destClass := rc.Name
		if destNS != "" && m.RunnerNamespace != "" && strings.HasPrefix(rc.Name, m.RunnerNamespace+"/") {
			destClass = destNS + "/" + strings.TrimPrefix(rc.Name, m.RunnerNamespace+"/")
		}
		key := TFIdentifier(strings.ReplaceAll(destClass, "/", "_"))
		mo.RunnerClasses = append(mo.RunnerClasses, tfRunnerClass{
			TFKey:         key,
			ResourceClass: destClass,
			Description:   rc.Description,
		})
	}
	// Sort stable by resource class name for deterministic output.
	sort.SliceStable(mo.RunnerClasses, func(i, j int) bool {
		return mo.RunnerClasses[i].ResourceClass < mo.RunnerClasses[j].ResourceClass
	})

	return mo, nil
}

// destProjectInfo records the resolved destination info for a project, used
// for remapping context restriction project IDs.
type destProjectInfo struct {
	TFKey  string
	DestID string // may be empty if no explicit mapping
}

// buildSrcSlugToDestInfo builds a map from source project slug → destProjectInfo.
// The DestID is the source_id (UUID) — for project restrictions we need the
// destination project UUID, but since the provider resolves this through the
// resource reference we store the source_id as-is and the HCL references the
// project resource.
func buildSrcSlugToDestInfo(m *manifest.Manifest, mp *manifest.Mapping) map[string]destProjectInfo {
	out := make(map[string]destProjectInfo, len(m.Projects))
	for _, p := range m.Projects {
		destSlug, ok := mp.ResolveProjectSlug(p.Slug)
		if !ok {
			destSlug = p.Slug
		}
		parts := strings.Split(destSlug, "/")
		repoName := parts[len(parts)-1]
		key := TFIdentifier(repoName)
		out[p.Slug] = destProjectInfo{TFKey: key, DestID: p.SourceID}
		// Also index by source_id so restriction value lookup works.
		if p.SourceID != "" {
			out[p.SourceID] = destProjectInfo{TFKey: key, DestID: p.SourceID}
		}
	}
	return out
}

// buildRestrictions converts manifest restrictions to tf model, applying org-type gating:
//   - project: both org types
//   - expression: both org types
//   - group: OAuth ONLY (provider rejects on standalone orgs)
//
// For project restrictions, the value is the source project UUID. The HCL
// references circleci_project.projects[<key>].id so the destination UUID is
// resolved by Terraform automatically — no manual remap needed.
func buildRestrictions(c manifest.Context, ctxKey string, srcSlugToDestInfo map[string]destProjectInfo, destOrgType OrgType) []tfRestriction {
	var out []tfRestriction
	seen := make(map[string]bool)

	for _, r := range c.Restrictions {
		switch r.Type {
		case "project":
			// Both org types. Look up the project TF key by source project UUID.
			info, ok := srcSlugToDestInfo[r.Value]
			if !ok {
				// Unknown project UUID — cannot map. Use the raw value.
				info = destProjectInfo{TFKey: TFIdentifier(r.Value), DestID: r.Value}
			}
			uniqueKey := ctxKey + "_project_" + info.TFKey
			if seen[uniqueKey] {
				continue
			}
			seen[uniqueKey] = true
			out = append(out, tfRestriction{
				TFKey:           uniqueKey,
				ContextTFKey:    ctxKey,
				Type:            "project",
				Value:           r.Value, // source UUID; HCL resolves via project resource ref
				Name:            r.Name,
				ProjectRepoName: info.TFKey, // repo name for HCL lookup
			})
		case "expression":
			// Both org types.
			uniqueKey := ctxKey + "_expr_" + TFIdentifier(r.Value)
			if seen[uniqueKey] {
				continue
			}
			seen[uniqueKey] = true
			out = append(out, tfRestriction{
				TFKey:        uniqueKey,
				ContextTFKey: ctxKey,
				Type:         "expression",
				Value:        r.Value,
				Name:         r.Name,
			})
		case "group":
			// OAuth ONLY — provider rejects group restrictions on standalone orgs.
			if destOrgType == OrgTypeStandalone {
				continue
			}
			uniqueKey := ctxKey + "_group_" + TFIdentifier(r.Value)
			if seen[uniqueKey] {
				continue
			}
			seen[uniqueKey] = true
			out = append(out, tfRestriction{
				TFKey:        uniqueKey,
				ContextTFKey: ctxKey,
				Type:         "group",
				Value:        r.Value,
				Name:         r.Name,
			})
		}
	}
	return out
}

// buildWebhooks converts manifest webhooks to the tf model.
// Webhooks are supported on both org types.
//
// Provider attribute reference (CircleCI-Public/circleci v0.3.x, schema-verified):
//
//	circleci_webhook:
//	  name       (Required, String)       — Webhook name.
//	  url        (Required, String, Sensitive) — Webhook URL.
//	  events     (Required, Set(String))  — Event types (workflow-completed, job-completed).
//	  verify_tls (Optional, Bool)         — Whether to verify TLS. Default true.
//	  project_id (Required, String)       — The project UUID (from circleci_project.id).
//	  signing_secret (Computed, String, Sensitive) — Server-generated signing secret.
//
// NOTE: project_id must reference the destination project's UUID via the
// circleci_project resource output (.id). The attribute name is project_id
// (not project_slug or project_name).
func buildWebhooks(p manifest.Project, projectKey string) []tfWebhook {
	var out []tfWebhook
	for _, w := range p.Webhooks {
		key := projectKey + "_" + TFIdentifier(w.Name)
		verifyTLS := true
		if w.VerifyTLS != nil {
			verifyTLS = *w.VerifyTLS
		}
		events := make([]string, len(w.Events))
		copy(events, w.Events)
		sort.Strings(events)
		out = append(out, tfWebhook{
			TFKey:        key,
			ProjectTFKey: projectKey,
			Name:         w.Name,
			URL:          w.URL,
			Events:       events,
			VerifyTLS:    verifyTLS,
		})
	}
	return out
}

// buildPipelineDefs converts manifest pipeline definitions to the tf model.
// Only called for standalone (GitHub App) destination orgs.
//
// Provider attribute reference (CircleCI-Public/circleci v0.3.x, schema-verified):
//
//	circleci_pipeline:
//	  name            (Required, String) — Pipeline definition name.
//	  description     (Optional, String) — Human-readable description.
//	  project_id      (Required, String) — Project UUID (from circleci_project.id).
//	  config_source   (Required, Block)  — Config source configuration.
//	    provider      (Required, String) — "github_app" | "github_server".
//	    github_app    (Optional, Block)  — GitHub App config source.
//	      external_id (Required, String) — GitHub repo numeric ID.
//	      file_path   (Optional, String) — Config file path (default ".circleci/config.yml").
//	  checkout_source (Required, Block)  — Checkout source configuration.
//	    provider      (Required, String) — "github_app" | "github_server".
//	    github_app    (Optional, Block)  — GitHub App checkout source.
//	      external_id (Required, String) — GitHub repo numeric ID.
//
// NOTE: The "github_app" sub-block attribute external_id matches the
// TriggerEventSource.RepoExternalID field in the manifest. Provider attr names
// are INFERRED from terraform-provider-circleci source (provider not yet
// fully published with detailed schema docs for these blocks) — marked INFERRED.
//
//	circleci_trigger:
//	  name            (Required, String) — Trigger name.
//	  pipeline_id     (Required, String) — Pipeline definition ID (from circleci_pipeline.id).
//	  project_id      (Required, String) — Project UUID (from circleci_project.id).
//	  event_preset    (Optional, String) — Event preset (e.g. "all-pushes"). INFERRED.
//	  disabled        (Optional, Bool)   — Whether the trigger is disabled.
//	  event_source    (Required, Block)  — Event source configuration. INFERRED.
//	    provider      (Required, String) — "github_app" | "github_server" | "webhook" | "schedule". INFERRED.
//	    github_app    (Optional, Block)  — GitHub App event source. INFERRED.
//	      external_id (Required, String) — GitHub repo numeric ID. INFERRED.
//	    schedule      (Optional, Block)  — Schedule event source. INFERRED.
//	      cron        (Required, String) — Cron expression. INFERRED.
func buildPipelineDefs(p manifest.Project, projectKey string) []tfPipelineDef {
	var out []tfPipelineDef
	for _, pd := range p.PipelineDefinitions {
		// Only emit for github_app and github_server providers (provider schema
		// rejects other providers; inline/oauth configs are not supported).
		provider := strings.ToLower(pd.ConfigSource.Provider)
		if provider != "github_app" && provider != "github_server" {
			continue
		}

		defKey := projectKey + "_" + TFIdentifier(pd.Name)

		var triggers []tfTrigger
		for _, t := range pd.Triggers {
			eventProvider := strings.ToLower(t.EventSource.Provider)
			// Only emit for supported event source providers.
			switch eventProvider {
			case "github_app", "github_server", "webhook", "schedule":
				// supported
			default:
				continue
			}

			trigKey := defKey + "_" + TFIdentifier(t.Name)
			triggers = append(triggers, tfTrigger{
				TFKey:                     trigKey,
				PipelineTFKey:             defKey,
				ProjectTFKey:              projectKey,
				Name:                      t.Name,
				EventPreset:               t.EventPreset,
				EventSourceProvider:       t.EventSource.Provider,
				EventSourceRepoExternalID: t.EventSource.RepoExternalID,
				ScheduleCron:              t.EventSource.ScheduleCron,
				Disabled:                  t.Disabled,
			})
		}

		out = append(out, tfPipelineDef{
			TFKey:                  defKey,
			ProjectTFKey:           projectKey,
			Name:                   pd.Name,
			Description:            pd.Description,
			ConfigProvider:         pd.ConfigSource.Provider,
			ConfigRepoExternalID:   pd.ConfigSource.RepoExternalID,
			ConfigFilePath:         pd.ConfigSource.FilePath,
			CheckoutProvider:       pd.CheckoutSource.Provider,
			CheckoutRepoExternalID: pd.CheckoutSource.RepoExternalID,
			Triggers:               triggers,
		})
	}
	return out
}

// TFIdentifier returns a valid HCL identifier from an arbitrary name.
// Replaces any character that is not alphanumeric or underscore with "_",
// and prefixes with "r" if the name starts with a digit.
// Exported so tests (in _test packages) can verify edge-cases directly.
func TFIdentifier(name string) string {
	var b strings.Builder
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				b.WriteRune('r')
			}
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		return "resource"
	}
	return s
}

// ----------------------------------------------------------------------------
// tfvars builders
// ----------------------------------------------------------------------------

// TFVarsFile is the structure emitted into migration.auto.tfvars.json.
// Using exported fields so json.Marshal picks them up.
type TFVarsFile struct {
	Contexts      []TFVarContext     `json:"contexts,omitempty"`
	Projects      []TFVarProject     `json:"projects,omitempty"`
	Restrictions  []TFVarRestriction `json:"restrictions,omitempty"`
	Webhooks      []TFVarWebhook     `json:"webhooks,omitempty"`
	Pipelines     []TFVarPipeline    `json:"pipelines,omitempty"`
	RunnerClasses []TFVarRunnerClass `json:"runner_classes,omitempty"`
}

// TFVarContext is one context entry in the tfvars file.
type TFVarContext struct {
	Name    string   `json:"name"`
	EnvVars []string `json:"env_vars,omitempty"`
}

// TFVarProject is one project entry in the tfvars file.
type TFVarProject struct {
	RepoName         string          `json:"repo_name"`
	DestSlug         string          `json:"dest_slug"`
	AdvancedSettings map[string]bool `json:"advanced_settings,omitempty"`
	EnvVars          []string        `json:"env_vars,omitempty"`
}

// TFVarRestriction is one context restriction in the tfvars file.
type TFVarRestriction struct {
	// ContextName is the name of the parent context.
	ContextName string `json:"context_name"`
	// Type is "project" | "expression" | "group".
	Type string `json:"type"`
	// Value is the restriction value (project UUID, expression, or group UUID).
	Value string `json:"value"`
	// ProjectRepoName is the repo name of the project for project-type restrictions.
	// Empty for expression and group restrictions.
	// Used by the HCL to look up circleci_project.projects[project_repo_name].id.
	ProjectRepoName string `json:"project_repo_name,omitempty"`
}

// TFVarWebhook is one project webhook in the tfvars file.
type TFVarWebhook struct {
	// ProjectRepoName is the repository name of the parent project.
	ProjectRepoName string `json:"project_repo_name"`
	// Name is the webhook name.
	Name string `json:"name"`
	// URL is the webhook URL.
	URL string `json:"url"`
	// Events is the list of event types.
	Events []string `json:"events,omitempty"`
	// VerifyTLS indicates whether TLS verification is enabled.
	VerifyTLS bool `json:"verify_tls"`
}

// TFVarPipeline is one pipeline definition in the tfvars file.
type TFVarPipeline struct {
	// ProjectRepoName is the repository name of the parent project.
	ProjectRepoName string `json:"project_repo_name"`
	// Name is the pipeline definition name.
	Name string `json:"name"`
	// Description is the pipeline definition description.
	Description string `json:"description,omitempty"`
	// ConfigProvider is the config source provider.
	ConfigProvider string `json:"config_provider"`
	// ConfigRepoExternalID is the config source repo external ID.
	ConfigRepoExternalID string `json:"config_repo_external_id"`
	// ConfigFilePath is the config file path.
	ConfigFilePath string `json:"config_file_path,omitempty"`
	// CheckoutProvider is the checkout source provider.
	CheckoutProvider string `json:"checkout_provider"`
	// CheckoutRepoExternalID is the checkout source repo external ID.
	CheckoutRepoExternalID string `json:"checkout_repo_external_id"`
	// Triggers holds the pipeline trigger configurations.
	Triggers []TFVarTrigger `json:"triggers,omitempty"`
}

// TFVarTrigger is one pipeline trigger in the tfvars file.
type TFVarTrigger struct {
	// Name is the trigger name.
	Name string `json:"name"`
	// EventPreset is the event preset.
	EventPreset string `json:"event_preset,omitempty"`
	// EventSourceProvider is the event source provider.
	EventSourceProvider string `json:"event_source_provider"`
	// EventSourceRepoExternalID is the event source repo external ID.
	EventSourceRepoExternalID string `json:"event_source_repo_external_id,omitempty"`
	// ScheduleCron is the cron expression (schedule triggers only).
	ScheduleCron string `json:"schedule_cron,omitempty"`
	// Disabled indicates whether the trigger is disabled.
	Disabled bool `json:"disabled"`
}

// TFVarRunnerClass is one runner resource class in the tfvars file.
type TFVarRunnerClass struct {
	// ResourceClass is the full resource class name (namespace/name).
	ResourceClass string `json:"resource_class"`
	// Description is the human-readable description.
	Description string `json:"description,omitempty"`
}

func buildTFVars(mo *model, destOrgType OrgType) *TFVarsFile {
	f := &TFVarsFile{}
	for _, c := range mo.Contexts {
		f.Contexts = append(f.Contexts, TFVarContext{
			Name:    c.Name,
			EnvVars: c.EnvVarKeys,
		})
		// Add restrictions.
		for _, r := range c.Restrictions {
			f.Restrictions = append(f.Restrictions, TFVarRestriction{
				ContextName:     c.Name,
				Type:            r.Type,
				Value:           r.Value,
				ProjectRepoName: r.ProjectRepoName,
			})
		}
	}
	for _, p := range mo.Projects {
		f.Projects = append(f.Projects, TFVarProject{
			RepoName:         p.RepoName,
			DestSlug:         p.DestSlug,
			AdvancedSettings: p.AdvSettings,
			EnvVars:          p.EnvVarKeys,
		})
		// Webhooks.
		for _, w := range p.Webhooks {
			f.Webhooks = append(f.Webhooks, TFVarWebhook{
				ProjectRepoName: p.RepoName,
				Name:            w.Name,
				URL:             w.URL,
				Events:          w.Events,
				VerifyTLS:       w.VerifyTLS,
			})
		}
		// Pipeline definitions (standalone only).
		if destOrgType == OrgTypeStandalone {
			for _, pd := range p.PipelineDefs {
				var triggers []TFVarTrigger
				for _, t := range pd.Triggers {
					triggers = append(triggers, TFVarTrigger{
						Name:                      t.Name,
						EventPreset:               t.EventPreset,
						EventSourceProvider:       t.EventSourceProvider,
						EventSourceRepoExternalID: t.EventSourceRepoExternalID,
						ScheduleCron:              t.ScheduleCron,
						Disabled:                  t.Disabled,
					})
				}
				f.Pipelines = append(f.Pipelines, TFVarPipeline{
					ProjectRepoName:        p.RepoName,
					Name:                   pd.Name,
					Description:            pd.Description,
					ConfigProvider:         pd.ConfigProvider,
					ConfigRepoExternalID:   pd.ConfigRepoExternalID,
					ConfigFilePath:         pd.ConfigFilePath,
					CheckoutProvider:       pd.CheckoutProvider,
					CheckoutRepoExternalID: pd.CheckoutRepoExternalID,
					Triggers:               triggers,
				})
			}
		}
	}
	for _, rc := range mo.RunnerClasses {
		f.RunnerClasses = append(f.RunnerClasses, TFVarRunnerClass{
			ResourceClass: rc.ResourceClass,
			Description:   rc.Description,
		})
	}
	return f
}

// SecretsVarsFile is the structure emitted into secrets.auto.tfvars.json.
type SecretsVarsFile struct {
	ContextSecrets map[string]map[string]string `json:"context_secrets,omitempty"`
	ProjectSecrets map[string]map[string]string `json:"project_secrets,omitempty"`
	WebhookURLs    map[string]string            `json:"webhook_urls,omitempty"`
}

func buildSecretsVars(mo *model, bundle *manifest.SecretBundle) *SecretsVarsFile {
	f := &SecretsVarsFile{
		ContextSecrets: make(map[string]map[string]string),
		ProjectSecrets: make(map[string]map[string]string),
	}

	// For each context in the model, look up its secrets in the bundle.
	for _, c := range mo.Contexts {
		vars := make(map[string]string)
		bundleCtx := bundle.ContextSecrets[c.Name]
		for _, k := range c.EnvVarKeys {
			if val, ok := bundleCtx[k]; ok {
				vars[k] = val
			} else {
				vars[k] = ""
			}
		}
		if len(vars) > 0 {
			f.ContextSecrets[c.Name] = vars
		}
	}

	// For each project in the model, look up its secrets.
	for _, p := range mo.Projects {
		vars := make(map[string]string)
		bundleProj := bundle.ProjectSecrets[p.DestSlug]
		if bundleProj == nil {
			// Also try the source slug from env var keys in the bundle.
			// The bundle key may use the source slug; check both.
			for slug, sv := range bundle.ProjectSecrets {
				if strings.HasSuffix(slug, "/"+p.RepoName) {
					bundleProj = sv
					break
				}
			}
		}
		for _, k := range p.EnvVarKeys {
			if val, ok := bundleProj[k]; ok {
				vars[k] = val
			} else {
				vars[k] = ""
			}
		}
		if len(vars) > 0 {
			f.ProjectSecrets[p.RepoName] = vars
		}
	}

	return f
}

func buildPlaceholderVars(mo *model) *SecretsVarsFile {
	f := &SecretsVarsFile{
		ContextSecrets: make(map[string]map[string]string),
		ProjectSecrets: make(map[string]map[string]string),
	}
	for _, c := range mo.Contexts {
		vars := make(map[string]string)
		for _, k := range c.EnvVarKeys {
			vars[k] = "REPLACE_ME"
		}
		if len(vars) > 0 {
			f.ContextSecrets[c.Name] = vars
		}
	}
	for _, p := range mo.Projects {
		vars := make(map[string]string)
		for _, k := range p.EnvVarKeys {
			vars[k] = "REPLACE_ME"
		}
		if len(vars) > 0 {
			f.ProjectSecrets[p.RepoName] = vars
		}
	}
	return f
}

// ----------------------------------------------------------------------------
// Import blocks writer
// ----------------------------------------------------------------------------

// writeImportsFile emits imports.tf with Terraform 1.5+ import {} blocks for
// resources that already exist in the destination. Only resources with a known
// destination ID are included.
//
// The destination ID format for each resource type (from provider docs):
//   - circleci_context: context UUID
//   - circleci_project: project UUID
//   - circleci_webhook: webhook ID
//   - circleci_runner_resource_class: resource class ID
func writeImportsFile(dir string, mo *model, ids *ExistingIDs, destOrgType OrgType) error {
	var b strings.Builder
	b.WriteString("# imports.tf — Terraform 1.5+ import blocks for existing destination resources.\n")
	b.WriteString("# Generated by: circleci-migrate terraform generate --import-existing\n")
	b.WriteString("#\n")
	b.WriteString("# Review each block before running: terraform plan\n")
	b.WriteString("# Remove blocks for resources you want Terraform to create fresh.\n\n")

	wrote := 0

	// Context imports.
	for _, c := range mo.Contexts {
		if id, ok := ids.Contexts[c.Name]; ok && id != "" {
			b.WriteString(fmt.Sprintf("import {\n  to = circleci_context.contexts[%q]\n  id = %q\n}\n\n", c.Name, id))
			wrote++
		}
	}

	// Project imports.
	for _, p := range mo.Projects {
		if id, ok := ids.Projects[p.RepoName]; ok && id != "" {
			b.WriteString(fmt.Sprintf("import {\n  to = circleci_project.projects[%q]\n  id = %q\n}\n\n", p.RepoName, id))
			wrote++
		}
		// Webhook imports.
		for _, w := range p.Webhooks {
			wKey := p.RepoName + "/" + w.Name
			if id, ok := ids.Webhooks[wKey]; ok && id != "" {
				b.WriteString(fmt.Sprintf("import {\n  to = circleci_webhook.webhooks[%q]\n  id = %q\n}\n\n", wKey, id))
				wrote++
			}
		}
	}

	// Runner resource class imports.
	for _, rc := range mo.RunnerClasses {
		if id, ok := ids.RunnerResourceClasses[rc.ResourceClass]; ok && id != "" {
			b.WriteString(fmt.Sprintf("import {\n  to = circleci_runner_resource_class.runners[%q]\n  id = %q\n}\n\n", rc.ResourceClass, id))
			wrote++
		}
	}

	if wrote == 0 {
		b.WriteString("# No existing resource IDs found in the provided --existing file.\n")
		b.WriteString("# All resources will be created fresh by terraform apply.\n")
	}

	return os.WriteFile(filepath.Join(dir, "imports.tf"), []byte(b.String()), 0o644)
}

// ----------------------------------------------------------------------------
// File writers
// ----------------------------------------------------------------------------

func writeFile(dir, name, tmplText string, data any) error {
	tmpl, err := template.New(name).Funcs(template.FuncMap{
		"hclStringList": hclStringList,
		"tfBool":        tfBool,
	}).Parse(tmplText)
	if err != nil {
		return fmt.Errorf("terraform generate: parsing template %s: %w", name, err)
	}
	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("terraform generate: executing template %s: %w", name, err)
	}
	out := buf.String()
	return os.WriteFile(filepath.Join(dir, name), []byte(out), 0o644)
}

// hclStringList formats a []string as an HCL string list literal, e.g. ["a", "b"].
func hclStringList(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteString("[")
	for i, s := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(`"`)
		b.WriteString(strings.ReplaceAll(s, `"`, `\"`))
		b.WriteString(`"`)
	}
	b.WriteString("]")
	return b.String()
}

// tfBool formats a bool as a Terraform boolean literal ("true" / "false").
func tfBool(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func writeJSON(dir, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("terraform generate: marshalling %s: %w", name, err)
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

func writeSecretsWorkbook(dir string, mo *model) error {
	var b strings.Builder
	b.WriteString("# Secrets fill-in workbook\n\n")
	b.WriteString("Fill in the values below, then replace the `REPLACE_ME` placeholders\n")
	b.WriteString("in `secrets.auto.tfvars.json` before running `terraform apply`.\n\n")
	b.WriteString("## Context secrets\n\n")
	for _, c := range mo.Contexts {
		if len(c.EnvVarKeys) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", c.Name))
		b.WriteString("| Variable | Value |\n|---|---|\n")
		for _, k := range c.EnvVarKeys {
			b.WriteString(fmt.Sprintf("| %s | |\n", k))
		}
		b.WriteString("\n")
	}
	b.WriteString("## Project secrets\n\n")
	for _, p := range mo.Projects {
		if len(p.EnvVarKeys) == 0 {
			continue
		}
		b.WriteString(fmt.Sprintf("### %s\n\n", p.RepoName))
		b.WriteString("| Variable | Value |\n|---|---|\n")
		for _, k := range p.EnvVarKeys {
			b.WriteString(fmt.Sprintf("| %s | |\n", k))
		}
		b.WriteString("\n")
	}
	return os.WriteFile(filepath.Join(dir, "SECRETS_WORKBOOK.md"), []byte(b.String()), 0o644)
}

// writeGapsFile emits GAPS.md listing everything Terraform does NOT manage for
// this manifest.
func writeGapsFile(dir string, m *manifest.Manifest, mo *model, destOrgType OrgType) error {
	var b strings.Builder
	b.WriteString("# Migration gaps — resources Terraform does not manage\n\n")
	b.WriteString("The resources below were exported from the source org but are **not**\n")
	b.WriteString("managed by the generated Terraform configuration. Use the indicated\n")
	b.WriteString("`circleci-migrate` commands to complete those steps.\n\n")

	// Separator: what IS managed (M2 scope).
	b.WriteString("## What Terraform manages (M2)\n\n")
	b.WriteString("- **Contexts** (`circleci_context`) + environment variables (`circleci_context_environment_variable`)\n")
	b.WriteString("- **Context restrictions** — `project` and `expression` types (both org types);")
	if destOrgType == OrgTypeOAuth {
		b.WriteString(" `group` type (OAuth org only)\n")
	} else {
		b.WriteString(" `group` type is **omitted** for standalone orgs (provider rejects it)\n")
	}
	b.WriteString("- **Projects** (`circleci_project`) + environment variables (`circleci_project_environment_variable`)")
	if destOrgType == OrgTypeOAuth {
		b.WriteString(" — advanced settings omitted for OAuth orgs\n")
	} else {
		b.WriteString(" — advanced settings included\n")
	}
	b.WriteString("- **Webhooks** (`circleci_webhook`) — both org types\n")
	b.WriteString("- **Self-hosted runners** (`circleci_runner_resource_class` + `circleci_runner_token`) — both org types\n")
	if destOrgType == OrgTypeStandalone {
		b.WriteString("- **Pipeline definitions** (`circleci_pipeline`) + **triggers** (`circleci_trigger`) — standalone/App only\n")
	} else {
		b.WriteString("- **Pipeline definitions** (`circleci_pipeline`) + **triggers** (`circleci_trigger`) — **OMITTED** for OAuth orgs (provider schema rejects `github_oauth`); see gap below\n")
	}
	b.WriteString("\n")

	gaps := collectGaps(m, destOrgType)
	if len(gaps) == 0 {
		b.WriteString("No gaps detected for this manifest. All exported resources are covered\n")
		b.WriteString("by the generated Terraform configuration.\n")
	} else {
		b.WriteString("## Gaps\n\n")
		for _, g := range gaps {
			b.WriteString(fmt.Sprintf("### %s\n\n", g.Title))
			b.WriteString(g.Description + "\n\n")
			b.WriteString("**Status:** " + g.Status + "\n\n")
			if g.Command != "" {
				b.WriteString("```bash\n" + g.Command + "\n```\n\n")
			}
		}
	}

	return os.WriteFile(filepath.Join(dir, "GAPS.md"), []byte(b.String()), 0o644)
}

// gap is one item in GAPS.md.
type gap struct {
	Title       string
	Description string
	Status      string
	Command     string
}

// collectGaps inspects the manifest and returns gaps for anything Terraform
// cannot manage.
func collectGaps(m *manifest.Manifest, destOrgType OrgType) []gap {
	var gaps []gap

	// For OAuth destination orgs, project advanced settings are not managed by
	// Terraform (GetSettings/UpdateSettings is standalone-only). Surface them
	// as a gap pointing at circleci-migrate sync.
	if destOrgType == OrgTypeOAuth {
		hasProjSettings := false
		for _, p := range m.Projects {
			if p.Settings != nil {
				hasProjSettings = true
				break
			}
		}
		if hasProjSettings {
			gaps = append(gaps, gap{
				Title: "Project advanced settings (OAuth org — Terraform cannot manage)",
				Description: "The CircleCI Terraform provider's `circleci_project` advanced-settings\n" +
					"attributes (`auto_cancel_builds`, `build_fork_prs`, `disable_ssh`, etc.) are\n" +
					"**only available for standalone (GitHub App / GitLab) orgs**. For OAuth (`gh/`)\n" +
					"destination orgs they are not included in the generated HCL and must be\n" +
					"configured via the CircleCI UI or via `circleci-migrate sync`.",
				Status:  "Requires UI configuration or CLI sync",
				Command: "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
			})
		}

		// For OAuth: pipelines/triggers are also not manageable via provider.
		hasPipelineDefs := false
		for _, p := range m.Projects {
			if len(p.PipelineDefinitions) > 0 {
				hasPipelineDefs = true
				break
			}
		}
		if hasPipelineDefs {
			gaps = append(gaps, gap{
				Title: "Pipeline definitions and triggers (OAuth org — provider rejects github_oauth)",
				Description: "The `circleci_pipeline` and `circleci_trigger` resources in the CircleCI\n" +
					"Terraform provider only support App (standalone) organizations. For GitHub OAuth\n" +
					"(`gh/`) destination orgs, the provider schema rejects these resources. Pipeline\n" +
					"definitions and triggers captured in the manifest must be recreated via the\n" +
					"CircleCI UI or via `circleci-migrate sync`.\n\n" +
					"**Use `--skip-terraform-managed` with `sync` to avoid double-writing** resources\n" +
					"that Terraform already manages (contexts, projects, webhooks, runners).",
				Status:  "Requires CLI sync (provider cannot manage for OAuth orgs)",
				Command: "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
			})
		}

		// Group restrictions (OAuth orgs SUPPORT group restrictions, but they are
		// VCS-team-specific and may not map to the destination; surface as manual).
		hasGroupRestrictions := false
		for _, c := range m.Contexts {
			for _, r := range c.Restrictions {
				if r.Type == "group" {
					hasGroupRestrictions = true
					break
				}
			}
			if hasGroupRestrictions {
				break
			}
		}
		if hasGroupRestrictions {
			gaps = append(gaps, gap{
				Title: "Context group restrictions (verify destination groups match)",
				Description: "Context group restrictions reference VCS team UUIDs from the source org.\n" +
					"For OAuth destinations, `circleci_context_restriction` with `type=group` IS\n" +
					"generated (OAuth supports group restrictions), but the group UUIDs are\n" +
					"source-org-specific — verify that the same team UUIDs exist in the destination\n" +
					"org before applying.",
				Status:  "Verify destination group UUIDs before terraform apply",
				Command: "",
			})
		}
	}

	// For standalone: group restrictions are omitted (provider rejects them).
	if destOrgType == OrgTypeStandalone {
		hasGroupRestrictions := false
		for _, c := range m.Contexts {
			for _, r := range c.Restrictions {
				if r.Type == "group" {
					hasGroupRestrictions = true
					break
				}
			}
			if hasGroupRestrictions {
				break
			}
		}
		if hasGroupRestrictions {
			gaps = append(gaps, gap{
				Title: "Context group restrictions (standalone org — provider rejects group type)",
				Description: "Context restrictions of `type=group` are only supported for GitHub OAuth\n" +
					"orgs. For standalone (GitHub App / GitLab / `circleci/`-type) destination orgs\n" +
					"the CircleCI provider rejects group restrictions. The group restrictions in the\n" +
					"manifest have been **omitted** from the generated HCL.",
				Status:  "Not applicable for standalone orgs — omitted from Terraform output",
				Command: "",
			})
		}
	}

	// Secret values are never managed by Terraform.
	hasContextVars := false
	for _, c := range m.Contexts {
		if len(c.EnvVars) > 0 {
			hasContextVars = true
			break
		}
	}
	hasProjVars := false
	for _, p := range m.Projects {
		if len(p.EnvVars) > 0 {
			hasProjVars = true
			break
		}
	}
	if hasContextVars || hasProjVars {
		gaps = append(gaps, gap{
			Title:       "Secret values",
			Description: "Terraform receives env-var values via `secrets.auto.tfvars.json`. The values\nmust be supplied separately — use `--secrets bundle.json` or `--placeholders`.",
			Status:      "Requires manual input before `terraform apply`",
			Command:     "circleci-migrate secrets capture  # then re-run: circleci-migrate terraform generate --secrets bundle.json ...",
		})
	}

	// Legacy schedules (v2 schedules, OAuth orgs).
	hasSchedules := false
	for _, p := range m.Projects {
		if len(p.Schedules) > 0 {
			hasSchedules = true
			break
		}
	}
	if hasSchedules {
		gaps = append(gaps, gap{
			Title:       "Legacy v2 schedules",
			Description: "The CircleCI Terraform provider does not cover legacy v2 scheduled pipelines.\nThey must be re-created by the CLI sync command.",
			Status:      "Requires CLI sync",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	// Checkout keys (deploy keys).
	hasCheckoutKeys := false
	for _, p := range m.Projects {
		if len(p.CheckoutKeys) > 0 {
			hasCheckoutKeys = true
			break
		}
	}
	if hasCheckoutKeys {
		gaps = append(gaps, gap{
			Title:       "Checkout / deploy keys",
			Description: "Checkout (deploy) keys are not managed by the CircleCI Terraform provider.\nThe CLI sync command re-creates them on the destination project.",
			Status:      "Requires CLI sync",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	// SSH keys.
	hasSSHKeys := false
	for _, p := range m.Projects {
		if len(p.SSHKeys) > 0 {
			hasSSHKeys = true
			break
		}
	}
	if hasSSHKeys {
		gaps = append(gaps, gap{
			Title:       "Additional SSH keys",
			Description: "Additional SSH keys are not managed by the CircleCI Terraform provider.\nCapture private-key material via `secrets capture --ssh-keys`, then run sync.",
			Status:      "Requires capture + CLI sync",
			Command:     "circleci-migrate secrets capture --ssh-keys\ncircleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	// Project API tokens.
	hasProjTokens := false
	for _, p := range m.Projects {
		if len(p.APITokens) > 0 {
			hasProjTokens = true
			break
		}
	}
	if hasProjTokens {
		gaps = append(gaps, gap{
			Title:       "Project API tokens",
			Description: "Project API tokens (label + scope) are not managed by the Terraform provider.\nThe CLI sync command recreates them and prints the new values.",
			Status:      "Requires CLI sync",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply --create-project-tokens",
		})
	}

	// CIAM roles and groups.
	if m.CIAM != nil && (len(m.CIAM.OrgRoles) > 0 || len(m.CIAM.Groups) > 0 || len(m.CIAM.ProjectUserGrants) > 0 || len(m.CIAM.ProjectGroupGrants) > 0) {
		gaps = append(gaps, gap{
			Title:       "CIAM roles and groups",
			Description: "CIAM org roles, groups, and project grants use private CircleCI endpoints\nthat the Terraform provider does not expose. Apply via CLI sync.",
			Status:      "Requires CLI sync",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	// Org settings.
	if s := m.Source.Org.Settings; s != nil {
		gaps = append(gaps, gap{
			Title:       "Org-level settings",
			Description: "Org settings (feature flags, OIDC custom claims, OTel exporters, contacts,\nstorage retention, budgets, orb allowlist, SSO, release tracker) are not\nmanaged by the CircleCI Terraform provider. Apply via CLI sync.",
			Status:      "Requires CLI sync",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	// Inline orbs.
	hasInlineOrbs := false
	for _, p := range m.Projects {
		for _, pd := range p.PipelineDefinitions {
			if strings.Contains(pd.ConfigSource.Provider, "inline") {
				hasInlineOrbs = true
				break
			}
		}
		if hasInlineOrbs {
			break
		}
	}
	if hasInlineOrbs {
		gaps = append(gaps, gap{
			Title:       "Inline (private) orbs",
			Description: "Private orbs referenced inline in pipeline configs must be inlined at\nsource before the config is committed to the destination.",
			Status:      "Requires orb inline step",
			Command:     "circleci-migrate orb inline --manifest manifest.json",
		})
	}

	// Sync selector hint (M2 feature).
	gaps = append(gaps, gap{
		Title: "CLI gap-fill — sync without double-writing Terraform-managed resources",
		Description: "After `terraform apply`, run `circleci-migrate sync` to complete the items\n" +
			"listed above. Use `--skip-terraform-managed` to avoid overwriting resources that\n" +
			"Terraform already owns (contexts, projects, restrictions, webhooks, runners" +
			func() string {
				if destOrgType == OrgTypeStandalone {
					return ", pipelines/triggers"
				}
				return ""
			}() + ").\n\n" +
			"Alternatively, use `--only <list>` to sync only specific resource types,\n" +
			"e.g. `--only org-settings,ciam,checkout-keys,ssh-keys,schedules,project-tokens`.",
		Status:  "Run after terraform apply",
		Command: "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply --skip-terraform-managed",
	})

	return gaps
}

// PlaintextWarning is the warning message printed when secrets are written
// in plaintext. Matches the same warning used by `secrets decrypt`.
const PlaintextWarning = `WARNING: secrets.auto.tfvars.json contains PLAINTEXT secret values.
  Treat it like a password file:
    • Never commit it to version control.
    • Delete it once 'terraform apply' is complete.
    • Use 'git secret' or a similar tool if long-term storage is needed.`
