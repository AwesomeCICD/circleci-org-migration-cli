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

	// Org settings — everything readable at the org level.
	writeOrgSettings(&b, m)

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
		if len(p.CheckoutKeys) > 0 {
			fmt.Fprintf(&b, "- Checkout keys: %d\n", len(p.CheckoutKeys))
		}
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
		if len(p.Webhooks) > 0 {
			fmt.Fprintf(&b, "- Webhooks: %d\n", len(p.Webhooks))
		}
		if len(p.Schedules) > 0 {
			fmt.Fprintf(&b, "- Schedules: %d\n", len(p.Schedules))
			for _, sched := range p.Schedules {
				if sched.ActorLogin != "" {
					fmt.Fprintf(&b, "  - `%s` (actor: `%s`)\n", sched.Name, sched.ActorLogin)
				}
			}
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

// writeOrgSettings renders the "## Org settings" section: every org-level
// setting we captured, so the report is a complete picture of the org. Empty
// subsections are skipped; namespaces/orbs are always noted (even when none).
func writeOrgSettings(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n## Org settings\n\n")
	s := m.Source.Org.Settings
	if s == nil {
		fmt.Fprintf(b, "_None captured._\n")
		return
	}

	// Feature flags (v1.1) — split enabled/disabled for scannability.
	if len(s.FeatureFlags) > 0 {
		var on, off []string
		for k, v := range s.FeatureFlags {
			if v {
				on = append(on, k)
			} else {
				off = append(off, k)
			}
		}
		sort.Strings(on)
		sort.Strings(off)
		fmt.Fprintf(b, "### Feature flags (%d)\n\n", len(s.FeatureFlags))
		if len(on) > 0 {
			fmt.Fprintf(b, "- Enabled: %s\n", joinNames(on))
		}
		if len(off) > 0 {
			fmt.Fprintf(b, "- Disabled: %s\n", joinNames(off))
		}
		fmt.Fprintf(b, "\n")
	}

	if r := s.StorageRetention; r != nil {
		fmt.Fprintf(b, "### Storage retention\n\n- Artifacts: %d day(s)\n- Workspaces: %d day(s)\n- Caches: %d day(s)\n", r.ArtifactDays, r.WorkspaceDays, r.CacheDays)
		if lim := s.StorageRetentionLimits; lim != nil {
			fmt.Fprintf(b, "- Plan limits — artifacts: %d–%d day(s), workspaces: %d–%d day(s), caches: %d–%d day(s)\n",
				lim.Artifact.Min, lim.Artifact.Max,
				lim.Workspace.Min, lim.Workspace.Max,
				lim.Cache.Min, lim.Cache.Max)
		}
		fmt.Fprintf(b, "\n")
	}

	if bgt := s.Budgets; bgt != nil && (bgt.OrgBudget != nil || len(bgt.ProjectBudgets) > 0) {
		fmt.Fprintf(b, "### Spend budgets\n\n")
		if bgt.OrgBudget != nil {
			fmt.Fprintf(b, "- Org budget: %d credits (enforcement: %s)\n", bgt.OrgBudget.Credits, orDash(bgt.OrgBudget.EnforcementType))
		}
		if len(bgt.ProjectBudgets) > 0 {
			fmt.Fprintf(b, "- Per-project budgets: %d (mapped to destination projects on sync)\n", len(bgt.ProjectBudgets))
		}
		fmt.Fprintf(b, "\n")
	}

	// Security toggles.
	var sec []string
	if s.BlockUnregisteredUsers != nil {
		sec = append(sec, fmt.Sprintf("- Prevent unregistered-user spend: `%t`", *s.BlockUnregisteredUsers))
	}
	if s.PolicyEnforcementEnabled != nil {
		sec = append(sec, fmt.Sprintf("- Config-policy enforcement: `%t`", *s.PolicyEnforcementEnabled))
	}
	if s.RequireContextGroupRestriction != nil {
		sec = append(sec, fmt.Sprintf("- Require context group restriction: `%t`", *s.RequireContextGroupRestriction))
	}
	if len(sec) > 0 {
		fmt.Fprintf(b, "### Security\n\n%s\n\n", strings.Join(sec, "\n"))
	}

	if len(s.OIDCAudience) > 0 || s.OIDCTTL != "" {
		fmt.Fprintf(b, "### OIDC custom claims\n\n- Audience: %s\n- TTL: `%s`\n\n", joinNames(s.OIDCAudience), orDash(s.OIDCTTL))
	}

	if len(s.URLOrbAllowList) > 0 {
		fmt.Fprintf(b, "### URL-orb allow list (%d)\n\n", len(s.URLOrbAllowList))
		for _, e := range s.URLOrbAllowList {
			fmt.Fprintf(b, "- `%s` → `%s` (auth: %s)\n", e.Name, e.Prefix, orDash(e.Auth))
		}
		fmt.Fprintf(b, "\n")
	}

	if len(s.ConfigPolicies) > 0 {
		names := make([]string, 0, len(s.ConfigPolicies))
		for n := range s.ConfigPolicies {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintf(b, "### Config policies (%d)\n\n- %s\n\n", len(names), joinNames(names))
	}

	if c := s.Contacts; c != nil && (len(c.Primary) > 0 || len(c.Security) > 0) {
		fmt.Fprintf(b, "### Contacts\n\n")
		if len(c.Primary) > 0 {
			fmt.Fprintf(b, "- Technical: %s\n", joinNames(c.Primary))
		}
		if len(c.Security) > 0 {
			fmt.Fprintf(b, "- Security: %s\n", joinNames(c.Security))
		}
		fmt.Fprintf(b, "\n")
	}

	if len(s.OTelExporters) > 0 {
		fmt.Fprintf(b, "### OpenTelemetry exporters (%d)\n\n", len(s.OTelExporters))
		for _, e := range s.OTelExporters {
			fmt.Fprintf(b, "- `%s` (%s)\n", orDash(e.Endpoint), orDash(e.Protocol))
		}
		fmt.Fprintf(b, "\n")
	}

	if rt := s.ReleaseTracker; rt != nil && rt.InconclusiveReleaseTTL != "" {
		fmt.Fprintf(b, "### Release tracker\n\n- Inconclusive release TTL: `%s`\n\n", rt.InconclusiveReleaseTTL)
	}

	if eh := s.EnvironmentHierarchy; eh != nil {
		fmt.Fprintf(b, "### Environment hierarchy: `%s`\n\n", orDash(eh.Name))
		for _, lvl := range eh.Levels {
			fmt.Fprintf(b, "  %d. %s\n", lvl.Position, orDash(lvl.IntegrationName))
		}
		fmt.Fprintf(b, "\n_Recreate manually — deploy-integration IDs are org-specific._\n\n")
	}

	if len(s.AuditLogConfigs) > 0 {
		fmt.Fprintf(b, "### Audit-log streaming (%d)\n\n- Recreate manually (the S3 target is specific to the source AWS account).\n\n", len(s.AuditLogConfigs))
	}

	// Namespaces / orbs — ALWAYS shown (even when none) so it's explicit.
	fmt.Fprintf(b, "### Namespaces & orbs\n\n")
	if s.OrbNamespace != "" {
		fmt.Fprintf(b, "- Source orb namespace: `%s`\n", s.OrbNamespace)
	}
	if len(s.Orbs) == 0 {
		fmt.Fprintf(b, "- No published orbs / claimed namespace for this org.\n\n")
	} else {
		fmt.Fprintf(b, "\n%d published orb(s) — orb source YAML is not exportable via REST API (GraphQL only); republish each under the destination namespace manually:\n\n", len(s.Orbs))
		for _, o := range s.Orbs {
			vis := "public"
			if o.IsPrivate {
				vis = "private"
			}
			fmt.Fprintf(b, "- `%s` @ `%s` (%s)\n", o.OrbName, orDash(o.LatestVersionNumber), vis)
		}
		fmt.Fprintf(b, "\n")
	}
}

// writeCIAMSection renders the CIAM roles and groups section of the report.
// Only shown for circleci-type source orgs where CIAM data was captured.
func writeCIAMSection(b *strings.Builder, m *manifest.Manifest) {
	if m.CIAM == nil {
		return
	}
	fmt.Fprintf(b, "\n## CIAM roles and groups\n\n")
	fmt.Fprintf(b, "_Only present for standalone (`circleci`-type) orgs. Users are identified by email; groups by name._\n\n")
	fmt.Fprintf(b, "Reference: [Manage roles and permissions](https://circleci.com/docs/guides/permissions-authentication/manage-roles-and-permissions/) | [Manage groups](https://circleci.com/docs/guides/permissions-authentication/manage-groups/)\n\n")

	ciam := m.CIAM

	if len(ciam.OrgRoles) > 0 {
		fmt.Fprintf(b, "### Org-level roles (%d)\n\n", len(ciam.OrgRoles))
		fmt.Fprintf(b, "| Email | Username | Role |\n|---|---|---|\n")
		for _, r := range ciam.OrgRoles {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", r.Email, orDash(r.Username), r.Role)
		}
		fmt.Fprintf(b, "\n")
	}

	if len(ciam.Groups) > 0 {
		fmt.Fprintf(b, "### CIAM groups (%d)\n\n", len(ciam.Groups))
		fmt.Fprintf(b, "| Name | Description | Member count |\n|---|---|---:|\n")
		for _, g := range ciam.Groups {
			fmt.Fprintf(b, "| `%s` | %s | %d |\n", g.Name, orDash(g.Description), len(g.MemberEmails))
		}
		fmt.Fprintf(b, "\n_Note: group membership is not available from the groups list API. Membership must be verified and recreated manually on the destination._\n\n")
	}

	if len(ciam.ProjectUserGrants) > 0 {
		fmt.Fprintf(b, "### Per-project user role grants (%d)\n\n", len(ciam.ProjectUserGrants))
		fmt.Fprintf(b, "| Project | Email | Role |\n|---|---|---|\n")
		for _, g := range ciam.ProjectUserGrants {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", g.ProjectName, g.Email, g.Role)
		}
		fmt.Fprintf(b, "\n")
	}

	if len(ciam.ProjectGroupGrants) > 0 {
		fmt.Fprintf(b, "### Per-project group role grants (%d)\n\n", len(ciam.ProjectGroupGrants))
		fmt.Fprintf(b, "| Project | Group | Role |\n|---|---|---|\n")
		for _, g := range ciam.ProjectGroupGrants {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", g.ProjectName, g.GroupName, g.Role)
		}
		fmt.Fprintf(b, "\n")
	}
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
	fmt.Fprintf(b, "- CIAM roles, groups, and per-project role grants (standalone circleci-type orgs only; users matched by email).\n")
}

// hasNonDefaultGroupRestrictions reports whether any context in the manifest
// has at least one group restriction that is NOT the default "All members"
// group (type=="group", value!=orgID).  These are real access restrictions
// that must be re-applied manually because:
//   - Group restrictions are only supported on GitHub OAuth ("gh/…") orgs.
//   - They cannot be created via the API on standalone ("circleci/…") or
//     Bitbucket orgs (fails with "This is only supported for OAuth orgs.").
//   - VCS team IDs are org-specific and do not map across orgs.
func hasNonDefaultGroupRestrictions(m *manifest.Manifest) bool {
	orgID := m.Source.Org.ID
	for _, c := range m.Contexts {
		for _, r := range c.Restrictions {
			if r.Type == "group" && r.Value != orgID {
				return true
			}
		}
	}
	return false
}

// writeManualSteps renders the manual steps required to finish the migration.
// The list is data-driven: an item is included only when the manifest provides
// the corresponding signal, with a small always-include baseline (secret values
// and key regeneration always apply). Warning messages are pulled in where the
// export recorded them. Every item names resources by their human-readable name
// (never a raw UUID), states what the source org had, and ends with a clickable
// settings URL so operators can act without hunting.
func writeManualSteps(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n### 3. Manual steps required\n\n")

	var items []string
	s := m.Source.Org.Settings
	host := m.Source.Host
	orgSlug := m.Source.Org.Slug

	// Always: secret values must be captured and re-applied.
	items = append(items, "**Context & project secret values** — CircleCI never exports env-var values. "+
		"Capture them with the in-pipeline `secrets` step in the source org, supply the bundle to `sync`, then rotate them after cutover."+
		warningSuffix(m, "context_values_excluded", "project_values_excluded"))

	// Always: checkout / deploy keys — bound to the source org's VCS connection.
	items = append(items, "**Checkout / deploy keys** — deploy and checkout keys are bound to the "+
		"source org's VCS connection (public key registered on the VCS side, private key stored in CircleCI). "+
		"The destination org has a fresh OAuth/VCS connection and new project UUIDs, so these keys do NOT transfer. "+
		"OAuth projects auto-provision a new deploy key when followed; GitHub App projects use HTTPS checkout and need no deploy key. "+
		"Verify checkout works on the destination after sync. "+
		"Ref: https://circleci.com/docs/guides/security/rotate-project-ssh-keys/#github-projects")

	// Additional SSH keys — per-project details when at least one project captured SSH key metadata.
	// IMPORTANT: additional SSH keys ARE migrated as-is (same private key) via `secrets capture --ssh-keys`.
	// Do NOT tell the user to regenerate them — the remote already authorises the same public key.
	if hasAdditionalSSHKeys(m) {
		var perProject []string
		for _, p := range m.Projects {
			if len(p.SSHKeys) == 0 {
				continue
			}
			name := projectDisplayName(p)
			hostnames := sshKeyHostnames(p.SSHKeys)
			settingsURL := projectSettingsURL(host, p.Slug, "ssh")
			perProject = append(perProject, fmt.Sprintf(
				"Project **%s** had %d additional SSH key(s) (hostname(s): %s) → %s (SSH Keys tab)",
				name, len(p.SSHKeys), hostnames, settingsURL))
		}
		detail := strings.Join(perProject, "; ")
		if detail == "" {
			detail = "(see manifest for details)"
		}
		items = append(items, "**Additional SSH keys** — "+
			"additional SSH keys are migrated as-is by the ssh-key extraction (`secrets capture --ssh-keys`); "+
			"the same private key keeps working against the remote that already authorizes its public key — no remote change needed. "+
			"Only add a new public key to the remote if you choose to generate fresh keys on the destination. "+
			detail+"."+
			warningSuffix(m, "ssh_keys_private_excluded"))
	}

	// Webhook signing secrets — per-project details when webhooks were captured.
	if hasWebhooks(m) {
		var perProject []string
		for _, p := range m.Projects {
			if len(p.Webhooks) == 0 {
				continue
			}
			name := projectDisplayName(p)
			settingsURL := projectSettingsURL(host, p.Slug, "webhooks")
			perProject = append(perProject, fmt.Sprintf(
				"Project **%s** had %d webhook(s) → %s (Webhooks tab)",
				name, len(p.Webhooks), settingsURL))
		}
		detail := strings.Join(perProject, "; ")
		if detail == "" {
			detail = "(see manifest for details)"
		}
		items = append(items, "**Webhook signing secrets** — sync recreates each webhook with a NEW signing secret. "+
			"HMAC-validating receivers will reject deliveries until BOTH the destination webhook secret and the "+
			"receiver's stored secret are realigned; update your receiver's stored secret and test delivery after cutover. "+
			"Ref: https://circleci.com/docs/guides/integration/outbound-webhooks/#validate-webhooks "+
			detail+"."+
			warningSuffix(m, "webhook_signing_secret_excluded"))
	}

	// Runner agent tokens — only when runner resource classes were captured.
	if len(m.RunnerResourceClasses) > 0 {
		items = append(items, fmt.Sprintf(
			"**Runner agent tokens (%d resource class(es))** — agent registration tokens are never retrievable via API. "+
				"After recreating each resource class on the destination namespace (`%s`), issue new tokens with "+
				"`circleci runner token create <resource-class> \"<nickname>\"`, add each token to `launch-agent-config.yml`, "+
				"and restart the launch-agent. "+
				"Ref: https://support.circleci.com/hc/en-us/articles/11816211460891 "+
				warningSuffix(m, "runner_agent_token_excluded"),
			len(m.RunnerResourceClasses), orDash(m.RunnerNamespace)))
	}

	// Org orbs — only when orbs were captured.
	if s != nil && len(s.Orbs) > 0 {
		ns := orDash(s.OrbNamespace)
		orbsURL := orgSettingsURL(host, orgSlug, "orbs")
		items = append(items, fmt.Sprintf(
			"**Org orbs (%d orb(s), source namespace `%s`)** — orb source YAML is not exportable via REST API (GraphQL only) "+
				"and a namespace lives in one org at a time (transfer is a one-way Support ticket after cutover). "+
				"During the namespace-transfer overlap, consuming repos can keep working by inlining the published orb source "+
				"(see `orb inline` command — https://circleci.com/docs/orbs/create-an-inline-orb/). "+
				"Republish each orb under the destination namespace manually using `circleci orb publish` after cutover. "+
				"Ref: https://support.circleci.com/hc/en-us/articles/21518826780827-Transferring-and-Renaming-Namespaces "+
				"→ %s (Orbs tab)"+
				warningSuffix(m, "orbs_require_republish"),
			len(s.Orbs), ns, orbsURL))
	}

	// OIDC cloud-provider trust — always add (distinct from the OIDC claim sync we do).
	// The destination org has a new UUID → new OIDC issuer URL → cloud IAM trust must be updated.
	items = append(items, "**OIDC cloud-provider trust** — the destination org has a NEW UUID, so its OIDC issuer is "+
		"`https://oidc.circleci.com/org/<new-uuid>` and the default audience also changes. "+
		"Any AWS IAM OIDC provider, GCP workload-identity pool, or Vault JWT auth mount that trusts the source org's issuer/audience "+
		"must be reconfigured to trust the destination org, or jobs will fail with AssumeRole / AccessDenied errors. "+
		"Ref: https://circleci.com/docs/guides/permissions-authentication/openid-connect-tokens/#format-of-the-openid-connect-id-token")

	// VCS branch-protection / required-status-check repoint — always add.
	items = append(items, "**VCS branch-protection / required status checks** — "+
		"when a project is recreated on the destination its CircleCI check name changes. "+
		"Update any branch-protection rules or required-status-check configurations on your VCS repositories "+
		"to reference the new check name, or pull requests will be blocked after cutover.")

	// Org admins, email-domain restriction, GHES endpoint — not readable via our APIs.
	items = append(items, "**Org-level settings not readable via API (manual checklist)** — "+
		"the following items cannot be captured by the export and must be configured manually on the destination: "+
		"(1) org admin role assignments; "+
		"(2) email-domain sign-up restrictions (if enabled); "+
		"(3) GitHub Enterprise Server endpoint configuration (if applicable).")

	// Budget enforcement=block — per-budget details when at least one block budget was captured.
	if hasBudgetEnforcementBlock(m) {
		var budgetDetails []string
		if s != nil && s.Budgets != nil {
			if s.Budgets.OrgBudget != nil && s.Budgets.OrgBudget.EnforcementType == "block" {
				budgetDetails = append(budgetDetails, fmt.Sprintf(
					"org-level budget (%d credits, enforcement=block)",
					s.Budgets.OrgBudget.Credits))
			}
			for _, pb := range s.Budgets.ProjectBudgets {
				if pb.EnforcementType != "block" {
					continue
				}
				projName := projectNameByID(m, pb.ProjectID)
				budgetDetails = append(budgetDetails, fmt.Sprintf(
					"project **%s** (%d credits, enforcement=block)",
					projName, pb.Credits))
			}
		}
		// Budgets are managed under Plan → Credit Usage, not org settings.
		planURL := appHost(host) + "/plan/" + orgSlug + "/credit-usage"
		detail := ""
		if len(budgetDetails) > 0 {
			detail = " Items: " + strings.Join(budgetDetails, "; ") + "."
		}
		items = append(items, "**Budget enforcement mode** — one or more budgets have `enforcement_type=block`; "+
			"the PUT budget endpoint only accepts credits and cannot set enforcement mode — "+
			"re-apply block enforcement manually on the destination after sync."+detail+
			" → "+planURL+" (Plan → Credit Usage tab)"+
			warningSuffix(m, "budget_enforcement_block_not_transferred"))
	}

	// Group restrictions — per-context details when non-default group restrictions are present.
	//
	// Org-type matrix for context restriction_type:
	//   project    — all org types (GitHub OAuth, standalone/circleci, Bitbucket)
	//   expression — all org types
	//   group      — GitHub OAuth ("gh/…") ONLY; API call fails on standalone/Bitbucket
	//
	// Because group restrictions are org-type-specific and their VCS team IDs
	// are not portable across orgs, they are NEVER automatically removed or
	// recreated by this tool.  They must always be re-applied manually on the
	// destination org.
	if hasNonDefaultGroupRestrictions(m) {
		orgID := m.Source.Org.ID
		var perContext []string
		for _, c := range m.Contexts {
			count := 0
			for _, r := range c.Restrictions {
				if r.Type == "group" && r.Value != orgID {
					count++
				}
			}
			if count == 0 {
				continue
			}
			contextURL := orgSettingsURL(host, orgSlug, "contexts")
			perContext = append(perContext, fmt.Sprintf(
				"Context **%s** had %d group restriction(s) → %s (Contexts tab)",
				c.Name, count, contextURL))
		}
		detail := strings.Join(perContext, "; ")
		if detail == "" {
			detail = "(see manifest for details)"
		}
		items = append(items, "**Context group restrictions (manual)** — one or more contexts have "+
			"`group`-type restrictions. Group restrictions are only supported on GitHub OAuth orgs "+
			"(`gh/…`); they cannot be created via the API on standalone (`circleci/…`) or Bitbucket orgs. "+
			"VCS team IDs embedded in group restrictions are org-specific and do not map across orgs. "+
			"Re-apply group restrictions manually on the destination after migration. "+
			detail+"."+
			warningSuffix(m, "group_restriction"))
	}

	// SSO (SAML) — only when present.
	if s != nil && s.SSO != nil {
		realm := orDash(s.SSO.Realm)
		ssoURL := orgSettingsURL(host, orgSlug, "single-sign-on")
		items = append(items, fmt.Sprintf(
			"**SSO (SAML)** — recreate manually (DNS TXT domain verification + IdP-side SAML app). "+
				"Source: enforced=`%t`, realm=`%s`. Not automatable. → %s (Single Sign-On tab)%s",
			s.SSO.Enforced, realm, ssoURL, warningSuffix(m, "sso")))
	}

	// Audit-log streaming — only when configs present.
	if s != nil && len(s.AuditLogConfigs) > 0 {
		securityURL := orgSettingsURL(host, orgSlug, "security")
		items = append(items, fmt.Sprintf(
			"**Audit-log streaming (%d config(s))** — the S3 ARN/region/bucket/endpoint point at the source AWS account, "+
				"so recreate each stream against destination-owned, environment-specific infrastructure. "+
				"→ %s (Security tab)%s",
			len(s.AuditLogConfigs), securityURL, warningSuffix(m, "audit-log", "audit_log")))
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

	// Project API tokens — per-project details when tokens were captured.
	// Token values are not retrievable after creation, so each token must be
	// recreated on the destination project and every consumer repointed.
	if hasAPITokens(m) {
		var perProject []string
		for _, p := range m.Projects {
			if len(p.APITokens) == 0 {
				continue
			}
			name := projectDisplayName(p)
			settingsURL := projectSettingsURL(host, p.Slug, "api")
			labels := make([]string, 0, len(p.APITokens))
			for _, t := range p.APITokens {
				labels = append(labels, fmt.Sprintf("%s (%s)", t.Label, t.Scope))
			}
			perProject = append(perProject, fmt.Sprintf(
				"Project **%s** had %d token(s): %s → %s (API Permissions tab)",
				name, len(p.APITokens), strings.Join(labels, ", "), settingsURL))
		}
		detail := strings.Join(perProject, "; ")
		if detail == "" {
			detail = "(see manifest for details)"
		}
		items = append(items, "**Project API tokens (values not recoverable)** — "+
			"project API token values are returned only once at creation time and cannot be retrieved afterwards. "+
			"Recreate each token on the destination project (Project Settings → API Permissions) and repoint "+
			"every consumer to the new token value. "+
			"See https://circleci.com/docs/guides/toolkit/managing-api-tokens/ for details. "+
			detail+"."+
			warningSuffix(m, "api_tokens_values_excluded"))
	}

	// App-destination repo connection — relevant whenever App pipeline definitions exist.
	if hasPipelineDefinitions(m) {
		items = append(items, "**Repository connections (App destinations)** — repos must already exist and be connected to "+
			"the destination CircleCI GitHub App, or project onboarding is skipped. Connect them before `sync --apply`.")
	}

	// CIAM roles and groups — only for circleci-type orgs where CIAM was captured.
	if m.CIAM != nil {
		rolesURL := "https://circleci.com/docs/guides/permissions-authentication/manage-roles-and-permissions/"
		groupsURL := "https://circleci.com/docs/guides/permissions-authentication/manage-groups/"
		items = append(items, fmt.Sprintf(
			"**CIAM user invitations (standalone orgs)** — `sync` applies org and project roles only for "+
				"users already present in the destination org (matched by email). Users not yet in the "+
				"destination org must be **invited first** before their roles can be assigned — "+
				"there is no bulk-invite API for circleci-type orgs. "+
				"After inviting users, re-run `sync` to apply their roles. "+
				"Refs: [Manage roles](%s) | [Manage groups](%s)",
			rolesURL, groupsURL))

		if len(m.CIAM.Groups) > 0 {
			groupNames := make([]string, 0, len(m.CIAM.Groups))
			for _, g := range m.CIAM.Groups {
				groupNames = append(groupNames, g.Name)
			}
			items = append(items, fmt.Sprintf(
				"**CIAM groups (%d group(s))** — `sync` creates groups by name; group **membership** "+
					"is not available from the groups list API and must be verified and set manually: %s. "+
					"→ [Manage groups](%s)",
				len(m.CIAM.Groups), joinNames(groupNames), groupsURL))
		}
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
	fmt.Fprintf(b, "- **Build/workflow/Insights/flaky-test history does not transfer.** Capture baseline screenshots of Insights dashboards and flaky-test reports before cutover; historical data remains only in the source org.\n")
	fmt.Fprintf(b, "- **Plan/billing tier is not migrated.** Ensure the destination org's plan/credit tier is set to the correct level in the destination org settings UI before enabling builds.\n")

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
	fmt.Fprintf(b, "- Branch-protection / required status-check rules on the VCS side (check names change when projects are recreated).\n")
	fmt.Fprintf(b, "- Documentation, READMEs, and bookmarks linking to the old org.\n")
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
