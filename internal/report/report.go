// Package report renders a human-readable summary and a saved audit document
// from an export manifest. The summary is printed to the terminal; the audit
// is written to disk so a migration can be reviewed and verified after the fact.
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"

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
	fmt.Fprintf(&b, "\n  Contexts   : %d  (%d variable name(s))\n", len(m.Contexts), cv)
	fmt.Fprintf(&b, "  Projects   : %d  (%d variable name(s))\n", len(m.Projects), pv)

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
	fmt.Fprintf(&b, "| Resource | Count | Variable names |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| Contexts | %d | %d |\n", len(m.Contexts), countContextVars(m))
	fmt.Fprintf(&b, "| Projects | %d | %d |\n", len(m.Projects), countProjectVars(m))
	fmt.Fprintf(&b, "| Warnings | %d | |\n", len(m.Warnings))

	// Contexts
	fmt.Fprintf(&b, "\n## Contexts\n\n")
	if len(m.Contexts) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	}
	for _, c := range m.Contexts {
		fmt.Fprintf(&b, "### `%s`\n\n", c.Name)
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
		// empty (older manifests or synthesised project entries).
		projectHeader := p.Name
		if projectHeader == "" {
			projectHeader = p.Slug
		} else {
			projectHeader = fmt.Sprintf("%s (`%s`)", p.Name, p.Slug)
		}
		fmt.Fprintf(&b, "### %s\n\n", projectHeader)
		if p.VCS.DefaultBranch != "" {
			fmt.Fprintf(&b, "- Default branch: `%s`\n", p.VCS.DefaultBranch)
		}
		fmt.Fprintf(&b, "- Environment variables (%d): %s\n", len(p.EnvVars), joinNames(projectVarNames(p)))
		if p.Settings != nil {
			fmt.Fprintf(&b, "- Advanced settings: %s\n", joinNames(setSettings(p.Settings)))
		}
		if len(p.CheckoutKeys) > 0 {
			fmt.Fprintf(&b, "- Checkout keys: %d\n", len(p.CheckoutKeys))
		}
		if len(p.Webhooks) > 0 {
			fmt.Fprintf(&b, "- Webhooks: %d\n", len(p.Webhooks))
		}
		if len(p.Schedules) > 0 {
			fmt.Fprintf(&b, "- Schedules: %d\n", len(p.Schedules))
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

	// Cutover runbook — the operator-facing checklist the customer follows to
	// finish the migration. Everything below the warnings table is derived from
	// the manifest plus the set of known manual/limitation facts.
	writeRunbook(&b, m)

	return b.String()
}

// writeRunbook appends the customer-facing cutover runbook to b: the recommended
// order, what sync automates, the manual steps (data-driven from the manifest),
// what does not transfer, and the external-pin reminder.
func writeRunbook(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n## Cutover runbook\n\n")
	fmt.Fprintf(b, "This report is your migration plan. Work through the steps below in order;\n")
	fmt.Fprintf(b, "the manual steps and data-loss notes are tailored to what this export contains.\n")

	writeCutoverOrder(b)
	writeAutomatedBySync(b)
	writeManualSteps(b, m)
	writeDataLoss(b, m)
	writeExternalPins(b)
}

// writeCutoverOrder renders the recommended, numbered cutover sequence.
func writeCutoverOrder(b *strings.Builder) {
	fmt.Fprintf(b, "\n### 1. Recommended cutover order\n\n")
	fmt.Fprintf(b, "1. **Export the source org** — done; this report is the result. Review it before continuing.\n")
	fmt.Fprintf(b, "2. **Capture secret values** — run the in-pipeline `secrets` orb/step (or `secrets capture`) in the source org to collect context and project env-var values. They are never exported via the API.\n")
	fmt.Fprintf(b, "3. **`sync --apply`** — creates the destination resources. New projects are created **paused**: App triggers are disabled and OAuth onboarding is not followed, so no builds run yet.\n")
	fmt.Fprintf(b, "4. **Validate the destination** — confirm contexts, env-var names, project settings, webhooks, schedules, and group restrictions look correct against this report.\n")
	fmt.Fprintf(b, "5. **Enable builds** — turn the destination live (`sync --yes`, the interactive prompt, or re-enable triggers / follow projects).\n")
	fmt.Fprintf(b, "6. **Rotate the captured secrets** — once builds are healthy, rotate every value you captured in step 2 and delete the extraction artifacts (`secrets.json` and any logs).\n")
	fmt.Fprintf(b, "7. **Update external pins** — repoint anything that references the old org (see the last section).\n")
}

// writeAutomatedBySync lists what `sync --apply` handles end-to-end.
func writeAutomatedBySync(b *strings.Builder) {
	fmt.Fprintf(b, "\n### 2. Automated by `sync --apply`\n\n")
	fmt.Fprintf(b, "- Contexts and their environment variables (names; values from the capture step).\n")
	fmt.Fprintf(b, "- Project settings, environment variables, webhooks, and scheduled pipelines.\n")
	fmt.Fprintf(b, "- Project- and org-level OIDC custom claims (audience / TTL).\n")
	fmt.Fprintf(b, "- Org settings: feature flags, OIDC, URL-orb allow list, config policies, technical/security contacts.\n")
	fmt.Fprintf(b, "- Project creation: OAuth orgs are onboarded by following the project; App orgs get their pipeline definitions and triggers recreated.\n")
	fmt.Fprintf(b, "- Context group restrictions, mapped onto destination CIAM groups.\n")
}

// writeManualSteps renders the manual steps required to finish the migration.
// The list is data-driven: an item is included only when the manifest provides
// the corresponding signal, with a small always-include baseline (secret values
// and key regeneration always apply). Warning messages are pulled in where the
// export recorded them.
func writeManualSteps(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n### 3. Manual steps required\n\n")

	var items []string
	s := m.Source.Org.Settings

	// Always: secret values must be captured and re-applied.
	items = append(items, "**Context & project secret values** — CircleCI never exports env-var values. "+
		"Capture them with the in-pipeline `secrets` step in the source org, supply the bundle to `sync`, then rotate them after cutover."+
		warningSuffix(m, "context_values_excluded", "project_values_excluded"))

	// Always: checkout / SSH keys cannot be transferred.
	items = append(items, "**Checkout & SSH keys** — private key material is never exported. "+
		"Regenerate deploy/checkout and user keys on the destination and update any VCS-side deploy keys.")

	// Always: webhook signing secrets.
	items = append(items, "**Webhook signing secrets** — outbound webhook signing secrets are not exported; "+
		"regenerate them on the destination and update the receiving systems.")

	// SSO (SAML) — only when present.
	if s != nil && s.SSO != nil {
		realm := orDash(s.SSO.Realm)
		items = append(items, fmt.Sprintf(
			"**SSO (SAML)** — recreate manually (DNS TXT domain verification + IdP-side SAML app). "+
				"Source: enforced=`%t`, realm=`%s`. Not automatable.%s",
			s.SSO.Enforced, realm, warningSuffix(m, "sso")))
	}

	// Audit-log streaming — only when configs present.
	if s != nil && len(s.AuditLogConfigs) > 0 {
		items = append(items, fmt.Sprintf(
			"**Audit-log streaming (%d config(s))** — the S3 ARN/region/bucket/endpoint point at the source AWS account, "+
				"so recreate each stream against destination-owned, environment-specific infrastructure.%s",
			len(s.AuditLogConfigs), warningSuffix(m, "audit-log", "audit_log")))
	}

	// OTel exporter header values — only when exporters present.
	if s != nil && len(s.OTelExporters) > 0 {
		items = append(items, fmt.Sprintf(
			"**OpenTelemetry exporter headers (%d exporter(s))** — header values are redacted by the server and cannot be replayed. "+
				"`sync` creates the exporters without headers; re-add the secret header values manually.",
			len(s.OTelExporters)))
	}

	// Danger flags — only when set, applied after validation.
	if s != nil && s.RequireContextGroupRestriction != nil && *s.RequireContextGroupRestriction {
		items = append(items, "**`require_context_group_restriction`** — danger flag. "+
			"Enable it on the destination only after group restrictions are validated, or contexts may be unusable.")
	}
	for _, p := range m.Projects {
		if p.Settings != nil && p.Settings.DropAllBuildRequests != nil && *p.Settings.DropAllBuildRequests {
			items = append(items, "**`drop_all_build_requests`** — danger flag set on at least one project. "+
				"Set it on the destination only after validation; until then it silently drops builds.")
			break
		}
	}

	// Org contacts — verify when present.
	if s != nil && s.Contacts != nil && (len(s.Contacts.Primary) > 0 || len(s.Contacts.Security) > 0) {
		items = append(items, "**Org technical & security contacts** — `sync` overwrites the destination lists; "+
			"verify the addresses after cutover.")
	}

	// CircleCI group definitions — only when captured. Context group-restriction
	// sync resolves destination groups BY NAME, so they must already exist there.
	if s != nil && len(s.Groups) > 0 {
		names := make([]string, 0, len(s.Groups))
		for _, g := range s.Groups {
			names = append(names, g.Name)
		}
		items = append(items, fmt.Sprintf(
			"**CircleCI groups** — recreate %d CircleCI group(s) in the destination org before/at cutover "+
				"so context group-restrictions resolve: %s. "+
				"Group membership is managed via your IdP/SSO and is not migrated.",
			len(s.Groups), joinNames(names)))
	}

	// App-destination repo connection — relevant whenever App pipeline definitions exist.
	if hasPipelineDefinitions(m) {
		items = append(items, "**Repository connections (App destinations)** — repos must already exist and be connected to "+
			"the destination CircleCI GitHub App, or project onboarding is skipped. Connect them before `sync --apply`.")
	}

	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

// writeDataLoss renders what does not transfer / changes during migration.
func writeDataLoss(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n### 4. Does not transfer / data loss\n\n")
	fmt.Fprintf(b, "- **Identifiers change.** Project, context, and pipeline UUIDs are reassigned by the destination; anything that hard-codes a source UUID must be updated.\n")
	fmt.Fprintf(b, "- **Captured secrets must be rotated.** Treat every value captured for migration as exposed and rotate it after cutover.\n")

	if isOAuthSource(m) {
		fmt.Fprintf(b, "- **OAuth → App has no equivalent for some settings.** Fork-PR builds, the OSS flag, and `pr_only_branch_overrides` do not exist on App destinations and are dropped.\n")
		fmt.Fprintf(b, "- **Multiple App pipeline definitions cannot collapse into one OAuth config.** Going the other direction loses that structure; plan config accordingly.\n")
	} else {
		fmt.Fprintf(b, "- **Cross-type moves lose settings.** OAuth→App drops fork-PR builds, the OSS flag, and `pr_only_branch_overrides`; multiple App pipeline definitions cannot collapse into a single OAuth config.\n")
	}
}

// writeExternalPins renders the reminder to repoint external integrations.
func writeExternalPins(b *strings.Builder) {
	fmt.Fprintf(b, "\n### 5. Update external pins\n\n")
	fmt.Fprintf(b, "After cutover, update everything that points at the old org to the new org's slugs/IDs:\n\n")
	fmt.Fprintf(b, "- Service catalogs / Backstage entries referencing the old project slugs.\n")
	fmt.Fprintf(b, "- Slack and other notification integrations.\n")
	fmt.Fprintf(b, "- Dashboards, status badges, and Insights links.\n")
	fmt.Fprintf(b, "- Branch-protection / required status-check integrations on the VCS side.\n")
	fmt.Fprintf(b, "- Documentation, READMEs, and bookmarks linking to the old org.\n")
}

// SaveMarkdown writes the audit document to path.
func SaveMarkdown(m *manifest.Manifest, path string) error {
	if err := os.WriteFile(path, []byte(Markdown(m)), 0o644); err != nil {
		return fmt.Errorf("writing audit report %s: %w", path, err)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
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

// isOAuthSource reports whether the source org is a GitHub OAuth org, inferred
// from the "gh/" slug prefix (App / GitLab orgs use "circleci/").
func isOAuthSource(m *manifest.Manifest) bool {
	return strings.HasPrefix(m.Source.Org.Slug, "gh/")
}
