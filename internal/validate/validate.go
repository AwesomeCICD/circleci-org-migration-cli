// Package validate compares two manifests (source and destination) and reports
// which items matched, which are missing on the destination, and which need
// manual attention. It is intentionally pure (no I/O) so it can be exercised
// by table-driven unit tests without any network access.
package validate

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// Status values for a single check item.
const (
	StatusMatched = "matched" // ✓ present on destination
	StatusMissing = "missing" // ✗ absent on destination — causes non-zero exit
	StatusManual  = "manual"  // ⚠ needs manual attention — surface but do not fail
)

// Item is one comparison result for a single resource.
type Item struct {
	// Status is one of StatusMatched, StatusMissing, StatusManual.
	Status string `json:"status"`
	// Section is the resource category (e.g. "Contexts", "Projects").
	Section string `json:"section"`
	// Name is the resource identifier (context name, project slug, etc.).
	Name string `json:"name"`
	// Detail is a human-readable description of the finding.
	Detail string `json:"detail"`
}

// Section groups all items for one resource type.
type Section struct {
	Name    string `json:"name"`
	Items   []Item `json:"items"`
	Skipped bool   `json:"skipped,omitempty"`
	// SkipReason is set when Skipped is true.
	SkipReason string `json:"skip_reason,omitempty"`
}

// Counts returns the number of matched, missing, and manual items in the section.
func (s *Section) Counts() (matched, missing, manual int) {
	for _, it := range s.Items {
		switch it.Status {
		case StatusMatched:
			matched++
		case StatusMissing:
			missing++
		case StatusManual:
			manual++
		}
	}
	return
}

// Result is the complete output of Compare.
type Result struct {
	SourceOrg string    `json:"source_org"`
	DestOrg   string    `json:"dest_org"`
	Sections  []Section `json:"sections"`
}

// HasMissing reports whether any item across all sections is StatusMissing.
// A true return value corresponds to a non-zero exit code.
func (r *Result) HasMissing() bool {
	for _, s := range r.Sections {
		for _, it := range s.Items {
			if it.Status == StatusMissing {
				return true
			}
		}
	}
	return false
}

// TotalsLine returns a concise summary string, e.g.
// "✓ 12 matched  ✗ 3 missing  ⚠ 2 manual"
func (r *Result) TotalsLine() string {
	var totalMatched, totalMissing, totalManual int
	for _, s := range r.Sections {
		m, ms, mn := s.Counts()
		totalMatched += m
		totalMissing += ms
		totalManual += mn
	}
	return fmt.Sprintf("✓ %d matched  ✗ %d missing  ⚠ %d manual",
		totalMatched, totalMissing, totalManual)
}

// Options controls which optional sections are evaluated.
type Options struct {
	// DestRunnerNamespace, when non-empty, enables the runner resource-class
	// comparison against the destination manifest's classes. When empty the
	// runner section is skipped with a note.
	DestRunnerNamespace string
	// DestOrbNamespace, when non-empty, enables the orb comparison. When empty
	// the orb section is skipped with a note.
	DestOrbNamespace string
}

// Compare diffs src (source manifest) against dst (destination manifest) and
// returns a structured Result. mapping, when non-nil, is used to translate
// source names/slugs to their expected destination counterparts so that
// renamed contexts and projects are correctly matched.
//
// Secret VALUES are never compared — the CircleCI API masks them and they are
// intentionally absent from both manifests. What is compared is purely the
// presence and structure of resources (names, types, counts, settings).
func Compare(src, dst *manifest.Manifest, mapping *manifest.Mapping, opts Options) Result {
	r := Result{
		SourceOrg: src.Source.Org.Slug,
		DestOrg:   dst.Source.Org.Slug,
	}

	r.Sections = append(r.Sections, compareContexts(src, dst, mapping))
	r.Sections = append(r.Sections, compareProjects(src, dst, mapping))
	r.Sections = append(r.Sections, compareOrgSettings(src, dst))
	r.Sections = append(r.Sections, compareRunners(src, dst, opts.DestRunnerNamespace))
	r.Sections = append(r.Sections, compareOrbs(src, dst, opts.DestOrbNamespace))
	r.Sections = append(r.Sections, compareCIAM(src, dst))

	return r
}

// ---------------------------------------------------------------------------
// Contexts
// ---------------------------------------------------------------------------

func compareContexts(src, dst *manifest.Manifest, mapping *manifest.Mapping) Section {
	sec := Section{Name: "Contexts"}

	// Index destination contexts by name for O(1) lookup.
	dstByName := make(map[string]manifest.Context, len(dst.Contexts))
	for _, c := range dst.Contexts {
		dstByName[c.Name] = c
	}

	for _, sc := range src.Contexts {
		// Determine the expected destination name: explicit mapping wins, then
		// identity (same name).
		// Contexts are not in the Projects map. Context-name mapping may be
		// added in a future version; for now the destination lookup uses the
		// same name as the source.
		destName := sc.Name

		dc, ok := dstByName[destName]
		if !ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Contexts",
				Name:    sc.Name,
				Detail:  fmt.Sprintf("context %q is missing on the destination", sc.Name),
			})
			continue
		}

		// Context exists — check env-var names.
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Contexts",
			Name:    sc.Name,
			Detail:  "context present",
		})

		// Build dest env-var set.
		dstVarNames := make(map[string]struct{}, len(dc.EnvVars))
		for _, v := range dc.EnvVars {
			dstVarNames[v.Name] = struct{}{}
		}
		for _, sv := range sc.EnvVars {
			if _, found := dstVarNames[sv.Name]; !found {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMissing,
					Section: "Contexts",
					Name:    sc.Name + "/" + sv.Name,
					Detail:  fmt.Sprintf("context %q: env var %q not found on destination", sc.Name, sv.Name),
				})
			} else {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMatched,
					Section: "Contexts",
					Name:    sc.Name + "/" + sv.Name,
					Detail:  "env var name present",
				})
			}
		}

		// Restrictions: compare by type and rough count.
		compareContextRestrictions(sc, dc, &sec)
	}

	return sec
}

// compareContextRestrictions checks that each source restriction type is
// represented on the destination. Group restrictions can only be applied on
// OAuth orgs and are noted as manual (we compare presence by type, not by
// UUID). Project restrictions are flagged as manual because the source
// project UUID cannot be resolved to a dest UUID here.
func compareContextRestrictions(sc, dc manifest.Context, sec *Section) {
	if len(sc.Restrictions) == 0 {
		return
	}

	// Count by type on source and dest.
	srcTypes := map[string]int{}
	for _, r := range sc.Restrictions {
		srcTypes[r.Type]++
	}
	dstTypes := map[string]int{}
	for _, r := range dc.Restrictions {
		dstTypes[r.Type]++
	}

	for rType, srcCount := range srcTypes {
		dstCount := dstTypes[rType]
		name := sc.Name + "/restriction:" + rType

		switch rType {
		case "group":
			// Group restrictions may not be present on non-OAuth orgs; always
			// flag as manual because group IDs differ between orgs.
			sec.Items = append(sec.Items, Item{
				Status:  StatusManual,
				Section: "Contexts",
				Name:    name,
				Detail:  fmt.Sprintf("context %q: %d group restriction(s) — verify group restrictions on destination (group IDs differ between orgs)", sc.Name, srcCount),
			})
		case "project":
			// Project restrictions embed a source project UUID; cannot auto-verify.
			status := StatusManual
			detail := fmt.Sprintf("context %q: %d project restriction(s) — verify project restrictions on destination (source UUIDs cannot be mapped automatically here)", sc.Name, srcCount)
			if dstCount == 0 {
				status = StatusMissing
				detail = fmt.Sprintf("context %q: %d project restriction(s) on source but none on destination — recreate manually", sc.Name, srcCount)
			}
			sec.Items = append(sec.Items, Item{
				Status:  status,
				Section: "Contexts",
				Name:    name,
				Detail:  detail,
			})
		case "expression":
			if dstCount >= srcCount {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMatched,
					Section: "Contexts",
					Name:    name,
					Detail:  fmt.Sprintf("context %q: %d expression restriction(s) present", sc.Name, srcCount),
				})
			} else {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMissing,
					Section: "Contexts",
					Name:    name,
					Detail:  fmt.Sprintf("context %q: %d expression restriction(s) on source but only %d on destination", sc.Name, srcCount, dstCount),
				})
			}
		default:
			// Unknown type — surface as manual.
			sec.Items = append(sec.Items, Item{
				Status:  StatusManual,
				Section: "Contexts",
				Name:    name,
				Detail:  fmt.Sprintf("context %q: restriction type %q — verify manually", sc.Name, rType),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Projects
// ---------------------------------------------------------------------------

func compareProjects(src, dst *manifest.Manifest, mapping *manifest.Mapping) Section {
	sec := Section{Name: "Projects"}

	// Index dest projects by slug for O(1) lookup.
	dstBySlug := make(map[string]manifest.Project, len(dst.Projects))
	for _, p := range dst.Projects {
		dstBySlug[p.Slug] = p
	}
	// Also index by name for fallback matching.
	dstByName := make(map[string]manifest.Project, len(dst.Projects))
	for _, p := range dst.Projects {
		if p.Name != "" {
			dstByName[p.Name] = p
		}
	}

	for _, sp := range src.Projects {
		// Resolve the expected destination slug.
		destSlug := sp.Slug
		if mapping != nil {
			if mapped, ok := mapping.ResolveProjectSlug(sp.Slug); ok {
				destSlug = mapped
			}
		}

		dp, ok := dstBySlug[destSlug]
		if !ok {
			// Fallback: try by project name (last slug component).
			name := slugLastComponent(destSlug)
			if p, found := dstByName[name]; found {
				dp = p
				ok = true
			}
		}

		if !ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Projects",
				Name:    sp.Slug,
				Detail:  fmt.Sprintf("project %q (expected dest slug %q) is missing on the destination", sp.Slug, destSlug),
			})
			continue
		}

		// Project exists.
		followedStr := ""
		if dp.Followed != nil && !*dp.Followed {
			followedStr = " (not followed/enabled)"
		}
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Projects",
			Name:    sp.Slug,
			Detail:  "project present" + followedStr,
		})

		// Check followed state.
		if dp.Followed != nil && !*dp.Followed {
			sec.Items = append(sec.Items, Item{
				Status:  StatusManual,
				Section: "Projects",
				Name:    sp.Slug + "/followed",
				Detail:  fmt.Sprintf("project %q exists on destination but shows as not followed/enabled. If you JUST ran apply, the follow may still be propagating — re-run validate in a minute. If it persists, enable builds for the project in the CircleCI UI.", destSlug),
			})
		}

		// Compare env-var names.
		dstVarNames := make(map[string]struct{}, len(dp.EnvVars))
		for _, v := range dp.EnvVars {
			dstVarNames[v.Name] = struct{}{}
		}
		for _, sv := range sp.EnvVars {
			if _, found := dstVarNames[sv.Name]; !found {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMissing,
					Section: "Projects",
					Name:    sp.Slug + "/" + sv.Name,
					Detail:  fmt.Sprintf("project %q: env var %q not found on destination", sp.Slug, sv.Name),
				})
			} else {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMatched,
					Section: "Projects",
					Name:    sp.Slug + "/" + sv.Name,
					Detail:  "env var name present",
				})
			}
		}

		// Compare key advanced settings.
		compareProjectSettings(sp, dp, destSlug, &sec)

		// Compare additional SSH-key fingerprint presence.
		compareProjectSSHKeys(sp, dp, &sec)

		// Checkout key presence.
		if len(sp.CheckoutKeys) > 0 {
			if len(dp.CheckoutKeys) == 0 {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMissing,
					Section: "Projects",
					Name:    destSlug + "/checkout-keys",
					Detail:  fmt.Sprintf("project %q: %d checkout key(s) on source but none on destination", sp.Slug, len(sp.CheckoutKeys)),
				})
			} else {
				sec.Items = append(sec.Items, Item{
					Status:  StatusMatched,
					Section: "Projects",
					Name:    destSlug + "/checkout-keys",
					Detail:  fmt.Sprintf("checkout keys present (%d on dest vs %d on source)", len(dp.CheckoutKeys), len(sp.CheckoutKeys)),
				})
			}
		}
	}

	return sec
}

func compareProjectSettings(sp, dp manifest.Project, destSlug string, sec *Section) {
	if sp.Settings == nil {
		return
	}
	if dp.Settings == nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Projects",
			Name:    destSlug + "/advanced-settings",
			Detail:  fmt.Sprintf("project %q: source has advanced settings but destination has none", sp.Slug),
		})
		return
	}
	// Compare key bool settings.
	type boolSetting struct {
		name string
		src  *bool
		dst  *bool
	}
	settings := []boolSetting{
		{"autocancel_builds", sp.Settings.AutocancelBuilds, dp.Settings.AutocancelBuilds},
		{"build_fork_prs", sp.Settings.BuildForkPRs, dp.Settings.BuildForkPRs},
		{"build_prs_only", sp.Settings.BuildPRsOnly, dp.Settings.BuildPRsOnly},
		{"disable_ssh", sp.Settings.DisableSSH, dp.Settings.DisableSSH},
		{"forks_receive_secret_env_vars", sp.Settings.ForksReceiveSecretEnvVars, dp.Settings.ForksReceiveSecretEnvVars},
		{"set_github_status", sp.Settings.SetGitHubStatus, dp.Settings.SetGitHubStatus},
		{"setup_workflows", sp.Settings.SetupWorkflows, dp.Settings.SetupWorkflows},
		{"write_settings_requires_admin", sp.Settings.WriteSettingsRequiresAdmin, dp.Settings.WriteSettingsRequiresAdmin},
	}
	for _, s := range settings {
		if s.src == nil {
			continue // not set on source, nothing to check
		}
		name := destSlug + "/settings/" + s.name
		if s.dst == nil || *s.src != *s.dst {
			dstVal := "not set"
			if s.dst != nil {
				dstVal = fmt.Sprintf("%t", *s.dst)
			}
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Projects",
				Name:    name,
				Detail:  fmt.Sprintf("project %q: setting %s differs (source=%t, dest=%s)", sp.Slug, s.name, *s.src, dstVal),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Projects",
				Name:    name,
				Detail:  fmt.Sprintf("setting %s=%t matches", s.name, *s.src),
			})
		}
	}
}

func compareProjectSSHKeys(sp, dp manifest.Project, sec *Section) {
	if len(sp.SSHKeys) == 0 {
		return
	}
	// Index dest fingerprints.
	dstFPs := make(map[string]struct{}, len(dp.SSHKeys))
	for _, k := range dp.SSHKeys {
		if k.Fingerprint != "" {
			dstFPs[k.Fingerprint] = struct{}{}
		}
	}
	for _, sk := range sp.SSHKeys {
		name := sp.Slug + "/ssh-key:" + sk.Fingerprint
		if sk.Fingerprint == "" {
			sec.Items = append(sec.Items, Item{
				Status:  StatusManual,
				Section: "Projects",
				Name:    sp.Slug + "/ssh-key:unknown",
				Detail:  fmt.Sprintf("project %q: SSH key with no fingerprint — verify manually", sp.Slug),
			})
			continue
		}
		if _, found := dstFPs[sk.Fingerprint]; !found {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Projects",
				Name:    name,
				Detail:  fmt.Sprintf("project %q: additional SSH key %q not found on destination", sp.Slug, sk.Fingerprint),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Projects",
				Name:    name,
				Detail:  "SSH key fingerprint present",
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Org settings
// ---------------------------------------------------------------------------

func compareOrgSettings(src, dst *manifest.Manifest) Section {
	sec := Section{Name: "Org Settings"}

	ss := src.Source.Org.Settings
	ds := dst.Source.Org.Settings

	if ss == nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Org Settings",
			Name:    "org-settings",
			Detail:  "source has no captured org settings — nothing to compare",
		})
		return sec
	}
	if ds == nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "org-settings",
			Detail:  "destination has no captured org settings but source has settings",
		})
		return sec
	}

	// SSO: always manual (requires DNS verification + IdP setup).
	if ss.SSO != nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "Org Settings",
			Name:    "sso",
			Detail:  fmt.Sprintf("SSO (SAML) is configured on source (enforced=%t, realm=%s) — must be reconfigured manually on the destination (DNS domain verification + IdP/SAML app setup)", ss.SSO.Enforced, orDash(ss.SSO.Realm)),
		})
	}

	// Feature flags: compare key flags.
	compareFeatureFlags(ss, ds, &sec)

	// OIDC custom claims.
	compareOIDCClaims(ss, ds, &sec)

	// URL-orb allow list.
	compareURLOrbAllowList(ss, ds, &sec)

	// Config policies.
	compareConfigPolicies(ss, ds, &sec)

	// Storage retention.
	compareStorageRetention(ss, ds, &sec)

	// Release tracker.
	compareReleaseTracker(ss, ds, &sec)

	// Contacts.
	compareContacts(ss, ds, &sec)

	// OTel exporters — values are masked; just compare count.
	compareOTelExporters(ss, ds, &sec)

	// Audit log configs — always manual (environment-specific ARNs).
	if len(ss.AuditLogConfigs) > 0 {
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "Org Settings",
			Name:    "audit-log-configs",
			Detail:  fmt.Sprintf("%d audit log config(s) on source — these are environment-specific (S3 ARN/bucket/region) and must be recreated manually on the destination", len(ss.AuditLogConfigs)),
		})
	}

	// Block unregistered users.
	if ss.BlockUnregisteredUsers != nil {
		if ds.BlockUnregisteredUsers == nil || *ss.BlockUnregisteredUsers != *ds.BlockUnregisteredUsers {
			dstVal := "not set"
			if ds.BlockUnregisteredUsers != nil {
				dstVal = fmt.Sprintf("%t", *ds.BlockUnregisteredUsers)
			}
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    "block-unregistered-users",
				Detail:  fmt.Sprintf("block_unregistered_users differs (source=%t, dest=%s)", *ss.BlockUnregisteredUsers, dstVal),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    "block-unregistered-users",
				Detail:  fmt.Sprintf("block_unregistered_users=%t matches", *ss.BlockUnregisteredUsers),
			})
		}
	}

	// Environment hierarchy — always manual.
	if ss.EnvironmentHierarchy != nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "Org Settings",
			Name:    "environment-hierarchy",
			Detail:  fmt.Sprintf("environment hierarchy %q is configured on source — cannot be auto-migrated (requires destination deploy-integration IDs)", ss.EnvironmentHierarchy.Name),
		})
	}

	return sec
}

func compareFeatureFlags(ss, ds *manifest.OrgSettings, sec *Section) {
	if len(ss.FeatureFlags) == 0 {
		return
	}
	// Compare each source flag against the destination; destination flags that
	// are absent default to false (CircleCI default).
	for k, sv := range ss.FeatureFlags {
		dv := ds.FeatureFlags[k] // zero value false when absent
		name := "feature-flag/" + k
		if sv == dv {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("feature flag %s=%t matches", k, sv),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("feature flag %s differs (source=%t, dest=%t)", k, sv, dv),
			})
		}
	}
}

func compareOIDCClaims(ss, ds *manifest.OrgSettings, sec *Section) {
	if len(ss.OIDCAudience) == 0 && ss.OIDCTTL == "" {
		return
	}
	srcAud := strings.Join(ss.OIDCAudience, ",")
	dstAud := strings.Join(ds.OIDCAudience, ",")
	if srcAud == dstAud && ss.OIDCTTL == ds.OIDCTTL {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Org Settings",
			Name:    "oidc-claims",
			Detail:  "OIDC custom claims match",
		})
	} else {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "oidc-claims",
			Detail:  fmt.Sprintf("OIDC custom claims differ (source audience=%s ttl=%s; dest audience=%s ttl=%s)", srcAud, orDash(ss.OIDCTTL), dstAud, orDash(ds.OIDCTTL)),
		})
	}
}

func compareURLOrbAllowList(ss, ds *manifest.OrgSettings, sec *Section) {
	if len(ss.URLOrbAllowList) == 0 {
		return
	}
	// Build dest set by Name+Prefix.
	dstSet := make(map[string]struct{}, len(ds.URLOrbAllowList))
	for _, e := range ds.URLOrbAllowList {
		dstSet[e.Name+"|"+e.Prefix] = struct{}{}
	}
	for _, se := range ss.URLOrbAllowList {
		key := se.Name + "|" + se.Prefix
		name := "url-orb-allow/" + se.Name
		if _, ok := dstSet[key]; ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("URL-orb allow entry %q present", se.Name),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("URL-orb allow entry %q (prefix %s) not found on destination", se.Name, se.Prefix),
			})
		}
	}
}

func compareConfigPolicies(ss, ds *manifest.OrgSettings, sec *Section) {
	if len(ss.ConfigPolicies) == 0 && ss.PolicyEnforcementEnabled == nil {
		return
	}
	// Compare enforcement toggle.
	if ss.PolicyEnforcementEnabled != nil {
		srcEnf := *ss.PolicyEnforcementEnabled
		dstEnf := false
		if ds.PolicyEnforcementEnabled != nil {
			dstEnf = *ds.PolicyEnforcementEnabled
		}
		if srcEnf == dstEnf {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    "config-policy-enforcement",
				Detail:  fmt.Sprintf("config policy enforcement=%t matches", srcEnf),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    "config-policy-enforcement",
				Detail:  fmt.Sprintf("config policy enforcement differs (source=%t, dest=%t)", srcEnf, dstEnf),
			})
		}
	}
	// Compare policy names.
	for name := range ss.ConfigPolicies {
		itemName := "config-policy/" + name
		if _, ok := ds.ConfigPolicies[name]; ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    itemName,
				Detail:  fmt.Sprintf("config policy %q present", name),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    itemName,
				Detail:  fmt.Sprintf("config policy %q not found on destination", name),
			})
		}
	}
}

func compareStorageRetention(ss, ds *manifest.OrgSettings, sec *Section) {
	sr := ss.StorageRetention
	dr := ds.StorageRetention
	if sr == nil {
		return
	}
	if dr == nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "storage-retention",
			Detail:  "storage retention is set on source but not captured on destination",
		})
		return
	}
	diffs := []string{}
	if sr.CacheDays != dr.CacheDays {
		diffs = append(diffs, fmt.Sprintf("cache %d→%d", sr.CacheDays, dr.CacheDays))
	}
	if sr.WorkspaceDays != dr.WorkspaceDays {
		diffs = append(diffs, fmt.Sprintf("workspace %d→%d", sr.WorkspaceDays, dr.WorkspaceDays))
	}
	if sr.ArtifactDays != dr.ArtifactDays {
		diffs = append(diffs, fmt.Sprintf("artifact %d→%d", sr.ArtifactDays, dr.ArtifactDays))
	}
	if len(diffs) == 0 {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Org Settings",
			Name:    "storage-retention",
			Detail:  fmt.Sprintf("storage retention matches (cache=%d workspace=%d artifact=%d days)", sr.CacheDays, sr.WorkspaceDays, sr.ArtifactDays),
		})
	} else {
		// Differences may be due to plan limits clamping — flag as manual not missing.
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "Org Settings",
			Name:    "storage-retention",
			Detail:  fmt.Sprintf("storage retention differs (%s) — destination plan limits may clamp values; verify in CircleCI UI", strings.Join(diffs, ", ")),
		})
	}
}

func compareReleaseTracker(ss, ds *manifest.OrgSettings, sec *Section) {
	if ss.ReleaseTracker == nil {
		return
	}
	if ds.ReleaseTracker == nil {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "release-tracker",
			Detail:  fmt.Sprintf("release tracker TTL %q is set on source but not found on destination", ss.ReleaseTracker.InconclusiveReleaseTTL),
		})
		return
	}
	if ss.ReleaseTracker.InconclusiveReleaseTTL == ds.ReleaseTracker.InconclusiveReleaseTTL {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Org Settings",
			Name:    "release-tracker",
			Detail:  fmt.Sprintf("release tracker TTL=%q matches", ss.ReleaseTracker.InconclusiveReleaseTTL),
		})
	} else {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "release-tracker",
			Detail:  fmt.Sprintf("release tracker TTL differs (source=%q, dest=%q)", ss.ReleaseTracker.InconclusiveReleaseTTL, ds.ReleaseTracker.InconclusiveReleaseTTL),
		})
	}
}

func compareContacts(ss, ds *manifest.OrgSettings, sec *Section) {
	if ss.Contacts == nil {
		return
	}
	srcPrimary := strings.Join(ss.Contacts.Primary, ",")
	srcSecurity := strings.Join(ss.Contacts.Security, ",")
	dstPrimary := ""
	dstSecurity := ""
	if ds.Contacts != nil {
		dstPrimary = strings.Join(ds.Contacts.Primary, ",")
		dstSecurity = strings.Join(ds.Contacts.Security, ",")
	}
	if srcPrimary == dstPrimary && srcSecurity == dstSecurity {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMatched,
			Section: "Org Settings",
			Name:    "contacts",
			Detail:  "org contacts match",
		})
	} else {
		sec.Items = append(sec.Items, Item{
			Status:  StatusMissing,
			Section: "Org Settings",
			Name:    "contacts",
			Detail:  fmt.Sprintf("org contacts differ (source primary=%s security=%s; dest primary=%s security=%s)", srcPrimary, srcSecurity, dstPrimary, dstSecurity),
		})
	}
}

func compareOTelExporters(ss, ds *manifest.OrgSettings, sec *Section) {
	if len(ss.OTelExporters) == 0 {
		return
	}
	// Values are masked; compare by endpoint+protocol presence.
	dstSet := make(map[string]struct{}, len(ds.OTelExporters))
	for _, e := range ds.OTelExporters {
		dstSet[e.Endpoint+"|"+e.Protocol] = struct{}{}
	}
	for _, se := range ss.OTelExporters {
		key := se.Endpoint + "|" + se.Protocol
		name := "otel/" + se.Endpoint
		if _, ok := dstSet[key]; ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("OTel exporter endpoint %q present (header values are masked and cannot be compared)", se.Endpoint),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Org Settings",
				Name:    name,
				Detail:  fmt.Sprintf("OTel exporter endpoint %q not found on destination", se.Endpoint),
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Runner resource classes
// ---------------------------------------------------------------------------

func compareRunners(src, dst *manifest.Manifest, destRunnerNamespace string) Section {
	sec := Section{Name: "Runner Resource Classes"}

	if len(src.RunnerResourceClasses) == 0 {
		sec.Skipped = true
		sec.SkipReason = "source manifest has no runner resource classes"
		return sec
	}
	if destRunnerNamespace == "" {
		sec.Skipped = true
		sec.SkipReason = "pass --dest-runner-namespace to enable runner resource-class comparison"
		return sec
	}

	// Index dest classes by short name (segment after "/").
	dstByShort := make(map[string]struct{}, len(dst.RunnerResourceClasses))
	for _, rc := range dst.RunnerResourceClasses {
		dstByShort[shortName(rc.Name)] = struct{}{}
	}

	for _, src := range src.RunnerResourceClasses {
		short := shortName(src.Name)
		name := "runner/" + src.Name
		if _, ok := dstByShort[short]; ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Runner Resource Classes",
				Name:    name,
				Detail:  fmt.Sprintf("runner class %q (short name %q) present on destination", src.Name, short),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Runner Resource Classes",
				Name:    name,
				Detail:  fmt.Sprintf("runner class %q not found in destination namespace %q", src.Name, destRunnerNamespace),
			})
		}
	}
	return sec
}

// ---------------------------------------------------------------------------
// Orbs
// ---------------------------------------------------------------------------

func compareOrbs(src, dst *manifest.Manifest, destOrbNamespace string) Section {
	sec := Section{Name: "Orbs"}

	if len(src.Orbs) == 0 {
		sec.Skipped = true
		sec.SkipReason = "source manifest has no captured orbs"
		return sec
	}
	if destOrbNamespace == "" {
		sec.Skipped = true
		sec.SkipReason = "pass --dest-orb-namespace to enable orb comparison"
		return sec
	}

	// Index dest orbs by short name (segment after "/").
	dstByShort := make(map[string]manifest.CapturedOrb, len(dst.Orbs))
	for _, o := range dst.Orbs {
		dstByShort[shortName(o.Name)] = o
	}

	for _, so := range src.Orbs {
		short := shortName(so.Name)
		name := "orb/" + so.Name
		do, ok := dstByShort[short]
		if !ok {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Orbs",
				Name:    name,
				Detail:  fmt.Sprintf("orb %q not found in destination namespace %q", so.Name, destOrbNamespace),
			})
			continue
		}
		// Check that all source versions are present on the destination.
		dstVersions := make(map[string]struct{}, len(do.Versions))
		for _, v := range do.Versions {
			dstVersions[v.Version] = struct{}{}
		}
		allPresent := true
		var missingVersions []string
		for _, sv := range so.Versions {
			if _, found := dstVersions[sv.Version]; !found {
				allPresent = false
				missingVersions = append(missingVersions, sv.Version)
			}
		}
		if allPresent {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMatched,
				Section: "Orbs",
				Name:    name,
				Detail:  fmt.Sprintf("orb %q present with all %d version(s)", so.Name, len(so.Versions)),
			})
		} else {
			sec.Items = append(sec.Items, Item{
				Status:  StatusMissing,
				Section: "Orbs",
				Name:    name,
				Detail:  fmt.Sprintf("orb %q: missing versions on destination: %s", so.Name, strings.Join(missingVersions, ", ")),
			})
		}
	}
	return sec
}

// ---------------------------------------------------------------------------
// CIAM
// ---------------------------------------------------------------------------

func compareCIAM(src, dst *manifest.Manifest) Section {
	sec := Section{Name: "CIAM"}

	if src.CIAM == nil {
		sec.Skipped = true
		sec.SkipReason = "source manifest has no CIAM data (not a standalone circleci-type org)"
		return sec
	}

	// CIAM role bindings require matching user identities by email; UUIDs differ
	// between orgs. Surface as manual with a comparison of counts.
	srcRoles := len(src.CIAM.OrgRoles)
	dstRoles := 0
	if dst.CIAM != nil {
		dstRoles = len(dst.CIAM.OrgRoles)
	}
	sec.Items = append(sec.Items, Item{
		Status:  StatusManual,
		Section: "CIAM",
		Name:    "ciam/org-roles",
		Detail:  fmt.Sprintf("CIAM org roles: source has %d, destination has %d — verify role bindings manually (user UUIDs differ between orgs; bindings must be confirmed by email)", srcRoles, dstRoles),
	})

	srcGroups := len(src.CIAM.Groups)
	dstGroups := 0
	if dst.CIAM != nil {
		dstGroups = len(dst.CIAM.Groups)
	}
	if srcGroups > 0 {
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "CIAM",
			Name:    "ciam/groups",
			Detail:  fmt.Sprintf("CIAM groups: source has %d, destination has %d — verify group definitions and memberships manually", srcGroups, dstGroups),
		})
	}

	srcProjGrants := len(src.CIAM.ProjectUserGrants) + len(src.CIAM.ProjectGroupGrants)
	if srcProjGrants > 0 {
		sec.Items = append(sec.Items, Item{
			Status:  StatusManual,
			Section: "CIAM",
			Name:    "ciam/project-grants",
			Detail:  fmt.Sprintf("CIAM project-level grants: source has %d user grant(s) and %d group grant(s) — verify manually", len(src.CIAM.ProjectUserGrants), len(src.CIAM.ProjectGroupGrants)),
		})
	}

	return sec
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// shortName returns the segment after the last "/" in a namespaced name such
// as "acme/my-runner" or "acme/my-orb". Returns the full string when there
// is no slash.
func shortName(s string) string {
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

// slugLastComponent returns the last "/" component of a slug (repo name).
func slugLastComponent(slug string) string {
	if idx := strings.LastIndex(slug, "/"); idx >= 0 {
		return slug[idx+1:]
	}
	return slug
}

// orDash returns s, or "—" when s is empty.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
