// Package report renders a human-readable summary and a saved audit document
// from an export manifest. The summary is printed to the terminal; the audit
// is written to disk so a migration can be reviewed and verified after the fact.
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/cciurl"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// Summary returns a concise, human-readable overview of an export for printing
// to the terminal.
func Summary(m *manifest.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CircleCI export summary\n")
	fmt.Fprintf(&b, "  Source org : %s  (%s)\n", orDash(m.Source.Org.Name), orDash(m.Source.Org.Slug))
	if m.Source.Org.ID != "" {
		fmt.Fprintf(&b, "  Org ID     : %s\n", m.Source.Org.ID)
	}
	fmt.Fprintf(&b, "  Host       : %s\n", orDash(m.Source.Host))
	if m.GeneratedAt != "" {
		fmt.Fprintf(&b, "  Generated  : %s\n", m.GeneratedAt)
	}

	cv := countContextVars(m)
	pv := countProjectVars(m)
	fmt.Fprintf(&b, "\n  Contexts   : %d  (%d env-var name(s); values captured separately)\n", len(m.Contexts), cv)
	fmt.Fprintf(&b, "  Projects   : %d  (%d env-var name(s); values captured separately)\n", len(m.Projects), pv)

	byCode := warningsByCode(m)
	fmt.Fprintf(&b, "  Warnings   : %d\n", len(m.Warnings))
	if len(byCode) > 0 {
		for _, c := range sortedKeys(byCode) {
			fmt.Fprintf(&b, "    - %s (%d)\n", c, byCode[c])
		}
	}

	fmt.Fprintf(&b, "\n  Note: secret VALUES are never exported via API. Use the in-pipeline\n")
	fmt.Fprintf(&b, "  secrets step to capture them. See the audit report for details.\n")
	return b.String()
}

// Markdown returns a full audit document describing everything captured and
// every warning, suitable for saving and reviewing.
func Markdown(m *manifest.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CircleCI migration audit\n\n")
	if m.GeneratedAt != "" {
		fmt.Fprintf(&b, "- Generated: `%s`\n", m.GeneratedAt)
	}
	if m.ToolVersion != "" {
		fmt.Fprintf(&b, "- Tool: `%s`\n", m.ToolVersion)
	}
	fmt.Fprintf(&b, "- Source org: `%s` (`%s`)\n", orDash(m.Source.Org.Name), orDash(m.Source.Org.Slug))
	if m.Source.Org.ID != "" {
		fmt.Fprintf(&b, "- Org ID: `%s`\n", m.Source.Org.ID)
	}
	fmt.Fprintf(&b, "- Host: `%s`\n", orDash(m.Source.Host))
	if s := m.Source.Org.Settings; s != nil && s.RequireContextGroupRestriction != nil {
		fmt.Fprintf(&b, "- Require context group restriction: `%t`\n", *s.RequireContextGroupRestriction)
	}
	if s := m.Source.Org.Settings; s != nil && s.SSO != nil {
		realm := orDash(s.SSO.Realm)
		fmt.Fprintf(&b, "- SSO (SAML): enforced=`%t`, realm=`%s` — must be recreated manually on the destination (DNS domain verification + IdP setup)\n", s.SSO.Enforced, realm)
	}

	fmt.Fprintf(&b, "\n## Summary\n\n")
	fmt.Fprintf(&b, "| Resource | Count | Env-var names captured |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| Contexts | %d | %d |\n", len(m.Contexts), countContextVars(m))
	fmt.Fprintf(&b, "| Projects | %d | %d |\n", len(m.Projects), countProjectVars(m))
	fmt.Fprintf(&b, "| Warnings | %d | — |\n", len(m.Warnings))
	fmt.Fprintf(&b, "\n_Legend: \"Env-var names captured\" is the count of variable **names** (not values). Secret values are never returned by the CircleCI API; capture them with `circleci-migrate secrets capture`, then pass the bundle to `sync`._\n")

	// Org settings — everything readable at the org level.
	writeOrgSettings(&b, m)

	// Contexts
	contextsURL := orgSettingsURL(m.Source.Host, m.Source.Org.Slug, "contexts")
	fmt.Fprintf(&b, "\n## Contexts\n\n")
	if len(m.Contexts) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	}
	for _, c := range m.Contexts {
		// Link the heading to the org Contexts settings page (#A3).
		fmt.Fprintf(&b, "### [%s](%s)\n\n", c.Name, contextsURL)
		fmt.Fprintf(&b, "- Environment variables (%d): %s\n", len(c.EnvVars), joinNames(contextVarNames(c)))
		if len(c.Restrictions) > 0 {
			fmt.Fprintf(&b, "- Restrictions:\n")
			for _, r := range c.Restrictions {
				label := r.Name
				if label == "" {
					label = r.Value
				}
				fmt.Fprintf(&b, "  - `%s`: %s\n", r.Type, label)
			}
		}
		if len(c.SecurityGroups) > 0 {
			names := make([]string, 0, len(c.SecurityGroups))
			for _, g := range c.SecurityGroups {
				names = append(names, g.Name)
			}
			fmt.Fprintf(&b, "- Security groups: %s\n", joinNames(names))
		}
		fmt.Fprintf(&b, "\n")
	}

	// Projects
	fmt.Fprintf(&b, "## Projects\n\n")
	if len(m.Projects) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	}
	for _, p := range m.Projects {
		// Show the human-readable Name primarily; fall back to Slug when Name is
		// empty (older manifests or synthesised project entries). Link heading to
		// the project's settings page (#A3/#A5).
		settingsURL := projectSettingsURL(m.Source.Host, p.Slug, "")
		var projectHeadingText string
		if p.Name == "" {
			projectHeadingText = p.Slug
		} else {
			projectHeadingText = fmt.Sprintf("%s (`%s`)", p.Name, p.Slug)
		}
		fmt.Fprintf(&b, "### [%s](%s)\n\n", projectHeadingText, settingsURL)
		if repoLine := projectRepoLine(p.VCS); repoLine != "" {
			fmt.Fprintf(&b, "- Repository: %s\n", repoLine)
		}
		if p.VCS.DefaultBranch != "" {
			fmt.Fprintf(&b, "- Default branch: `%s`\n", p.VCS.DefaultBranch)
		}
		fmt.Fprintf(&b, "- Environment variables (%d): %s\n", len(p.EnvVars), joinNames(projectVarNames(p)))
		if p.Settings != nil {
			fmt.Fprintf(&b, "- Advanced settings: %s\n", joinNames(setSettings(p.Settings)))
		}

		// Checkout keys — detailed per-key listing (#A4).
		if len(p.CheckoutKeys) > 0 {
			fmt.Fprintf(&b, "- Checkout keys (%d):\n", len(p.CheckoutKeys))
			for _, k := range p.CheckoutKeys {
				preferred := ""
				if k.Preferred {
					preferred = " ✓ preferred"
				}
				fmt.Fprintf(&b, "  - type: `%s`, fingerprint: `%s`%s\n", k.Type, orDash(k.Fingerprint), preferred)
			}
		}

		// Additional SSH keys.
		if len(p.SSHKeys) > 0 {
			fmt.Fprintf(&b, "- Additional SSH keys (%d):\n", len(p.SSHKeys))
			for _, k := range p.SSHKeys {
				host := k.Hostname
				if host == "" {
					host = "(global)"
				}
				pubKeyPreview := sshPublicKeyPreview(k.PublicKey)
				fmt.Fprintf(&b, "  - host: `%s`, fingerprint: `%s`, key: `%s`\n", host, orDash(k.Fingerprint), pubKeyPreview)
			}
		}

		// Webhooks — detailed per-webhook listing (#A4).
		if len(p.Webhooks) > 0 {
			fmt.Fprintf(&b, "- Webhooks (%d):\n", len(p.Webhooks))
			for _, w := range p.Webhooks {
				tls := ""
				if w.VerifyTLS != nil {
					tls = fmt.Sprintf(", verify-tls: `%t`", *w.VerifyTLS)
				}
				events := ""
				if len(w.Events) > 0 {
					events = fmt.Sprintf(", events: %s", joinNames(w.Events))
				}
				fmt.Fprintf(&b, "  - `%s` → `%s`%s%s\n", w.Name, w.URL, tls, events)
			}
		}

		// Schedules — detailed per-schedule listing (#A4).
		if len(p.Schedules) > 0 {
			fmt.Fprintf(&b, "- Schedules (%d):\n", len(p.Schedules))
			for _, sched := range p.Schedules {
				actor := ""
				if sched.ActorLogin != "" {
					actor = fmt.Sprintf(", actor: `%s`", sched.ActorLogin)
				}
				desc := ""
				if sched.Description != "" {
					desc = fmt.Sprintf(" — %s", sched.Description)
				}
				fmt.Fprintf(&b, "  - `%s`%s%s\n", sched.Name, desc, actor)
			}
		}

		// Per-project OIDC custom claims (#A4).
		if len(p.OIDCAudience) > 0 || p.OIDCTTL != "" {
			fmt.Fprintf(&b, "- OIDC custom claims: audience=%s, TTL=`%s`\n", joinNames(p.OIDCAudience), orDash(p.OIDCTTL))
		}

		// Pipeline definitions + triggers (#A1).
		if len(p.PipelineDefinitions) > 0 {
			fmt.Fprintf(&b, "- Pipeline definitions (%d):\n", len(p.PipelineDefinitions))
			for _, pd := range p.PipelineDefinitions {
				descLine := ""
				if pd.Description != "" {
					descLine = fmt.Sprintf(" — %s", pd.Description)
				}
				configSrc := pipelineSourceLabel(pd.ConfigSource)
				checkoutSrc := pipelineSourceLabel(pd.CheckoutSource)
				pdName := pd.Name
				if pdName == "" {
					pdName = "(unnamed pipeline)"
				}
				fmt.Fprintf(&b, "  - **`%s`**%s\n", pdName, descLine)
				if configSrc != "" {
					fmt.Fprintf(&b, "    - Config: %s\n", configSrc)
				}
				if checkoutSrc != "" {
					fmt.Fprintf(&b, "    - Checkout: %s\n", checkoutSrc)
				}
				if len(pd.Triggers) > 0 {
					fmt.Fprintf(&b, "    - Triggers (%d):\n", len(pd.Triggers))
					for _, tr := range pd.Triggers {
						disabledMark := ""
						if tr.Disabled {
							disabledMark = " _(disabled)_"
						}
						cronInfo := ""
						if tr.EventSource.ScheduleCron != "" {
							cronInfo = fmt.Sprintf(", cron: `%s`", tr.EventSource.ScheduleCron)
						}
						fmt.Fprintf(&b, "      - `%s` (event: `%s`, provider: `%s`%s)%s\n",
							tr.Name, orDash(tr.EventName), orDash(tr.EventSource.Provider), cronInfo, disabledMark)
					}
				}
			}
		}

		// Project API token metadata (#A4).
		if len(p.APITokens) > 0 {
			fmt.Fprintf(&b, "- API tokens (%d): ", len(p.APITokens))
			labels := make([]string, 0, len(p.APITokens))
			for _, t := range p.APITokens {
				labels = append(labels, fmt.Sprintf("%s (%s)", t.Label, t.Scope))
			}
			fmt.Fprintf(&b, "%s\n", strings.Join(labels, ", "))
		}

		if p.Settings != nil && len(p.Settings.V11FeatureFlags) > 0 {
			// Show any flags not already shown via the explicit settings fields.
			var extra []string
			for k, v := range p.Settings.V11FeatureFlags {
				if k != "api-trigger-with-config" && k != "drop-all-build-requests" {
					extra = append(extra, fmt.Sprintf("%s=%t", k, v))
				}
			}
			if len(extra) > 0 {
				sort.Strings(extra)
				fmt.Fprintf(&b, "- Additional v1.1 feature flags: %s\n", strings.Join(extra, ", "))
			}
		}
		fmt.Fprintf(&b, "\n")
	}

	// Runner resource classes (#A4).
	if len(m.RunnerResourceClasses) > 0 {
		fmt.Fprintf(&b, "## Runner resource classes\n\n")
		fmt.Fprintf(&b, "_Self-hosted runner resource classes captured from namespace `%s`. Agent registration tokens are never retrievable via API and must be reissued after recreating each class._\n\n", orDash(m.RunnerNamespace))
		fmt.Fprintf(&b, "| Name | Description |\n|---|---|\n")
		for _, rc := range m.RunnerResourceClasses {
			fmt.Fprintf(&b, "| `%s` | %s |\n", rc.Name, orDash(rc.Description))
		}
		fmt.Fprintf(&b, "\n")
	}

	// Warnings
	fmt.Fprintf(&b, "## Warnings & manual follow-ups\n\n")
	if len(m.Warnings) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	} else {
		fmt.Fprintf(&b, "| Scope | Code | Detail |\n|---|---|---|\n")
		for _, w := range m.Warnings {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", w.Scope, w.Code, escapePipes(w.Message))
		}
	}

	// CIAM roles and groups — circleci-type orgs only.
	writeCIAMSection(&b, m)

	// Cutover runbook — the operator-facing checklist the customer follows to
	// finish the migration. Everything below the warnings table is derived from
	// the manifest plus the set of known manual/limitation facts.
	writeRunbook(&b, m)

	return b.String()
}

// SaveMarkdown writes the audit document to path.
func SaveMarkdown(m *manifest.Manifest, path string) error {
	if err := os.WriteFile(path, []byte(Markdown(m)), 0o644); err != nil {
		return fmt.Errorf("writing audit report %s: %w", path, err)
	}
	return nil
}

// --- URL builders ----------------------------------------------------------
//
// These thin wrappers delegate to internal/cciurl so the logic lives in one
// place and can be shared with other packages (e.g. cmd/sync plan renderer).

// appHost returns the CircleCI app host for building settings URLs.
// Delegates to cciurl.AppHost; kept here so existing callers within this
// package are unchanged.
func appHost(sourceHost string) string {
	return cciurl.AppHost(sourceHost)
}

// projectSettingsURL returns the CircleCI web-app URL for a project's settings
// root. Delegates to cciurl.ProjectSettingsURL.
func projectSettingsURL(sourceHost, slug, tab string) string {
	return cciurl.ProjectSettingsURL(sourceHost, slug, tab)
}

// orgSettingsURL returns the CircleCI web-app URL for the org settings root (or
// a named tab). Delegates to cciurl.OrgSettingsURL.
func orgSettingsURL(sourceHost, orgSlug, tab string) string {
	return cciurl.OrgSettingsURL(sourceHost, orgSlug, tab)
}

// --- helpers ---------------------------------------------------------------

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// ciamUserCell returns the identifier to show for a CIAM user grant: the email
// when present, otherwise the username, otherwise an em dash. The CIAM
// role-grants API frequently returns an empty email, so falling back to the
// username keeps the report's user columns from rendering as blank cells.
func ciamUserCell(email, username string) string {
	if email != "" {
		return email
	}
	if username != "" {
		return username
	}
	return "—"
}

func countContextVars(m *manifest.Manifest) int {
	n := 0
	for _, c := range m.Contexts {
		n += len(c.EnvVars)
	}
	return n
}

func countProjectVars(m *manifest.Manifest) int {
	n := 0
	for _, p := range m.Projects {
		n += len(p.EnvVars)
	}
	return n
}

func contextVarNames(c manifest.Context) []string {
	names := make([]string, 0, len(c.EnvVars))
	for _, v := range c.EnvVars {
		names = append(names, v.Name)
	}
	return names
}

func projectVarNames(p manifest.Project) []string {
	names := make([]string, 0, len(p.EnvVars))
	for _, v := range p.EnvVars {
		names = append(names, v.Name)
	}
	return names
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "_none_"
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}

func setSettings(s *manifest.AdvancedSettings) []string {
	var out []string
	add := func(name string, v *bool) {
		if v != nil {
			out = append(out, fmt.Sprintf("%s=%t", name, *v))
		}
	}
	add("autocancel_builds", s.AutocancelBuilds)
	add("build_fork_prs", s.BuildForkPRs)
	add("build_prs_only", s.BuildPRsOnly)
	add("disable_ssh", s.DisableSSH)
	add("forks_receive_secret_env_vars", s.ForksReceiveSecretEnvVars)
	add("oss", s.OSS)
	add("set_github_status", s.SetGitHubStatus)
	add("setup_workflows", s.SetupWorkflows)
	add("write_settings_requires_admin", s.WriteSettingsRequiresAdmin)
	if len(s.PROnlyBranchOverrides) > 0 {
		out = append(out, "pr_only_branch_overrides=["+strings.Join(s.PROnlyBranchOverrides, ",")+"]")
	}
	return out
}

func warningsByCode(m *manifest.Manifest) map[string]int {
	byCode := map[string]int{}
	for _, w := range m.Warnings {
		byCode[w.Code]++
	}
	return byCode
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}

// warningSuffix returns " (export note: <message>)" for the first warning whose
// code contains any of the given substrings, or "" when none match. It lets the
// runbook quote the export's own recorded warning text where present.
func warningSuffix(m *manifest.Manifest, codeSubstrings ...string) string {
	for _, w := range m.Warnings {
		for _, sub := range codeSubstrings {
			if strings.Contains(w.Code, sub) {
				return " (export note: " + w.Message + ")"
			}
		}
	}
	return ""
}

// hasPipelineDefinitions reports whether any project captured an App-style
// pipeline definition, which signals an App (circleci-type) destination flow.
func hasPipelineDefinitions(m *manifest.Manifest) bool {
	for _, p := range m.Projects {
		if len(p.PipelineDefinitions) > 0 {
			return true
		}
	}
	return false
}

// hasAdditionalSSHKeys reports whether any project in the manifest has at
// least one additional SSH key entry captured.
func hasAdditionalSSHKeys(m *manifest.Manifest) bool {
	for _, p := range m.Projects {
		if len(p.SSHKeys) > 0 {
			return true
		}
	}
	return false
}

// hasWebhooks reports whether any project in the manifest has at least one
// webhook captured.
func hasWebhooks(m *manifest.Manifest) bool {
	for _, p := range m.Projects {
		if len(p.Webhooks) > 0 {
			return true
		}
	}
	return false
}

// hasAPITokens reports whether any project in the manifest has at least one
// project API token captured.
func hasAPITokens(m *manifest.Manifest) bool {
	for _, p := range m.Projects {
		if len(p.APITokens) > 0 {
			return true
		}
	}
	return false
}

// hasBudgetEnforcementBlock reports whether any captured budget entry has
// EnforcementType == "block".
func hasBudgetEnforcementBlock(m *manifest.Manifest) bool {
	s := m.Source.Org.Settings
	if s == nil || s.Budgets == nil {
		return false
	}
	b := s.Budgets
	if b.OrgBudget != nil && b.OrgBudget.EnforcementType == "block" {
		return true
	}
	for _, pb := range b.ProjectBudgets {
		if pb.EnforcementType == "block" {
			return true
		}
	}
	return false
}

// isOAuthSource reports whether the source org is a GitHub OAuth org, inferred
// from the "gh/" slug prefix (App / GitLab orgs use "circleci/").
func isOAuthSource(m *manifest.Manifest) bool {
	return strings.HasPrefix(m.Source.Org.Slug, "gh/")
}

// projectDisplayName returns the human-readable name for a project. It prefers
// the Name field; falls back to the Slug when Name is empty.
func projectDisplayName(p manifest.Project) string {
	if p.Name != "" {
		return p.Name
	}
	return p.Slug
}

// sshKeyHostnames returns a comma-separated list of unique hostnames from the
// given SSH keys, replacing empty hostnames with "(global)". Used in the manual
// steps section to tell operators which hosts need their keys re-added.
func sshKeyHostnames(keys []manifest.ProjectSSHKey) string {
	seen := map[string]struct{}{}
	var hosts []string
	for _, k := range keys {
		h := k.Hostname
		if h == "" {
			h = "(global)"
		}
		if _, ok := seen[h]; !ok {
			seen[h] = struct{}{}
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return "(none)"
	}
	return strings.Join(hosts, ", ")
}

// projectNameByID returns the human-readable name of the project whose
// SourceID matches projID. When no match is found it falls back to the UUID
// itself so the output is always actionable.
func projectNameByID(m *manifest.Manifest, projID *string) string {
	if projID == nil {
		return "unknown"
	}
	id := *projID
	for _, p := range m.Projects {
		if p.SourceID == id {
			return projectDisplayName(p)
		}
	}
	return id // fallback: show the UUID rather than nothing
}

// pipelineSourceLabel returns a concise human-readable label for a PipelineSource.
// It shows the repo full name and file path when available, falling back to the
// provider alone when no repo is recorded. Returns "" for an empty source.
func pipelineSourceLabel(src manifest.PipelineSource) string {
	if src.Provider == "" && src.RepoFullName == "" && src.FilePath == "" {
		return ""
	}
	var parts []string
	if src.Provider != "" {
		parts = append(parts, src.Provider)
	}
	if src.RepoFullName != "" {
		parts = append(parts, src.RepoFullName)
	}
	if src.FilePath != "" {
		parts = append(parts, src.FilePath)
	}
	return strings.Join(parts, " / ")
}

// sshPublicKeyPreview returns a short preview of an SSH public-key string for
// display in reports (e.g. "ssh-rsa AAAAB3... [truncated]"). The preview shows
// the key type and the first 12 characters of the key body, followed by "..."
// when the full key is longer. This gives the operator enough to visually
// recognise a key without bloating the report.
func sshPublicKeyPreview(pub string) string {
	if pub == "" {
		return "—"
	}
	const maxLen = 32
	if len(pub) <= maxLen {
		return pub
	}
	return pub[:maxLen] + "..."
}

// projectRepoLine returns the "Repository" value for a project's VCS entry, or
// "" when nothing useful can be shown. It handles three cases:
//
//  1. CircleCI-native / App projects: provider == "circleci" or the URL is an
//     opaque scheme-less "//circleci.com/…" UUID path. In this case the URL
//     carries no useful human-readable information, so we return a static label
//     rather than emitting a confusing "//" URL.
//
//  2. Real VCS providers (GitHub, GitLab, Bitbucket): normalise any accidental
//     scheme-less "//host/…" URL to "https://host/…" so the rendered line
//     always contains a clickable https URL. If, after normalisation, the URL
//     still looks like a UUID path (no recognisable hostname), omit it and just
//     show the provider name.
//
//  3. No provider AND no URL: return "" so the caller skips the line entirely.
func projectRepoLine(vcs manifest.ProjectVCS) string {
	lowerProvider := strings.ToLower(vcs.Provider)

	// CircleCI-native projects have provider=="circleci" or a scheme-less URL
	// whose host is "circleci.com" with UUID path segments.
	if lowerProvider == "circleci" || isCircleCINativeURL(vcs.URL) {
		return "CircleCI-native project (no external VCS repo)"
	}

	// No provider and no URL: nothing to show.
	if vcs.Provider == "" && vcs.URL == "" {
		return ""
	}

	// Normalise a scheme-less "//host/path" URL to "https://host/path".
	u := normaliseVCSURL(vcs.URL)

	if vcs.Provider != "" {
		if u != "" {
			return fmt.Sprintf("%s — `%s`", vcs.Provider, u)
		}
		return vcs.Provider
	}
	// URL only (no provider label).
	return fmt.Sprintf("`%s`", u)
}

// isCircleCINativeURL reports whether url is the scheme-less opaque form that
// CircleCI returns for App-native (non-VCS) projects, e.g.
// "//circleci.com/<org-uuid>/<proj-uuid>".  These URLs carry no useful
// human-readable information and must not be emitted in reports.
func isCircleCINativeURL(url string) bool {
	return strings.HasPrefix(url, "//circleci.com/") || strings.HasPrefix(url, "//circleci.com")
}

// normaliseVCSURL converts a scheme-less "//host/path" URL (as sometimes
// returned by the CircleCI project API for GitHub/Bitbucket repos) to an
// "https://host/path" URL. Already-valid https:// URLs are returned unchanged.
// Empty strings are returned as-is. If the resulting URL still looks like a
// UUID-only path, "" is returned so the caller can omit it gracefully.
func normaliseVCSURL(rawURL string) string {
	if rawURL == "" {
		return ""
	}
	u := rawURL
	if strings.HasPrefix(u, "//") {
		u = "https:" + u
	}
	return u
}
