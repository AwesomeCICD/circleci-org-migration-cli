package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// OrgSettingsWriter is the destination org-settings API the syncer needs.
// It is a narrow interface over the methods defined in api/org/orgsettings.go,
// api/org/otel.go, api/org/contacts.go, api/org/storage_retention.go,
// api/org/budgets.go, api/org/blockusers.go, and api/org/release_tracker.go.
type OrgSettingsWriter interface {
	UpdateFeatureFlags(ctx context.Context, vcsType, orgName string, flags map[string]bool) error
	SetOIDCClaims(ctx context.Context, orgID string, audience []string, ttl string) error
	CreateURLOrbAllowEntry(ctx context.Context, slugOrID, name, prefix, auth string) error
	PutPolicyBundle(ctx context.Context, ownerID string, policies map[string]string) error
	SetPolicyEnforcement(ctx context.Context, ownerID string, enabled bool) error
	CreateOTelExporter(ctx context.Context, orgID, endpoint, protocol string, insecure bool, headers map[string]string) error
	SetContacts(ctx context.Context, orgID string, primary, security []string) error
	// SetStorageRetention writes artifact/cache/workspace retention controls to
	// the destination org. The server clamps values to the plan's limits.
	// controls is passed as a manifest.StorageRetentionControls value; callers
	// may need a thin adapter if using api/org.Client (see storage_retention_adapter.go).
	SetStorageRetention(ctx context.Context, orgUUID string, controls StorageRetentionArgs) error
	// SetBudget creates or updates a spend budget. Pass projectID == nil for the
	// org-level budget; pass a non-nil project UUID for a per-project budget.
	SetBudget(ctx context.Context, orgUUID string, projectID *string, credits int) error
	// SetBlockUnregisteredUsers enables or disables the "block unregistered user
	// spend" feature.
	SetBlockUnregisteredUsers(ctx context.Context, orgUUID string, enabled bool) error
	// SetReleaseTrackerSettings applies release-tracker settings to the destination
	// org via PATCH. Called only when ReleaseTracker is non-nil in the manifest.
	SetReleaseTrackerSettings(ctx context.Context, orgUUID string, ttl string) error
}

// URLOrbAllowEntry is a single entry on an org's URL-orb allow list.
// It mirrors api/org.URLOrbAllowEntry; the syncer package re-declares it so
// idempotency checks do not force an api/org import in syncer.
type URLOrbAllowEntry struct {
	ID     string
	Name   string
	Prefix string
	Auth   string
}

// OTelExporter is one OpenTelemetry exporter configuration on an org.
// It mirrors api/org.OTelExporter; the syncer package re-declares it so
// idempotency checks do not force an api/org import in syncer.
type OTelExporter struct {
	ID       string
	Endpoint string
	Protocol string
	Insecure bool
	Headers  map[string]string
}

// URLOrbAllowListGetter is an optional capability for OrgSettingsWriter implementations
// that support pre-flight idempotency checks for the URL-orb allow list.
// When OrgSettingsWriter also implements this interface, syncURLOrbAllowList will
// skip entries whose name+prefix already exist in the destination (preventing
// duplicates on re-runs). Implementations that do not provide this method fall
// back to the previous unconditional-create behaviour.
type URLOrbAllowListGetter interface {
	GetURLOrbAllowList(ctx context.Context, slugOrID string) ([]URLOrbAllowEntry, error)
}

// OTelExporterGetter is an optional capability for OrgSettingsWriter implementations
// that support pre-flight idempotency checks for OTel exporters.
// When OrgSettingsWriter also implements this interface, syncOTelExporters will
// skip exporters whose endpoint+protocol already exist in the destination
// (preventing duplicates on re-runs). Implementations that do not provide this
// method fall back to the previous unconditional-create behaviour.
type OTelExporterGetter interface {
	GetOTelExporters(ctx context.Context, orgID string) ([]OTelExporter, error)
}

// StorageRetentionArgs carries the storage-retention values to write. It is a
// locally-defined struct so neither the syncer nor the OrgSettingsWriter
// interface needs to depend on api/org directly.
type StorageRetentionArgs struct {
	CacheDays     int
	WorkspaceDays int
	ArtifactDays  int
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
func (s *Syncer) SyncOrgSettings(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	destOrgID, err := s.Org.ResolveOrgID(ctx, destSlug)
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

	s.syncFeatureFlags(ctx, report, src, vcs, orgName, opts)
	s.syncOIDCClaims(ctx, report, src, destOrgID, opts)
	s.syncURLOrbAllowList(ctx, report, src, destSlug, opts)
	s.syncPolicies(ctx, report, src, destOrgID, opts)
	s.reportAuditLogConfigs(ctx, report, src)
	s.reportSSO(ctx, report, src)
	s.syncOTelExporters(ctx, report, src, destOrgID, opts)
	s.syncContacts(ctx, report, src, destOrgID, opts)
	s.syncStorageRetention(ctx, report, src, destOrgID, opts)
	s.syncBudgets(ctx, report, src, destOrgID, mapping, opts)
	s.syncBlockUnregisteredUsers(ctx, report, src, destOrgID, opts)
	s.reportOrbs(ctx, report, src)
	s.syncReleaseTracker(ctx, report, src, destOrgID, opts)
	s.reportEnvironmentHierarchy(ctx, report, src)

	return report, nil
}

// reportSSO records the captured SSO (SAML) state as a single "manual" action.
// SSO is never auto-applied: recreating it on the destination requires DNS TXT
// domain verification and IdP-side SAML app / iframe-origin setup, none of which
// is automatable, so we surface it for the operator and never write.
func (s *Syncer) reportSSO(ctx context.Context, report *Report, src *manifest.OrgSettings) {
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
func (s *Syncer) reportAuditLogConfigs(ctx context.Context, report *Report, src *manifest.OrgSettings) {
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
func (s *Syncer) syncFeatureFlags(ctx context.Context, report *Report, src *manifest.OrgSettings, vcs, orgName string, opts Options) {
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

		if err := s.OrgSettings.UpdateFeatureFlags(ctx, vcs, orgName, map[string]bool{flagKey: val}); err != nil {
			report.add("org-settings", target, "error", err.Error())
			continue
		}
		report.add("org-settings", target, "set", fmt.Sprintf("set feature flag %q = %v", flagKey, val))
	}
}

// syncOIDCClaims writes the OIDC audience/TTL when present.
func (s *Syncer) syncOIDCClaims(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if len(src.OIDCAudience) == 0 && src.OIDCTTL == "" {
		return
	}

	target := "oidc_claims"
	if !opts.Apply {
		report.add("org-settings", target, "set",
			fmt.Sprintf("would set OIDC audience=%v ttl=%q", src.OIDCAudience, src.OIDCTTL))
		return
	}

	if err := s.OrgSettings.SetOIDCClaims(ctx, destOrgID, src.OIDCAudience, src.OIDCTTL); err != nil {
		report.add("org-settings", target, "error", err.Error())
		return
	}
	report.add("org-settings", target, "set",
		fmt.Sprintf("set OIDC audience=%v ttl=%q", src.OIDCAudience, src.OIDCTTL))
}

// syncURLOrbAllowList adds each allow-list entry that is not already present.
// When OrgSettings also implements URLOrbAllowListGetter, existing entries
// (matched by name+prefix) are skipped with status "exists" to prevent
// duplicates on re-runs.
func (s *Syncer) syncURLOrbAllowList(ctx context.Context, report *Report, src *manifest.OrgSettings, destSlug string, opts Options) {
	if len(src.URLOrbAllowList) == 0 {
		return
	}

	// Pre-fetch existing entries for idempotency when the writer supports it
	// and we are in apply mode (in dry-run the dest slug may be a placeholder).
	existing := map[string]bool{} // key: name+"\x00"+prefix
	if opts.Apply {
		if getter, ok := s.OrgSettings.(URLOrbAllowListGetter); ok {
			if entries, err := getter.GetURLOrbAllowList(ctx, destSlug); err == nil {
				for _, e := range entries {
					existing[e.Name+"\x00"+e.Prefix] = true
				}
			}
		}
	}

	for _, entry := range src.URLOrbAllowList {
		target := "url_orb_allow_list:" + entry.Name

		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would add URL-orb allow-list entry %q (%s)", entry.Name, entry.Prefix))
			continue
		}

		if existing[entry.Name+"\x00"+entry.Prefix] {
			report.add("org-settings", target, "exists",
				fmt.Sprintf("URL-orb allow-list entry %q (%s) already present", entry.Name, entry.Prefix))
			continue
		}

		if err := s.OrgSettings.CreateURLOrbAllowEntry(ctx, destSlug, entry.Name, entry.Prefix, entry.Auth); err != nil {
			report.add("org-settings", target, "error", err.Error())
			continue
		}
		report.add("org-settings", target, "set",
			fmt.Sprintf("added URL-orb allow-list entry %q (%s)", entry.Name, entry.Prefix))
	}
}

// syncPolicies writes the policy bundle and enforcement setting.
func (s *Syncer) syncPolicies(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if len(src.ConfigPolicies) > 0 {
		target := "config_policies"
		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would write %d config polic(ies) (Scale plan required)", len(src.ConfigPolicies)))
		} else if err := s.OrgSettings.PutPolicyBundle(ctx, destOrgID, src.ConfigPolicies); err != nil {
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
		} else if err := s.OrgSettings.SetPolicyEnforcement(ctx, destOrgID, enabled); err != nil {
			report.add("org-settings", target, "error",
				fmt.Sprintf("could not set policy enforcement (Scale plan required): %v", err))
		} else {
			report.add("org-settings", target, "set",
				fmt.Sprintf("set policy enforcement enabled=%v", enabled))
		}
	}
}

// syncOTelExporters creates each OTel exporter on the destination. When
// OrgSettings also implements OTelExporterGetter, existing exporters (matched
// by endpoint+protocol) are skipped with status "exists" to prevent duplicates
// on re-runs. Header values are NOT sent — they were redacted on export and are
// unusable. When an exporter had headers, a "manual" action is emitted listing
// the header keys that must be re-added as secrets. The 5-exporter cap is
// enforced by the API; if more than 5 exporters are present a note is included
// in the detail.
func (s *Syncer) syncOTelExporters(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if len(src.OTelExporters) == 0 {
		return
	}

	capNote := ""
	if len(src.OTelExporters) > 5 {
		capNote = " (warning: source has >5 exporters; the API enforces a 5-exporter cap per org)"
	}

	// Pre-fetch existing exporters for idempotency when the writer supports it
	// and we are in apply mode.
	existing := map[string]bool{} // key: endpoint+"\x00"+protocol
	if opts.Apply {
		if getter, ok := s.OrgSettings.(OTelExporterGetter); ok {
			if exporters, err := getter.GetOTelExporters(ctx, destOrgID); err == nil {
				for _, e := range exporters {
					existing[e.Endpoint+"\x00"+e.Protocol] = true
				}
			}
		}
	}

	for i, ex := range src.OTelExporters {
		target := fmt.Sprintf("otel:%d", i)

		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would create OTel exporter endpoint=%q protocol=%q insecure=%v%s",
					ex.Endpoint, ex.Protocol, ex.Insecure, capNote))
			if len(ex.Headers) > 0 {
				keys := headerKeys(ex.Headers)
				report.add("org-settings", target, "manual",
					fmt.Sprintf("OTel exporter header values were redacted on export and cannot be replayed; re-add these header keys manually: %v", keys))
			}
			continue
		}

		if existing[ex.Endpoint+"\x00"+ex.Protocol] {
			report.add("org-settings", target, "exists",
				fmt.Sprintf("OTel exporter endpoint=%q protocol=%q already present", ex.Endpoint, ex.Protocol))
			continue
		}

		if err := s.OrgSettings.CreateOTelExporter(ctx, destOrgID, ex.Endpoint, ex.Protocol, ex.Insecure, nil); err != nil {
			report.add("org-settings", target, "error", fmt.Sprintf("could not create OTel exporter: %v", err))
			continue
		}
		report.add("org-settings", target, "set",
			fmt.Sprintf("created OTel exporter endpoint=%q protocol=%q insecure=%v%s",
				ex.Endpoint, ex.Protocol, ex.Insecure, capNote))

		if len(ex.Headers) > 0 {
			keys := headerKeys(ex.Headers)
			report.add("org-settings", target, "manual",
				fmt.Sprintf("OTel exporter header values were redacted on export and cannot be replayed; re-add these header keys manually: %v", keys))
		}
	}
}

// syncContacts applies the org's primary and security contact email lists to
// the destination via PUT (overwrites). Skipped silently when Contacts is nil
// or both lists are empty.
func (s *Syncer) syncContacts(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if src.Contacts == nil {
		return
	}
	if len(src.Contacts.Primary) == 0 && len(src.Contacts.Security) == 0 {
		return
	}

	target := "contacts"
	if !opts.Apply {
		report.add("org-settings", target, "set",
			fmt.Sprintf("would set contacts primary=%v security=%v",
				src.Contacts.Primary, src.Contacts.Security))
		return
	}

	if err := s.OrgSettings.SetContacts(ctx, destOrgID, src.Contacts.Primary, src.Contacts.Security); err != nil {
		report.add("org-settings", target, "error", fmt.Sprintf("could not set contacts: %v", err))
		return
	}
	report.add("org-settings", target, "set",
		fmt.Sprintf("set contacts primary=%v security=%v",
			src.Contacts.Primary, src.Contacts.Security))
}

// headerKeys returns a sorted list of keys from a map for deterministic output.
func headerKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Sort for deterministic output in test assertions and reports.
	sortStrings(keys)
	return keys
}

// sortStrings is a simple insertion sort for short key slices (avoids importing
// "sort" only for this helper).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// syncStorageRetention transfers the manifest's storage-retention controls to
// the destination org when the manifest carries them. Dry-run aware; reports
// created/updated like other sync sections. A note is included that the server
// clamps values to the destination plan's limits.
func (s *Syncer) syncStorageRetention(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if src.StorageRetention == nil {
		return
	}
	sr := src.StorageRetention
	target := "storage_retention"
	detail := fmt.Sprintf(
		"cache=%dd workspace=%dd artifact=%dd (values may be clamped to the destination plan's limits)",
		sr.CacheDays, sr.WorkspaceDays, sr.ArtifactDays,
	)

	if !opts.Apply {
		report.add("org-settings", target, "set",
			"would set storage-retention controls: "+detail)
		return
	}

	args := StorageRetentionArgs{
		CacheDays:     sr.CacheDays,
		WorkspaceDays: sr.WorkspaceDays,
		ArtifactDays:  sr.ArtifactDays,
	}
	if err := s.OrgSettings.SetStorageRetention(ctx, destOrgID, args); err != nil {
		report.add("org-settings", target, "error",
			fmt.Sprintf("could not set storage-retention controls: %v", err))
		return
	}
	report.add("org-settings", target, "set", "set storage-retention controls: "+detail)
}

// syncBudgets transfers the org-level budget and, where possible, per-project
// budgets to the destination. Per-project budgets require the source project UUID
// to be mapped to a destination project UUID via the mapping; unmapped projects
// are flagged for manual recreation. EnforcementType is captured for reference
// but the PUT endpoint only accepts credits (+project_id), so it may not be
// transferred automatically (a note is emitted in the report detail).
func (s *Syncer) syncBudgets(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, mapping *manifest.Mapping, opts Options) {
	if src.Budgets == nil {
		return
	}
	b := src.Budgets

	// Org-level budget.
	if b.OrgBudget != nil {
		ob := b.OrgBudget
		target := "budget:org"
		enfNote := ""
		if ob.EnforcementType != "" && ob.EnforcementType != "block" {
			enfNote = fmt.Sprintf(" (enforcement_type=%q captured for reference — only credits are transferred via PUT)", ob.EnforcementType)
		}
		detail := fmt.Sprintf("credits=%d enforcement_type=%q%s", ob.Credits, ob.EnforcementType, enfNote)

		if !opts.Apply {
			report.add("org-settings", target, "set",
				"would set org budget: "+detail)
		} else if err := s.OrgSettings.SetBudget(ctx, destOrgID, nil, ob.Credits); err != nil {
			report.add("org-settings", target, "error",
				fmt.Sprintf("could not set org budget: %v", err))
		} else {
			report.add("org-settings", target, "set", "set org budget: "+detail)
		}
	}

	// Per-project budgets: map source project UUID → destination project UUID.
	for i, pb := range b.ProjectBudgets {
		if pb.ProjectID == nil {
			continue
		}
		srcProjID := *pb.ProjectID
		target := fmt.Sprintf("budget:project:%d", i)

		// Attempt to resolve the destination project UUID via the mapping.
		// The mapping value must be a destination project UUID (not a slug);
		// passing a slug (containing "/") causes a 422 from the budgets API.
		destProjID, ok := resolveProjectByID(srcProjID, mapping)
		if !ok {
			report.add("org-settings", target, "manual",
				fmt.Sprintf(
					"per-project budget (source project_id=%q, credits=%d, enforcement_type=%q) "+
						"cannot be automatically transferred: the source project UUID has no "+
						"mapping to the destination — add an entry to the mapping file with "+
						"the source project UUID as the key and the destination project UUID "+
						"as the value, then re-run",
					srcProjID, pb.Credits, pb.EnforcementType))
			continue
		}

		// Warn when the mapped value looks like a slug (contains "/") rather
		// than a UUID — the budgets API requires a UUID and rejects slugs with
		// a 422.  Emit a "manual" action so the operator can correct the mapping
		// before re-running; do not attempt the doomed API call.
		if strings.Contains(destProjID, "/") {
			report.add("org-settings", target, "manual",
				fmt.Sprintf(
					"per-project budget mapping value %q looks like a project slug, not a UUID "+
						"(contains \"/\") — the budgets API requires a destination project UUID; "+
						"update the mapping entry for source project %q to use the destination "+
						"project UUID and re-run",
					destProjID, srcProjID))
			continue
		}

		if !opts.Apply {
			report.add("org-settings", target, "set",
				fmt.Sprintf("would set per-project budget dest_project_id=%q credits=%d", destProjID, pb.Credits))
			continue
		}
		if err := s.OrgSettings.SetBudget(ctx, destOrgID, &destProjID, pb.Credits); err != nil {
			report.add("org-settings", target, "error",
				fmt.Sprintf("could not set per-project budget dest_project_id=%q: %v", destProjID, err))
			continue
		}
		report.add("org-settings", target, "set",
			fmt.Sprintf("set per-project budget dest_project_id=%q credits=%d", destProjID, pb.Credits))
	}
}

// resolveProjectByID tries to find a destination project UUID for a source
// project UUID using the mapping's Projects table. It returns the destination
// project ID value (from the mapping) and true when found. Returns ("", false)
// when the mapping is nil, the Projects table is empty, or the source ID is
// absent.
//
// The mapping entry must use the source project UUID as the key and the
// DESTINATION PROJECT UUID as the value. The budgets API requires a UUID and
// rejects slug-form values (containing "/") with a 422 error. Callers should
// validate the returned value with strings.Contains(v, "/") and warn accordingly.
func resolveProjectByID(srcProjID string, mapping *manifest.Mapping) (string, bool) {
	if mapping == nil || len(mapping.Projects) == 0 {
		return "", false
	}
	// Direct key lookup (source project UUID used as mapping key).
	if dest, ok := mapping.Projects[srcProjID]; ok {
		return dest, true
	}
	return "", false
}

// syncBlockUnregisteredUsers transfers the "block unregistered user spend" feature
// flag to the destination. Dry-run aware.
func (s *Syncer) syncBlockUnregisteredUsers(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if src.BlockUnregisteredUsers == nil {
		return
	}
	enabled := *src.BlockUnregisteredUsers
	target := "block_unregistered_users"

	if !opts.Apply {
		report.add("org-settings", target, "set",
			fmt.Sprintf("would set block_unregistered_users enabled=%v", enabled))
		return
	}

	if err := s.OrgSettings.SetBlockUnregisteredUsers(ctx, destOrgID, enabled); err != nil {
		report.add("org-settings", target, "error",
			fmt.Sprintf("could not set block_unregistered_users: %v", err))
		return
	}
	report.add("org-settings", target, "set",
		fmt.Sprintf("set block_unregistered_users enabled=%v", enabled))
}

// reportOrbs records each captured orb as a "manual" action. Orbs cannot be
// auto-migrated: the destination org has a different namespace and orb source
// code is only accessible via GraphQL / the republish workflow. Each entry
// carries enough metadata (name, version, is_private, hidden) for the operator
// to republish the orb in the destination namespace.
func (s *Syncer) reportOrbs(ctx context.Context, report *Report, src *manifest.OrgSettings) {
	for _, orb := range src.Orbs {
		target := "orb:" + orb.OrbName
		report.add("org-settings", target, "manual",
			fmt.Sprintf(
				"orb %q (version %s, private=%v, hidden=%v) — "+
					"republish this orb in the destination namespace; "+
					"orbs cannot be auto-migrated (the destination org has a different namespace "+
					"and orb source is only available via GraphQL/republish)",
				orb.OrbName, orb.LatestVersionNumber, orb.IsPrivate, orb.Hidden,
			))
	}
}

// syncReleaseTracker transfers the release-tracker settings (inconclusive_release_ttl)
// to the destination org via PATCH. Dry-run aware.
func (s *Syncer) syncReleaseTracker(ctx context.Context, report *Report, src *manifest.OrgSettings, destOrgID string, opts Options) {
	if src.ReleaseTracker == nil || src.ReleaseTracker.InconclusiveReleaseTTL == "" {
		return
	}
	ttl := src.ReleaseTracker.InconclusiveReleaseTTL
	target := "release_tracker_settings"

	if !opts.Apply {
		report.add("org-settings", target, "set",
			fmt.Sprintf("would set release-tracker inconclusive_release_ttl=%q", ttl))
		return
	}

	if err := s.OrgSettings.SetReleaseTrackerSettings(ctx, destOrgID, ttl); err != nil {
		report.add("org-settings", target, "error",
			fmt.Sprintf("could not set release-tracker settings: %v", err))
		return
	}
	report.add("org-settings", target, "set",
		fmt.Sprintf("set release-tracker inconclusive_release_ttl=%q", ttl))
}

// reportEnvironmentHierarchy records the captured environment hierarchy as a
// "manual" action. The hierarchy cannot be auto-migrated because recreating it
// via the POST endpoint requires destination deploy-integration IDs that cannot
// be mapped automatically from the source org. The report includes the hierarchy
// name and level names so the operator can recreate it manually after configuring
// the matching deploy integrations in the destination.
func (s *Syncer) reportEnvironmentHierarchy(ctx context.Context, report *Report, src *manifest.OrgSettings) {
	if src.EnvironmentHierarchy == nil {
		return
	}
	h := src.EnvironmentHierarchy
	levelNames := make([]string, 0, len(h.Levels))
	for _, l := range h.Levels {
		levelNames = append(levelNames, fmt.Sprintf("%d:%s", l.Position, l.IntegrationName))
	}
	report.add("org-settings", "environment_hierarchy", "manual",
		fmt.Sprintf(
			"environment hierarchy %q (levels: %v) — "+
				"recreate the environment hierarchy in the destination after configuring "+
				"the matching deploy integrations; the source integration IDs cannot be "+
				"mapped automatically",
			h.Name, levelNames,
		))
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
