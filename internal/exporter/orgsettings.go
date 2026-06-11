package exporter

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// exportOrgSettings fills m.Source.Org.Settings with all readable org-level
// settings. Every sub-read is best-effort: on error a manifest warning is
// added and the field is left empty. App orgs (circleci/<uuid>) will 404 on
// the v1.1 feature-flags endpoint — that is normal and treated as empty.
func (e *Exporter) exportOrgSettings(ctx context.Context, m *manifest.Manifest, o *org.Organization, orgSlug string) {
	s := &manifest.OrgSettings{}
	hasAny := false

	// Feature flags (v1.1). Works for GitHub OAuth orgs (gh/<org>) AND for
	// GitHub-App / standalone orgs, whose slug is "circleci/<uuid>" — the v1.1
	// settings endpoint accepts vcs="circleci", name=<uuid>. (Previously
	// circleci-type orgs were skipped entirely, capturing zero flags.)
	vcs, name, ok := splitOrgSlug(orgSlug, o.VCSType)
	if !ok {
		if parts := strings.SplitN(orgSlug, "/", 2); len(parts) == 2 && parts[0] == "circleci" && parts[1] != "" {
			vcs, name, ok = "circleci", parts[1], true
		}
	}
	if ok {
		if flags, ferr := e.Org.GetFeatureFlags(ctx, vcs, name); ferr != nil {
			m.AddWarning("org", "feature_flags_unreadable", fmt.Sprintf("could not read feature flags: %v", ferr))
		} else if len(flags) > 0 {
			s.FeatureFlags = flags
			// Convenience copy of the context-group-restriction flag.
			if v, present := flags["require_context_group_restriction"]; present {
				v := v
				s.RequireContextGroupRestriction = &v
			}
			hasAny = true
		}

		// Legacy RequireContextGroupRestriction via the old GetOrgSettings path
		// (belt-and-suspenders; covers orgs where GetFeatureFlags returned empty).
		if s.RequireContextGroupRestriction == nil {
			if os, serr := e.Org.GetOrgSettings(ctx, vcs, name); serr == nil && os != nil && os.RequireContextGroupRestriction != nil {
				s.RequireContextGroupRestriction = os.RequireContextGroupRestriction
				hasAny = true
			}
		}
	}

	// OIDC custom claims (v2; keyed by org UUID).
	if o.ID != "" {
		if audience, ttl, oerr := e.Org.GetOIDCClaims(ctx, o.ID); oerr != nil {
			m.AddWarning("org", "oidc_claims_unreadable", fmt.Sprintf("could not read OIDC claims: %v", oerr))
		} else if len(audience) > 0 || ttl != "" {
			s.OIDCAudience = audience
			s.OIDCTTL = ttl
			hasAny = true
		}
	}

	// URL-orb allow list (v2; keyed by slug-or-id).
	if urlList, uerr := e.Org.GetURLOrbAllowList(ctx, orgSlug); uerr != nil {
		m.AddWarning("org", "url_orb_allow_list_unreadable", fmt.Sprintf("could not read URL-orb allow list: %v", uerr))
	} else if len(urlList) > 0 {
		for _, entry := range urlList {
			s.URLOrbAllowList = append(s.URLOrbAllowList, manifest.URLOrbAllowEntry{
				Name:   entry.Name,
				Prefix: entry.Prefix,
				Auth:   redactURLOrbAuth(entry.Auth),
			})
		}
		hasAny = true
	}

	// Config policies (v2; Scale plan only — 404 / 403 treated as empty).
	if o.ID != "" {
		if bundle, perr := e.Org.GetPolicyBundle(ctx, o.ID); perr != nil {
			m.AddWarning("org", "policy_bundle_unreadable", fmt.Sprintf("could not read config policies (Scale plan required): %v", perr))
		} else if len(bundle) > 0 {
			s.ConfigPolicies = bundle
			hasAny = true
		}

		if enabled, eerr := e.Org.GetPolicyEnforcement(ctx, o.ID); eerr != nil {
			m.AddWarning("org", "policy_enforcement_unreadable", fmt.Sprintf("could not read policy enforcement setting: %v", eerr))
		} else {
			s.PolicyEnforcementEnabled = &enabled
			hasAny = true
		}

		// Audit-log streaming configs (v2; org-scoped). Captured for the record
		// only — never auto-synced (their S3 ARN/region/bucket/endpoint are
		// environment-specific to the source org's AWS account).
		if configs, aerr := e.Org.GetAuditLogConfigs(ctx, o.ID); aerr != nil {
			m.AddWarning("org", "audit_log_configs_unreadable", fmt.Sprintf("could not read audit-log configs: %v", aerr))
		} else if len(configs) > 0 {
			for _, cfg := range configs {
				s.AuditLogConfigs = append(s.AuditLogConfigs, manifest.AuditLogConfig{
					ID:         cfg.ID,
					Purpose:    cfg.Purpose,
					TargetType: cfg.TargetType,
					IsDisabled: cfg.IsDisabled,
					Config: manifest.AuditLogTarget{
						ARN:          cfg.Config.ARN,
						Region:       cfg.Config.Region,
						BucketName:   cfg.Config.BucketName,
						BucketPrefix: cfg.Config.BucketPrefix,
						Endpoint:     cfg.Config.Endpoint,
					},
				})
			}
			hasAny = true
		}

		// SSO (SAML): best-effort, reference-only capture. SSO cannot be
		// auto-synced (recreation needs DNS domain verification + IdP setup), so
		// it is recorded for the operator and surfaced as a manual sync action.
		if e.exportSSO(ctx, m, o.ID, s) {
			hasAny = true
		}

		// OTel exporters (EXPERIMENTAL; up to 5 per org). Header values may
		// contain auth tokens (e.g. "Authorization: Bearer <token>") and are
		// redacted client-side before being written to the manifest. Key names
		// are preserved so the operator knows which headers were configured.
		if exporters, oerr := e.Org.GetOTelExporters(ctx, o.ID); oerr != nil {
			m.AddWarning("org", "otel_exporters_unreadable", fmt.Sprintf("could not read OTel exporters: %v", oerr))
		} else if len(exporters) > 0 {
			for _, ex := range exporters {
				redactedHeaders, redactedHdrKeys := redactOTelHeaders(ex.Headers)
				me := manifest.OTelExporter{
					Endpoint: ex.Endpoint,
					Protocol: ex.Protocol,
					Insecure: ex.Insecure,
					Headers:  redactedHeaders,
				}
				s.OTelExporters = append(s.OTelExporters, me)
				if len(redactedHdrKeys) > 0 {
					m.AddWarning("org", "otel_header_redacted", fmt.Sprintf(
						"redacted %d OTel exporter header value(s) for %q (header names preserved): %s",
						len(redactedHdrKeys), ex.Endpoint, strings.Join(redactedHdrKeys, ", ")))
				}
			}
			hasAny = true
		}

		// Org contacts (primary/security email lists).
		if primary, security, cerr := e.Org.GetContacts(ctx, o.ID); cerr != nil {
			m.AddWarning("org", "contacts_unreadable", fmt.Sprintf("could not read org contacts: %v", cerr))
		} else if len(primary) > 0 || len(security) > 0 {
			s.Contacts = &manifest.OrgContacts{Primary: primary, Security: security}
			hasAny = true
		}

		// Group definitions (names/IDs only). Captured so the cutover runbook can
		// tell the operator which groups to recreate in the destination org —
		// context group-restriction sync resolves destination groups by name. The
		// default "All members" group (ID == org ID) is auto-created on every org,
		// so it is excluded. Group MEMBERSHIP is never captured (managed via IdP).
		if groups, gerr := e.Org.ListGroups(ctx, o.ID); gerr != nil {
			m.AddWarning("org", "groups_unreadable", fmt.Sprintf("could not read org groups: %v", gerr))
		} else {
			var captured []manifest.OrgGroup
			for _, g := range groups {
				if g.ID == o.ID {
					// "All members" default group — auto-created everywhere; skip.
					continue
				}
				captured = append(captured, manifest.OrgGroup{ID: g.ID, Name: g.Name})
			}
			if len(captured) > 0 {
				s.Groups = captured
				hasAny = true
			}
		}

		// Storage-retention controls (best-effort; on error warn and continue).
		clog.Debugf("GetStorageRetention org_id=%s", o.ID)
		if sr, serr := e.Org.GetStorageRetention(ctx, o.ID); serr != nil {
			m.AddWarning("org", "retention_unreadable",
				fmt.Sprintf("could not read storage-retention controls: %v", serr))
		} else if sr != nil {
			c := sr.Controls
			s.StorageRetention = &manifest.StorageRetentionControls{
				CacheDays:     c.CacheDays,
				WorkspaceDays: c.WorkspaceDays,
				ArtifactDays:  c.ArtifactDays,
			}
			// Also capture plan limits so operators know the destination plan's
			// bounds before applying sync. Zero-value bounds (unset by server) are
			// not stored to avoid misleading output.
			l := sr.Limits
			if l.Cache.Max > 0 || l.Workspace.Max > 0 || l.Artifact.Max > 0 {
				s.StorageRetentionLimits = &manifest.StorageRetentionLimits{
					Cache:     manifest.StorageRetentionBound{Min: l.Cache.Min, Max: l.Cache.Max},
					Workspace: manifest.StorageRetentionBound{Min: l.Workspace.Min, Max: l.Workspace.Max},
					Artifact:  manifest.StorageRetentionBound{Min: l.Artifact.Min, Max: l.Artifact.Max},
				}
			}
			hasAny = true
			clog.Debugf("storage retention: cache=%d workspace=%d artifact=%d",
				c.CacheDays, c.WorkspaceDays, c.ArtifactDays)
		}

		// Spend budgets (best-effort; on error warn and continue).
		clog.Debugf("GetBudgets org_id=%s", o.ID)
		if budgets, berr := e.Org.GetBudgets(ctx, o.ID); berr != nil {
			m.AddWarning("org", "budgets_unreadable",
				fmt.Sprintf("could not read spend budgets: %v", berr))
		} else if len(budgets) > 0 {
			ob := &manifest.OrgBudgets{}
			for i := range budgets {
				b := &budgets[i]
				entry := manifest.BudgetEntry{
					Credits:         b.Credits,
					BudgetID:        b.BudgetID,
					EnforcementType: b.EnforcementType,
					ProjectID:       b.ProjectID,
				}
				if b.ProjectID == nil {
					ob.OrgBudget = &entry
				} else {
					ob.ProjectBudgets = append(ob.ProjectBudgets, entry)
				}
				// Warn when enforcement=block: the PUT budget endpoint only accepts
				// credits (+ optional project_id); enforcement_type cannot be set
				// programmatically, so block enforcement must be re-applied manually.
				if b.EnforcementType == "block" {
					scope := "org"
					desc := "org-level budget"
					if b.ProjectID != nil {
						scope = "org"
						desc = fmt.Sprintf("project budget (project_id=%s)", *b.ProjectID)
					}
					m.AddWarning(scope, "budget_enforcement_block_not_transferred",
						fmt.Sprintf("%s has enforcement_type=block; the PUT budget endpoint only accepts credits — enforcement mode must be set manually on the destination", desc))
				}
			}
			s.Budgets = ob
			hasAny = true
			clog.Debugf("budgets: org=%v project_count=%d",
				ob.OrgBudget != nil, len(ob.ProjectBudgets))
		}

		// Block-unregistered-users feature flag (best-effort; on error warn and continue).
		clog.Debugf("GetBlockUnregisteredUsers org_id=%s", o.ID)
		if blockEnabled, buerr := e.Org.GetBlockUnregisteredUsers(ctx, o.ID); buerr != nil {
			m.AddWarning("org", "block_unregistered_users_unreadable",
				fmt.Sprintf("could not read block-unregistered-users setting: %v", buerr))
		} else {
			s.BlockUnregisteredUsers = &blockEnabled
			hasAny = true
			clog.Debugf("block_unregistered_users: enabled=%v", blockEnabled)
		}

		// Org orb list (best-effort; on error warn and continue). Orbs cannot be
		// auto-migrated — the destination org has a different namespace and orb
		// source is only available via GraphQL/republish — so they are surfaced as
		// manual actions in sync.
		clog.Debugf("GetOrgOrbs org_id=%s", o.ID)
		if orbs, oerr := e.Org.GetOrgOrbs(ctx, o.ID); oerr != nil {
			m.AddWarning("org", "orbs_unreadable",
				fmt.Sprintf("could not read org orb list: %v", oerr))
		} else if len(orbs) > 0 {
			for _, orb := range orbs {
				s.Orbs = append(s.Orbs, manifest.OrgOrb{
					OrbName:             orb.OrbName,
					LatestVersionNumber: orb.LatestVersionNumber,
					OrbID:               orb.OrbID,
					IsPrivate:           orb.IsPrivate,
					Hidden:              orb.Hidden,
					Description:         orb.Description,
				})
			}
			// Emit a single warning for the org: orb source YAML is not exported
			// (GraphQL-only) and the destination org has a different namespace.
			// The captured list is a republish runbook reference only.
			m.AddWarning("org", "orbs_require_republish",
				fmt.Sprintf("%d org orb(s) captured for reference; orb source YAML is not exportable via REST API (GraphQL only) and the destination org has a different namespace — republish each orb under the destination namespace manually", len(orbs)))
			hasAny = true
			clog.Debugf("org orbs: captured %d orb(s)", len(orbs))
		}

		// Derive the org's orb namespace (best-effort; no new GraphQL needed).
		// For GitHub OAuth orgs ("gh/<name>") the namespace is conventionally the
		// org name. For circleci-type orgs, use the org name (may differ from
		// the namespace in practice, but it's the best we can do without GraphQL).
		if o.Name != "" {
			s.OrbNamespace = o.Name
		} else if _, name, ok := splitOrgSlug(orgSlug, o.VCSType); ok {
			s.OrbNamespace = name
		}
		if s.OrbNamespace != "" {
			hasAny = true
		}

		// Release-tracker org settings (best-effort; on error warn and continue).
		// When configured, sync transfers the TTL to the destination via PATCH.
		clog.Debugf("GetReleaseTrackerSettings org_id=%s", o.ID)
		if rtSettings, rterr := e.Org.GetReleaseTrackerSettings(ctx, o.ID); rterr != nil {
			m.AddWarning("org", "release_tracker_unreadable",
				fmt.Sprintf("could not read release-tracker settings: %v", rterr))
		} else if rtSettings != nil {
			s.ReleaseTracker = &manifest.ReleaseTrackerSettings{
				InconclusiveReleaseTTL: rtSettings.InconclusiveReleaseTTL,
			}
			hasAny = true
			clog.Debugf("release_tracker: inconclusive_release_ttl=%s", rtSettings.InconclusiveReleaseTTL)
		}

		// Environment hierarchy (best-effort; on error warn and continue). The
		// hierarchy cannot be auto-migrated (the POST endpoint needs destination
		// deploy-integration IDs), so it is recorded for reference and surfaced as
		// a manual action in sync.
		clog.Debugf("GetEnvironmentHierarchy org_id=%s", o.ID)
		if envH, hierr := e.Org.GetEnvironmentHierarchy(ctx, o.ID); hierr != nil {
			m.AddWarning("org", "environment_hierarchy_unreadable",
				fmt.Sprintf("could not read environment hierarchy: %v", hierr))
		} else if envH != nil {
			mh := &manifest.EnvironmentHierarchy{
				Name:        envH.Name,
				Description: envH.Description,
			}
			for _, l := range envH.Levels {
				mh.Levels = append(mh.Levels, manifest.EnvHierarchyLevel{
					Position:        l.Position,
					IntegrationName: l.IntegrationName,
				})
			}
			s.EnvironmentHierarchy = mh
			hasAny = true
			clog.Debugf("environment_hierarchy: name=%s levels=%d", envH.Name, len(envH.Levels))
		}
	}

	if hasAny {
		m.Source.Org.Settings = s
	}
}

// exportSSO reads the org's SSO enforcement and connection (best-effort) into s.
// It returns true when SSO state worth recording was found (enforcement on or a
// connection present); the all-empty case (enforcement off + no connection) is
// skipped so it does not appear in the manifest. Read failures add an "org"
// warning and never fail the export.
func (e *Exporter) exportSSO(ctx context.Context, m *manifest.Manifest, orgID string, s *manifest.OrgSettings) bool {
	enforced, eerr := e.Org.GetSSOEnforced(ctx, orgID)
	if eerr != nil {
		m.AddWarning("org", "sso_unreadable", fmt.Sprintf("could not read SSO enforcement: %v", eerr))
	}

	connection, found, cerr := e.Org.GetSSOConnection(ctx, orgID)
	if cerr != nil {
		m.AddWarning("org", "sso_unreadable", fmt.Sprintf("could not read SSO connection: %v", cerr))
	}

	if !enforced && !found {
		// No SSO configured and not enforced — nothing to record.
		return false
	}

	sso := &manifest.SSOSettings{Enforced: enforced}
	if found {
		// Extract the (non-sensitive) realm before redacting.
		if realm, ok := connection["realm"].(string); ok {
			sso.Realm = realm
		}
		// The manifest must never contain secret values. SSO connection bodies
		// carry IdP material (signing certs, client secrets, metadata XML); keep
		// the field NAMES for reference but redact their values.
		redacted, redactedKeys := redactSSOConnection(connection)
		sso.Connection = redacted
		if len(redactedKeys) > 0 {
			m.AddWarning("org", "sso_secret_redacted", fmt.Sprintf(
				"redacted %d sensitive SSO connection field(s) from the manifest "+
					"(SSO/SAML is reference-only and must be recreated manually on the "+
					"destination): %s", len(redactedKeys), strings.Join(redactedKeys, ", ")))
		}
	}
	s.SSO = sso
	return true
}

// ssoRedactionPlaceholder marks an SSO connection field whose value held IdP
// secret material and is intentionally NOT recorded in the manifest.
const ssoRedactionPlaceholder = "<redacted: SSO IdP material is not migrated; recreate SSO manually>"

// ssoSensitiveKeySubstrings are case-insensitive substrings that mark an SSO
// connection field as containing secret/IdP material (signing certificate,
// client secret, metadata XML which embeds certs, private keys, etc.).
var ssoSensitiveKeySubstrings = []string{
	"secret", "password", "credential", "token",
	"private", "cert", "x509", "metadata",
}

// redactSSOConnection returns a copy of the SSO connection map with the values
// of secret-shaped keys replaced by ssoRedactionPlaceholder, plus the sorted
// list of keys that were redacted. Field names are preserved (so the manifest
// still documents WHICH IdP fields existed) but their values never leak into
// the manifest, which is contractually free of secret values.
//
// The function recurses into nested map[string]any values so that secret fields
// inside nested objects (e.g. idp_config.private_key, nested *_cert/*_secret)
// are also redacted by the same key-substring rules.
func redactSSOConnection(conn map[string]any) (map[string]any, []string) {
	return redactSSOConnectionInto(conn, "")
}

// redactSSOConnectionInto is the recursive implementation of redactSSOConnection.
// keyPrefix is used only to build dotted key paths for the returned redactedKeys
// list (e.g. "idp_config.private_key"). At the top level it is empty.
func redactSSOConnectionInto(conn map[string]any, keyPrefix string) (map[string]any, []string) {
	if conn == nil {
		return nil, nil
	}
	out := make(map[string]any, len(conn))
	var redactedKeys []string
	for k, v := range conn {
		fullKey := k
		if keyPrefix != "" {
			fullKey = keyPrefix + "." + k
		}
		lk := strings.ToLower(k)
		sensitive := false
		for _, sub := range ssoSensitiveKeySubstrings {
			if strings.Contains(lk, sub) {
				sensitive = true
				break
			}
		}
		if sensitive {
			out[k] = ssoRedactionPlaceholder
			redactedKeys = append(redactedKeys, fullKey)
		} else if nested, ok := v.(map[string]any); ok {
			// Recurse into nested objects so sub-fields are also redacted.
			redactedNested, nestedKeys := redactSSOConnectionInto(nested, fullKey)
			out[k] = redactedNested
			redactedKeys = append(redactedKeys, nestedKeys...)
		} else {
			out[k] = v
		}
	}
	sort.Strings(redactedKeys)
	return out, redactedKeys
}

// otelHeaderRedactionPlaceholder marks an OTel exporter header value that was
// redacted to prevent auth tokens from appearing in the manifest.
const otelHeaderRedactionPlaceholder = "<redacted: OTel auth header>"

// redactOTelHeaders returns a copy of the OTel exporter headers map with every
// header value replaced by otelHeaderRedactionPlaceholder, preserving key names
// so the operator knows which headers were configured. The second return value
// is the sorted list of header keys whose values were redacted.
//
// Rationale: OTel exporter headers commonly carry authentication material (e.g.
// "Authorization: Bearer <token>"). Redacting all header VALUES unconditionally
// is the conservative, safe-to-share choice — header names alone carry no secret.
func redactOTelHeaders(headers map[string]string) (map[string]string, []string) {
	if len(headers) == 0 {
		return headers, nil
	}
	out := make(map[string]string, len(headers))
	redactedKeys := make([]string, 0, len(headers))
	for k := range headers {
		out[k] = otelHeaderRedactionPlaceholder
		redactedKeys = append(redactedKeys, k)
	}
	sort.Strings(redactedKeys)
	return out, redactedKeys
}

// urlOrbAuthSafeValues is the known-safe enum set for URLOrbAllowEntry.Auth.
// Observed values confirmed from the CircleCI API and test fixtures:
//   - "none"  — no authentication required for the orb source URL
//   - "aws"   — AWS-signed requests (IRSA/instance profile; no embedded secret)
//
// Any value outside this set is treated as a potential credential and is
// redacted before the manifest is written. This defensive stance ensures that
// future Auth types that DO carry secret material (e.g. a bearer-token form)
// are never written verbatim into the manifest.
var urlOrbAuthSafeValues = map[string]bool{
	"none": true,
	"aws":  true,
}

// urlOrbAuthRedactionPlaceholder marks a URLOrbAllowEntry.Auth value that is
// not in the known-safe enum set and has therefore been redacted.
const urlOrbAuthRedactionPlaceholder = "<redacted: unknown URL-orb auth type>"

// redactURLOrbAuth returns auth verbatim when it is empty (no auth configured)
// or in the known-safe enum set (currently "none" and "aws"). Any other value
// is replaced with urlOrbAuthRedactionPlaceholder to prevent unrecognised
// credential formats from appearing in the manifest. Empty is passed through so
// entries with no auth round-trip correctly through sync.
func redactURLOrbAuth(auth string) string {
	if auth == "" || urlOrbAuthSafeValues[auth] {
		return auth
	}
	return urlOrbAuthRedactionPlaceholder
}
