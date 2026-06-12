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
	OrgTypeOAuth
	// OrgTypeStandalone represents a GitHub App / GitLab / standalone org
	// (slug prefix "circleci/"). All circleci_project attributes are available.
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
					"      To override, pass --dest-org-type standalone.\n",
				m.Source.Org.Slug)
		case OrgTypeStandalone:
			fmt.Fprintf(os.Stderr,
				"Note: --dest-org-type not set; inferred \"standalone\" from source slug %q.\n"+
					"      Advanced project settings will be included.\n"+
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
	model, err := buildModel(m, mp, opts.DestOrgID, destOrgType)
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

	// Emit migration.auto.tfvars.json (non-secret values).
	tfvars := buildTFVars(model)
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
}

// tfProject is a project with resolved destination info.
type tfProject struct {
	TFKey       string
	DestSlug    string
	RepoName    string
	AdvSettings map[string]bool // attribute name → value
	EnvVarKeys  []string
}

// model holds the resolved intermediate representation.
type model struct {
	DestOrgID   string
	DestOrgType OrgType
	Contexts    []tfContext
	Projects    []tfProject
}

// buildModel resolves the manifest into the intermediate model.
func buildModel(m *manifest.Manifest, mp *manifest.Mapping, destOrgID string, destOrgType OrgType) (*model, error) {
	mo := &model{DestOrgID: destOrgID, DestOrgType: destOrgType}

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
		mo.Contexts = append(mo.Contexts, tfContext{
			TFKey:      key,
			Name:       c.Name,
			EnvVarKeys: vars,
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

		vars := make([]string, 0, len(p.EnvVars))
		for _, ev := range p.EnvVars {
			vars = append(vars, ev.Name)
		}
		sort.Strings(vars)

		mo.Projects = append(mo.Projects, tfProject{
			TFKey:       key,
			DestSlug:    destSlug,
			RepoName:    repoName,
			AdvSettings: advSettings,
			EnvVarKeys:  vars,
		})
	}

	return mo, nil
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
	Contexts []TFVarContext `json:"contexts,omitempty"`
	Projects []TFVarProject `json:"projects,omitempty"`
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

func buildTFVars(mo *model) *TFVarsFile {
	f := &TFVarsFile{}
	for _, c := range mo.Contexts {
		f.Contexts = append(f.Contexts, TFVarContext{
			Name:    c.Name,
			EnvVars: c.EnvVarKeys,
		})
	}
	for _, p := range mo.Projects {
		f.Projects = append(f.Projects, TFVarProject{
			RepoName:         p.RepoName,
			DestSlug:         p.DestSlug,
			AdvancedSettings: p.AdvSettings,
			EnvVars:          p.EnvVarKeys,
		})
	}
	return f
}

// SecretsVarsFile is the structure emitted into secrets.auto.tfvars.json.
type SecretsVarsFile struct {
	ContextSecrets map[string]map[string]string `json:"context_secrets,omitempty"`
	ProjectSecrets map[string]map[string]string `json:"project_secrets,omitempty"`
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
// File writers
// ----------------------------------------------------------------------------

func writeFile(dir, name, tmplText string, data any) error {
	tmpl, err := template.New(name).Parse(tmplText)
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

	gaps := collectGaps(m, destOrgType)
	if len(gaps) == 0 {
		b.WriteString("No gaps detected for this manifest. All exported resources are covered\n")
		b.WriteString("by the generated Terraform configuration.\n")
	} else {
		for _, g := range gaps {
			b.WriteString(fmt.Sprintf("## %s\n\n", g.Title))
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

	// Context restrictions (M2 scope).
	hasRestrictions := false
	for _, c := range m.Contexts {
		if len(c.Restrictions) > 0 {
			hasRestrictions = true
			break
		}
	}
	if hasRestrictions {
		gaps = append(gaps, gap{
			Title:       "Context restrictions",
			Description: "Context restrictions (project/expression/group) are scoped to M2 Terraform\nsupport. For now, apply them via CLI sync.",
			Status:      "Requires CLI sync (Terraform M2)",
			Command:     "circleci-migrate sync --manifest manifest.json --dest-token $CIRCLECI_DEST_TOKEN --apply",
		})
	}

	return gaps
}

// PlaintextWarning is the warning message printed when secrets are written
// in plaintext. Matches the same warning used by `secrets decrypt`.
const PlaintextWarning = `WARNING: secrets.auto.tfvars.json contains PLAINTEXT secret values.
  Treat it like a password file:
    • Never commit it to version control.
    • Delete it once 'terraform apply' is complete.
    • Use 'git secret' or a similar tool if long-term storage is needed.`
