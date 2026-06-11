package report

import (
	"fmt"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

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
