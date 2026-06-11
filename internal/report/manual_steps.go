package report

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// writeManualSteps renders the manual steps required to finish the migration.
// The list is data-driven: an item is included only when the manifest provides
// the corresponding signal, with a small always-include baseline (secret values
// and key regeneration always apply). Warning messages are pulled in where the
// export recorded them. Every item names resources by their human-readable name
// (never a raw UUID), states what the source org had, and ends with a clickable
// settings URL so operators can act without hunting.
func writeManualSteps(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n### 3. Manual steps required\n\n")
	fmt.Fprintf(b, "> **Automatable items** — context and project env-var *values* and additional SSH keys can be captured by the CLI: `circleci-migrate secrets capture` (add `--ssh-keys` for SSH keys), then `sync --secrets secrets.json --apply`. Items below labelled **[Automatable]** do not need manual intervention when you use those commands. All other items require manual action.\n\n")

	var items []string
	s := m.Source.Org.Settings
	host := m.Source.Host
	orgSlug := m.Source.Org.Slug

	// Always: secret values must be captured and re-applied.
	items = append(items, "**[Automatable] Context & project secret values** — CircleCI never exports env-var values. "+
		"Capture them with `circleci-migrate secrets capture` (in-pipeline or local), supply the bundle to `sync --secrets`, then rotate them after cutover. "+
		"Automatable: `circleci-migrate secrets capture`, then `sync --secrets secrets.json --apply`."+
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
		items = append(items, "**[Automatable] Additional SSH keys** — "+
			"additional SSH keys are migrated as-is by the ssh-key extraction (`secrets capture --ssh-keys`); "+
			"the same private key keeps working against the remote that already authorizes its public key — no remote change needed. "+
			"Only add a new public key to the remote if you choose to generate fresh keys on the destination. "+
			"Automatable: `circleci-migrate secrets capture --ssh-keys`, then `sync --secrets secrets.json --apply`. "+
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
			"**CIAM users & org roles (standalone orgs)** — `sync --apply` sets **org-level** role grants, "+
				"but only for users already present in the destination org (matched by email, then username). "+
				"Users not yet in the destination must be **invited first** (there is no bulk-invite API for "+
				"circleci-type orgs); after inviting, re-run `sync` to apply their org roles. "+
				"Refs: [Manage roles](%s) | [Manage groups](%s)",
			rolesURL, groupsURL))

		if len(m.CIAM.ProjectUserGrants) > 0 || len(m.CIAM.ProjectGroupGrants) > 0 {
			items = append(items, fmt.Sprintf(
				"**CIAM per-project role grants (%d)** — `sync` does **not** apply project-level role grants "+
					"automatically: the destination project UUID is assigned on creation and is not reliably "+
					"mappable from the source, so they must be recreated manually on each destination project "+
					"(see the *CIAM roles and groups* section above for the full list). → [Manage roles](%s)",
				len(m.CIAM.ProjectUserGrants)+len(m.CIAM.ProjectGroupGrants), rolesURL))
		}

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
