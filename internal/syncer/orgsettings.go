package syncer

import (
	"fmt"
	"strings"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// OrgSettingsWriter is the destination org-settings API the syncer needs.
// It is a narrow interface over the methods defined in api/org/orgsettings.go.
type OrgSettingsWriter interface {
	UpdateFeatureFlags(vcsType, orgName string, flags map[string]bool) error
	SetOIDCClaims(orgID string, audience []string, ttl string) error
	CreateURLOrbAllowEntry(slugOrID, name, prefix, auth string) error
	PutPolicyBundle(ownerID string, policies map[string]string) error
	SetPolicyEnforcement(ownerID string, enabled bool) error
}

// dangerFlags are flags that are skipped by default during sync because
// writing them can freeze or break a destination org.
//   - drop_all_build_requests: stops ALL builds from being triggered.
//   - require_context_group_restriction: restricts context access; enabling in
//     a new org without the security groups in place can lock out pipelines.
var dangerFlags = map[string]bool{
	"drop_all_build_requests":           true,
	"require_context_group_restriction": true,
}

// SyncOrgSettings applies org-level settings from the manifest to the
// destination org. The destination org slug is taken from mapping.Org.To.
//
// Feature flags: written one-by-one (best-effort). The two "danger" flags
// (drop_all_build_requests, require_context_group_restriction) are skipped
// with a "manual" action explaining the risk.
//
// OIDC, URL-orb allow list, and config policies are applied when present.
// Per-item errors are recorded as actions with status "error" and do not
// cause a top-level error return (mirrors SyncContexts/SyncProjects patterns).
func (s *Syncer) SyncOrgSettings(m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	destOrgID, err := s.Org.ResolveOrgID(destSlug)
	if err != nil {
		return nil, fmt.Errorf("SyncOrgSettings: resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID
	s.logf("Syncing org settings to %s (id %s)%s", destSlug, destOrgID, dryRunSuffix(opts.Apply))

	if s.OrgSettings == nil {
		// No writer injected — nothing to do.
		return report, nil
	}

	src := m.Source.Org.Settings
	if src == nil {
		s.logf("  No org settings in manifest — nothing to sync")
		return report, nil
	}

	// Resolve the vcs/name pair from the destination slug for v1.1 flag writes.
	vcs, orgName := splitDestSlug(destSlug)

	s.syncFeatureFlags(report, src, vcs, orgName, opts)
	s.syncOIDCClaims(report, src, destOrgID, opts)
	s.syncURLOrbAllowList(report, src, destSlug, opts)
	s.syncPolicies(report, src, destOrgID, opts)
	s.reportAuditLogConfigs(report, src)
	s.reportSSO(report, src)

	return report, nil
}

// reportSSO records the captured SSO (SAML) state as a single "manual" action.
// SSO is never auto-applied: recreating it on the destination requires DNS TXT
// domain verification and IdP-side SAML app / iframe-origin setup, none of which
// is automatable, so we surface it for the operator and never write.
func (s *Syncer) reportSSO(report *Report, src *manifest.OrgSettings) {
	if src.SSO == nil {
		return
	}
	detail := fmt.Sprintf(
		"SSO is configured on the source org (enforced=%v, realm=%q) and must be recreated manually on the destination — it requires DNS TXT domain verification plus IdP-side SAML app / iframe-origin setup and cannot be auto-synced",
		src.SSO.Enforced, src.SSO.Realm,
	)
	report.add("org-settings", "sso", "manual", detail)
}

// reportAuditLogConfigs records each captured audit-log config as a "manual"
// action. These are never auto-applied: the S3 ARN/region/bucket/endpoint are
// environment-specific and point at the SOURCE org's AWS account, so POSTing the
// source values to the destination would stream audit logs to the wrong account.
func (s *Syncer) reportAuditLogConfigs(report *Report, src *manifest.OrgSettings) {
	for _, cfg := range src.AuditLogConfigs {
		target := "audit_log_config"
		if cfg.Purpose != "" {
			target += ":" + cfg.Purpose
		}
		bucket := cfg.Config.BucketName
		if cfg.Config.BucketPrefix != "" {
			bucket += "/" + cfg.Config.BucketPrefix
		}
		detail := fmt.Sprintf(
			"audit-log config (purpose=%q, target=%q, bucket=%q, region=%q, arn=%q, endpoint=%q) is environment-specific and must be recreated in the destination — its S3 ARN/region/bucket/endpoint point at the source org's AWS account and are not copied automatically",
			cfg.Purpose, cfg.TargetType, bucket, cfg.Config.Region, cfg.Config.ARN, cfg.Config.Endpoint,
		)
		report.add("org-settings", target, "manual", detail)
	}
}

// syncFeatureFlags writes each feature flag to the destination. Danger flags
// are skipped with a "manual" action regardless of Apply.
func (s *Syncer) syncFeatureFlags(report *Report, src *manifest.OrgSettings, vcs, orgName string, opts Options) {
	if len(src.FeatureFlags) == 0 {
		return
	}
	if vcs == "" || orgName == "" {
		report.add("org-settings", "feature_flags", "manual",
			"cannot write feature flags to a circleci/-type org (no vcs/name form available)")
		return
	}

	for flagKey, val := range src.FeatureFlags {
		target := "feature_flag:" + flagKey

		if dangerFlags[flagKey] {
			report.add("org-settings", target, "manual",
				fmt.Sprintf("flag %q skipped: writing this flag to a new org is unsafe (it can freeze or break pipelines). Set manually after validating the destination org is ready.", flagKey))
			continue
		}

		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would set feature flag %q = %v", flagKey, val))
			continue
		}

		if err := s.OrgSettings.UpdateFeatureFlags(vcs, orgName, map[string]bool{flagKey: val}); err != nil {
			report.add("org-settings", target, "error", err.Error())
			continue
		}
		report.add("org-settings", target, "set", fmt.Sprintf("set feature flag %q = %v", flagKey, val))
	}
}

// syncOIDCClaims writes the OIDC audience/TTL when present.
func (s *Syncer) syncOIDCClaims(report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if len(src.OIDCAudience) == 0 && src.OIDCTTL == "" {
		return
	}

	target := "oidc_claims"
	if !opts.Apply {
		report.add("org-settings", target, "set",
			fmt.Sprintf("would set OIDC audience=%v ttl=%q", src.OIDCAudience, src.OIDCTTL))
		return
	}

	if err := s.OrgSettings.SetOIDCClaims(destOrgID, src.OIDCAudience, src.OIDCTTL); err != nil {
		report.add("org-settings", target, "error", err.Error())
		return
	}
	report.add("org-settings", target, "set",
		fmt.Sprintf("set OIDC audience=%v ttl=%q", src.OIDCAudience, src.OIDCTTL))
}

// syncURLOrbAllowList adds each allow-list entry that is not already present.
func (s *Syncer) syncURLOrbAllowList(report *Report, src *manifest.OrgSettings, destSlug string, opts Options) {
	for _, entry := range src.URLOrbAllowList {
		target := "url_orb_allow_list:" + entry.Name

		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would add URL-orb allow-list entry %q (%s)", entry.Name, entry.Prefix))
			continue
		}

		if err := s.OrgSettings.CreateURLOrbAllowEntry(destSlug, entry.Name, entry.Prefix, entry.Auth); err != nil {
			report.add("org-settings", target, "error", err.Error())
			continue
		}
		report.add("org-settings", target, "set",
			fmt.Sprintf("added URL-orb allow-list entry %q (%s)", entry.Name, entry.Prefix))
	}
}

// syncPolicies writes the policy bundle and enforcement setting.
func (s *Syncer) syncPolicies(report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if len(src.ConfigPolicies) > 0 {
		target := "config_policies"
		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would write %d config polic(ies) (Scale plan required)", len(src.ConfigPolicies)))
		} else if err := s.OrgSettings.PutPolicyBundle(destOrgID, src.ConfigPolicies); err != nil {
			report.add("org-settings", target, "error",
				fmt.Sprintf("could not write config policies (Scale plan required): %v", err))
		} else {
			report.add("org-settings", target, "set",
				fmt.Sprintf("wrote %d config polic(ies)", len(src.ConfigPolicies)))
		}
	}

	if src.PolicyEnforcementEnabled != nil {
		target := "policy_enforcement"
		enabled := *src.PolicyEnforcementEnabled
		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would set policy enforcement enabled=%v", enabled))
		} else if err := s.OrgSettings.SetPolicyEnforcement(destOrgID, enabled); err != nil {
			report.add("org-settings", target, "error",
				fmt.Sprintf("could not set policy enforcement (Scale plan required): %v", err))
		} else {
			report.add("org-settings", target, "set",
				fmt.Sprintf("set policy enforcement enabled=%v", enabled))
		}
	}
}

// splitDestSlug extracts the (vcs, orgName) pair from a destination slug
// like "gh/acme" or "github/acme". Returns ("", "") for circleci/-type orgs.
func splitDestSlug(slug string) (vcs, orgName string) {
	parts := strings.SplitN(slug, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", ""
	}
	if parts[0] == "circleci" {
		return "", ""
	}
	// Map vcs prefix to the canonical v1.1 names: "gh" → "github", "bb" → "bitbucket".
	v := parts[0]
	switch v {
	case "gh":
		v = "github"
	case "bb":
		v = "bitbucket"
	}
	return v, parts[1]
}
