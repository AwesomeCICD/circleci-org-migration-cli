package validate_test

import (
	"testing"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/validate"
)

// boolPtr returns a pointer to a bool — convenience for test setup.
func boolPtr(b bool) *bool { return &b }

// minimalManifest returns a valid, minimal manifest with the given slug.
func minimalManifest(slug string) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: "1",
		Source: manifest.Source{
			Host: "https://circleci.com",
			Org: manifest.Org{
				Slug: slug,
				Name: slug,
			},
		},
	}
}

// ---------------------------------------------------------------------------
// Compare — identical manifests
// ---------------------------------------------------------------------------

func TestCompare_IdenticalManifests_AllMatched(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{Name: "deploy", EnvVars: []manifest.ContextEnvVar{{Name: "KEY"}}},
	}
	src.Projects = []manifest.Project{
		{Slug: "gh/acme/web", EnvVars: []manifest.ProjectEnvVar{{Name: "DB_URL"}}},
	}
	src.Source.Org.Settings = &manifest.OrgSettings{
		FeatureFlags: map[string]bool{"build_github_notification": true},
	}

	// Deep-copy into dst with the same data.
	dst := minimalManifest("gh/acme-new")
	dst.Contexts = []manifest.Context{
		{Name: "deploy", EnvVars: []manifest.ContextEnvVar{{Name: "KEY"}}},
	}
	dst.Projects = []manifest.Project{
		{Slug: "gh/acme-new/web", EnvVars: []manifest.ProjectEnvVar{{Name: "DB_URL"}}},
	}
	dst.Source.Org.Settings = &manifest.OrgSettings{
		FeatureFlags: map[string]bool{"build_github_notification": true},
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if result.HasMissing() {
		t.Errorf("expected no missing items; totals: %s", result.TotalsLine())
		for _, s := range result.Sections {
			for _, item := range s.Items {
				if item.Status == validate.StatusMissing {
					t.Logf("  MISSING: %s / %s — %s", s.Name, item.Name, item.Detail)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Compare — missing context on destination
// ---------------------------------------------------------------------------

func TestCompare_MissingContext(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{Name: "deploy-prod", EnvVars: []manifest.ContextEnvVar{{Name: "TOKEN"}}},
	}

	dst := minimalManifest("gh/acme-new")
	// No contexts on destination.

	result := validate.Compare(src, dst, nil, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when a context is absent on destination")
	}
	// Find the missing item.
	found := false
	for _, s := range result.Sections {
		if s.Name != "Contexts" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "deploy-prod" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for context 'deploy-prod'")
	}
}

// ---------------------------------------------------------------------------
// Compare — missing env var in a context
// ---------------------------------------------------------------------------

func TestCompare_MissingContextEnvVar(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{Name: "ci", EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_A"}, {Name: "SECRET_B"}}},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Contexts = []manifest.Context{
		{Name: "ci", EnvVars: []manifest.ContextEnvVar{{Name: "SECRET_A"}}}, // SECRET_B missing
	}

	result := validate.Compare(src, dst, nil, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when an env var is absent")
	}
	found := false
	for _, s := range result.Sections {
		if s.Name != "Contexts" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "ci/SECRET_B" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for 'ci/SECRET_B'")
	}
}

// ---------------------------------------------------------------------------
// Compare — missing project on destination
// ---------------------------------------------------------------------------

func TestCompare_MissingProject(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{
		{Slug: "gh/acme/api", Name: "api"},
	}
	dst := minimalManifest("gh/acme-new")
	// No projects on destination.

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when a project is absent on destination")
	}
	found := false
	for _, s := range result.Sections {
		if s.Name != "Projects" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "gh/acme/api" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for project 'gh/acme/api'")
	}
}

// ---------------------------------------------------------------------------
// Compare — missing project env var
// ---------------------------------------------------------------------------

func TestCompare_MissingProjectEnvVar(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{
		{Slug: "gh/acme/web", Name: "web", EnvVars: []manifest.ProjectEnvVar{{Name: "DEPLOY_KEY"}, {Name: "API_URL"}}},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Projects = []manifest.Project{
		{Slug: "gh/acme-new/web", Name: "web", EnvVars: []manifest.ProjectEnvVar{{Name: "API_URL"}}}, // DEPLOY_KEY missing
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when a project env var is absent")
	}
	found := false
	for _, s := range result.Sections {
		if s.Name != "Projects" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "gh/acme/web/DEPLOY_KEY" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for 'gh/acme/web/DEPLOY_KEY'")
	}
}

// ---------------------------------------------------------------------------
// Compare — SSO always manual
// ---------------------------------------------------------------------------

func TestCompare_SSOAlwaysManual(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		SSO: &manifest.SSOSettings{Enforced: true, Realm: "acme-saml"},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})

	// SSO should NOT cause HasMissing — it's manual-only.
	if result.HasMissing() {
		// A HasMissing is acceptable only if something else is missing.
		// But SSO alone must not make it fail. Inspect.
		for _, s := range result.Sections {
			if s.Name == "Org Settings" {
				for _, item := range s.Items {
					if item.Status == validate.StatusMissing && item.Name == "sso" {
						t.Error("SSO should be StatusManual, not StatusMissing")
					}
				}
			}
		}
	}

	// Confirm SSO item is present as manual.
	found := false
	for _, s := range result.Sections {
		if s.Name != "Org Settings" {
			continue
		}
		for _, item := range s.Items {
			if item.Name == "sso" && item.Status == validate.StatusManual {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected SSO to appear as StatusManual in Org Settings")
	}
}

// ---------------------------------------------------------------------------
// Compare — settings diff flagged
// ---------------------------------------------------------------------------

func TestCompare_OrgSettingsDiff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		FeatureFlags: map[string]bool{"build_github_notification": true},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		FeatureFlags: map[string]bool{"build_github_notification": false},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when a feature flag differs")
	}
	found := false
	for _, s := range result.Sections {
		if s.Name != "Org Settings" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "feature-flag/build_github_notification" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for feature-flag/build_github_notification")
	}
}

// ---------------------------------------------------------------------------
// Compare — mapping applied correctly
// ---------------------------------------------------------------------------

func TestCompare_MappingApplied(t *testing.T) {
	// Source has gh/old/web; dest has gh/new/web; mapping maps the org.
	src := minimalManifest("gh/old")
	src.Projects = []manifest.Project{
		{Slug: "gh/old/web", Name: "web"},
	}
	dst := minimalManifest("gh/new")
	dst.Projects = []manifest.Project{
		{Slug: "gh/new/web", Name: "web"},
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/old", To: "gh/new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if result.HasMissing() {
		t.Errorf("with correct mapping, expected no missing items; totals: %s", result.TotalsLine())
	}
}

// TestCompare_MappingWithExplicitProjectSlug verifies that an explicit
// project slug in the mapping overrides the org-level derivation.
func TestCompare_MappingWithExplicitProjectSlug(t *testing.T) {
	src := minimalManifest("gh/old")
	src.Projects = []manifest.Project{
		{Slug: "gh/old/web", Name: "web"},
	}
	dst := minimalManifest("gh/new")
	dst.Projects = []manifest.Project{
		{Slug: "gh/new/website", Name: "website"}, // renamed repo
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/old", To: "gh/new"},
		Projects: map[string]string{
			"gh/old/web": "gh/new/website",
		},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if result.HasMissing() {
		t.Errorf("with explicit project mapping, expected no missing items; totals: %s", result.TotalsLine())
	}
}

// ---------------------------------------------------------------------------
// Compare — runners skipped without namespace flag
// ---------------------------------------------------------------------------

func TestCompare_RunnersSkipped_WhenNoDestNamespace(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.RunnerResourceClasses = []manifest.RunnerResourceClass{
		{Name: "acme/my-runner"},
	}
	dst := minimalManifest("gh/acme-new")

	result := validate.Compare(src, dst, nil, validate.Options{})

	for _, s := range result.Sections {
		if s.Name == "Runner Resource Classes" {
			if !s.Skipped {
				t.Error("expected Runner Resource Classes section to be Skipped when no --dest-runner-namespace")
			}
			return
		}
	}
	t.Error("runner section not found")
}

// ---------------------------------------------------------------------------
// Compare — runner matched with dest namespace
// ---------------------------------------------------------------------------

func TestCompare_RunnerMatched_WithDestNamespace(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.RunnerResourceClasses = []manifest.RunnerResourceClass{
		{Name: "acme/my-runner"},
	}
	dst := minimalManifest("gh/acme-new")
	dst.RunnerResourceClasses = []manifest.RunnerResourceClass{
		{Name: "acme-new/my-runner"},
	}

	result := validate.Compare(src, dst, nil, validate.Options{DestRunnerNamespace: "acme-new"})

	if result.HasMissing() {
		t.Errorf("runner class should match by short name; totals: %s", result.TotalsLine())
	}
}

// ---------------------------------------------------------------------------
// Compare — orbs skipped without namespace flag
// ---------------------------------------------------------------------------

func TestCompare_OrbsSkipped_WhenNoDestNamespace(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Orbs = []manifest.CapturedOrb{
		{Name: "acme/my-orb", Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0"}}},
	}
	dst := minimalManifest("gh/acme-new")

	result := validate.Compare(src, dst, nil, validate.Options{})

	for _, s := range result.Sections {
		if s.Name == "Orbs" {
			if !s.Skipped {
				t.Error("expected Orbs section to be Skipped when no --dest-orb-namespace")
			}
			return
		}
	}
	t.Error("orbs section not found")
}

// ---------------------------------------------------------------------------
// Compare — CIAM manual items present for standalone orgs
// ---------------------------------------------------------------------------

func TestCompare_CIAM_ManualWhenPresent(t *testing.T) {
	src := minimalManifest("circleci/some-uuid")
	src.CIAM = &manifest.CIAMData{
		OrgRoles: []manifest.CIAMOrgRole{{Email: "admin@example.com", Role: "org-admin"}},
		Groups:   []manifest.CIAMGroup{{Name: "developers"}},
	}
	dst := minimalManifest("circleci/other-uuid")
	// No CIAM on dest yet.

	result := validate.Compare(src, dst, nil, validate.Options{})

	// CIAM should NOT cause HasMissing (it's all manual).
	for _, s := range result.Sections {
		if s.Name == "CIAM" {
			for _, item := range s.Items {
				if item.Status == validate.StatusMissing {
					t.Errorf("CIAM item %q should be StatusManual not StatusMissing", item.Name)
				}
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Compare — CIAM skipped for VCS-type orgs
// ---------------------------------------------------------------------------

func TestCompare_CIAM_Skipped_WhenNoSourceData(t *testing.T) {
	src := minimalManifest("gh/acme")
	// No CIAM data — VCS-type org.
	dst := minimalManifest("gh/acme-new")

	result := validate.Compare(src, dst, nil, validate.Options{})

	for _, s := range result.Sections {
		if s.Name == "CIAM" {
			if !s.Skipped {
				t.Error("expected CIAM section to be Skipped when source has no CIAM data")
			}
			return
		}
	}
	t.Error("CIAM section not found")
}

// ---------------------------------------------------------------------------
// Compare — project settings diff
// ---------------------------------------------------------------------------

func TestCompare_ProjectSettingsDiff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{
		{
			Slug:     "gh/acme/web",
			Name:     "web",
			Settings: &manifest.AdvancedSettings{AutocancelBuilds: boolPtr(true)},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Projects = []manifest.Project{
		{
			Slug:     "gh/acme-new/web",
			Name:     "web",
			Settings: &manifest.AdvancedSettings{AutocancelBuilds: boolPtr(false)},
		},
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when autocancel_builds differs")
	}
}

// ---------------------------------------------------------------------------
// Compare — SSH key missing
// ---------------------------------------------------------------------------

func TestCompare_SSHKeyMissing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{
		{
			Slug: "gh/acme/web",
			Name: "web",
			SSHKeys: []manifest.ProjectSSHKey{
				{Fingerprint: "aa:bb:cc", Hostname: "github.com"},
			},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Projects = []manifest.Project{
		{Slug: "gh/acme-new/web", Name: "web"}, // No SSH keys.
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})

	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when SSH key is absent on destination")
	}
}

// ---------------------------------------------------------------------------
// TotalsLine
// ---------------------------------------------------------------------------

func TestResult_TotalsLine(t *testing.T) {
	r := validate.Result{
		Sections: []validate.Section{
			{
				Name: "Contexts",
				Items: []validate.Item{
					{Status: validate.StatusMatched},
					{Status: validate.StatusMissing},
					{Status: validate.StatusManual},
				},
			},
		},
	}
	line := r.TotalsLine()
	if line == "" {
		t.Error("TotalsLine should not be empty")
	}
	// Should contain the counts.
	for _, want := range []string{"1", "matched", "missing", "manual"} {
		if !containsSubstring(line, want) {
			t.Errorf("TotalsLine %q should contain %q", line, want)
		}
	}
}

// ---------------------------------------------------------------------------
// Compare — project restriction types
// ---------------------------------------------------------------------------

func TestCompare_ContextExpressionRestriction_Missing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{
			Name: "secure",
			Restrictions: []manifest.Restriction{
				{Type: "expression", Value: "pipeline.git.branch == 'main'"},
			},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Contexts = []manifest.Context{
		{Name: "secure"}, // No restrictions.
	}

	result := validate.Compare(src, dst, nil, validate.Options{})

	found := false
	for _, s := range result.Sections {
		if s.Name != "Contexts" {
			continue
		}
		for _, item := range s.Items {
			if item.Status == validate.StatusMissing && item.Name == "secure/restriction:expression" {
				found = true
			}
		}
	}
	if !found {
		t.Error("expected a missing item for expression restriction on context 'secure'")
	}
}

// ---------------------------------------------------------------------------
// Compare — OIDC claims diff
// ---------------------------------------------------------------------------

func TestCompare_OIDCClaimsDiff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		OIDCAudience: []string{"https://example.com"},
		OIDCTTL:      "1h",
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		OIDCAudience: []string{"https://other.com"},
		OIDCTTL:      "2h",
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when OIDC claims differ")
	}
}

func TestCompare_OIDCClaimsMatch(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		OIDCAudience: []string{"https://example.com"},
		OIDCTTL:      "1h",
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		OIDCAudience: []string{"https://example.com"},
		OIDCTTL:      "1h",
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when OIDC claims match")
	}
}

// ---------------------------------------------------------------------------
// Compare — URL orb allow list
// ---------------------------------------------------------------------------

func TestCompare_URLOrbAllowList_Missing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "my-orb", Prefix: "https://example.com"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when URL orb allow entry is absent")
	}
}

func TestCompare_URLOrbAllowList_Matched(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "my-orb", Prefix: "https://example.com"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		URLOrbAllowList: []manifest.URLOrbAllowEntry{
			{Name: "my-orb", Prefix: "https://example.com"},
		},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when URL orb allow list matches")
	}
}

// ---------------------------------------------------------------------------
// Compare — config policies
// ---------------------------------------------------------------------------

func TestCompare_ConfigPolicies_Missing(t *testing.T) {
	yes := true
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		ConfigPolicies:           map[string]string{"deny-all": "package org\ndefault allow = false"},
		PolicyEnforcementEnabled: &yes,
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when config policy is absent on destination")
	}
}

// ---------------------------------------------------------------------------
// Compare — storage retention
// ---------------------------------------------------------------------------

func TestCompare_StorageRetention_Diff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{CacheDays: 30, WorkspaceDays: 15, ArtifactDays: 60},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{CacheDays: 7, WorkspaceDays: 15, ArtifactDays: 60},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	// Storage retention diffs are StatusManual (not StatusMissing), so
	// HasMissing should be false but manual count > 0.
	_, _, manual := countAllItems(result)
	if manual == 0 {
		t.Error("expected at least one manual item for storage retention diff")
	}
}

func TestCompare_StorageRetention_Match(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{CacheDays: 30, WorkspaceDays: 15, ArtifactDays: 60},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{CacheDays: 30, WorkspaceDays: 15, ArtifactDays: 60},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when storage retention matches")
	}
}

func TestCompare_StorageRetention_NilOnDest(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		StorageRetention: &manifest.StorageRetentionControls{CacheDays: 30, WorkspaceDays: 15, ArtifactDays: 60},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when storage retention is set on source but nil on dest")
	}
}

// ---------------------------------------------------------------------------
// Compare — release tracker
// ---------------------------------------------------------------------------

func TestCompare_ReleaseTracker_Diff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "2h"},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when release tracker TTL differs")
	}
}

func TestCompare_ReleaseTracker_Match(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		ReleaseTracker: &manifest.ReleaseTrackerSettings{InconclusiveReleaseTTL: "1h"},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when release tracker TTL matches")
	}
}

// ---------------------------------------------------------------------------
// Compare — contacts
// ---------------------------------------------------------------------------

func TestCompare_Contacts_Match(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary:  []string{"admin@example.com"},
			Security: []string{"security@example.com"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary:  []string{"admin@example.com"},
			Security: []string{"security@example.com"},
		},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when contacts match")
	}
}

func TestCompare_Contacts_Diff(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary: []string{"admin@example.com"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		Contacts: &manifest.OrgContacts{
			Primary: []string{"other@example.com"},
		},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when contacts differ")
	}
}

// ---------------------------------------------------------------------------
// Compare — OTel exporters
// ---------------------------------------------------------------------------

func TestCompare_OTelExporters_Matched(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com", Protocol: "grpc"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com", Protocol: "grpc"},
		},
	}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if result.HasMissing() {
		t.Fatal("expected no missing items when OTel exporter matches")
	}
}

func TestCompare_OTelExporters_Missing(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		OTelExporters: []manifest.OTelExporter{
			{Endpoint: "https://otel.example.com", Protocol: "grpc"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when OTel exporter is absent on destination")
	}
}

// ---------------------------------------------------------------------------
// Compare — audit log configs (always manual)
// ---------------------------------------------------------------------------

func TestCompare_AuditLogConfigs_AlwaysManual(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		AuditLogConfigs: []manifest.AuditLogConfig{
			{ID: "cfg-1", Purpose: "security"},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})

	// Audit log configs are StatusManual, should not cause HasMissing.
	for _, s := range result.Sections {
		if s.Name != "Org Settings" {
			continue
		}
		for _, it := range s.Items {
			if it.Name == "audit-log-configs" && it.Status == validate.StatusMissing {
				t.Error("audit-log-configs should be StatusManual, not StatusMissing")
			}
		}
	}
	_, _, manual := countAllItems(result)
	if manual == 0 {
		t.Error("expected at least one manual item for audit log configs")
	}
}

// ---------------------------------------------------------------------------
// Compare — environment hierarchy (always manual)
// ---------------------------------------------------------------------------

func TestCompare_EnvironmentHierarchy_AlwaysManual(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Source.Org.Settings = &manifest.OrgSettings{
		EnvironmentHierarchy: &manifest.EnvironmentHierarchy{Name: "prod-hierarchy"},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Source.Org.Settings = &manifest.OrgSettings{}

	result := validate.Compare(src, dst, nil, validate.Options{})

	// Environment hierarchy is always StatusManual.
	found := false
	for _, s := range result.Sections {
		if s.Name != "Org Settings" {
			continue
		}
		for _, it := range s.Items {
			if it.Name == "environment-hierarchy" {
				if it.Status == validate.StatusMissing {
					t.Error("environment-hierarchy should be StatusManual, not StatusMissing")
				}
				if it.Status == validate.StatusManual {
					found = true
				}
			}
		}
	}
	if !found {
		t.Error("expected environment-hierarchy to appear as StatusManual in Org Settings")
	}
}

// ---------------------------------------------------------------------------
// Compare — orbs matched
// ---------------------------------------------------------------------------

func TestCompare_Orbs_Matched(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Orbs = []manifest.CapturedOrb{
		{Name: "acme/my-orb", Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Orbs = []manifest.CapturedOrb{
		{Name: "acme-new/my-orb", Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}},
	}

	result := validate.Compare(src, dst, nil, validate.Options{DestOrbNamespace: "acme-new"})
	if result.HasMissing() {
		t.Errorf("expected no missing items when orb versions all match; totals: %s", result.TotalsLine())
	}
}

func TestCompare_Orbs_MissingVersion(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Orbs = []manifest.CapturedOrb{
		{Name: "acme/my-orb", Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0"}, {Version: "1.1.0"}}},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Orbs = []manifest.CapturedOrb{
		{Name: "acme-new/my-orb", Versions: []manifest.CapturedOrbVersion{{Version: "1.0.0"}}}, // 1.1.0 missing
	}

	result := validate.Compare(src, dst, nil, validate.Options{DestOrbNamespace: "acme-new"})
	if !result.HasMissing() {
		t.Fatal("expected HasMissing=true when orb version 1.1.0 is absent on destination")
	}
}

// ---------------------------------------------------------------------------
// Compare — context group restriction (always manual)
// ---------------------------------------------------------------------------

func TestCompare_ContextGroupRestriction_AlwaysManual(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Contexts = []manifest.Context{
		{
			Name: "ci",
			Restrictions: []manifest.Restriction{
				{Type: "group", Value: "some-group-uuid"},
			},
		},
	}
	dst := minimalManifest("gh/acme-new")
	dst.Contexts = []manifest.Context{
		{Name: "ci"}, // No restrictions.
	}

	result := validate.Compare(src, dst, nil, validate.Options{})

	// Group restrictions are always manual.
	for _, s := range result.Sections {
		if s.Name != "Contexts" {
			continue
		}
		for _, it := range s.Items {
			if it.Name == "ci/restriction:group" && it.Status == validate.StatusMissing {
				t.Error("group restriction should be StatusManual, not StatusMissing")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Compare — project no settings on source
// ---------------------------------------------------------------------------

func TestCompare_ProjectNoSettings_NoItems(t *testing.T) {
	src := minimalManifest("gh/acme")
	src.Projects = []manifest.Project{
		{Slug: "gh/acme/web", Name: "web"}, // No settings.
	}
	dst := minimalManifest("gh/acme-new")
	dst.Projects = []manifest.Project{
		{Slug: "gh/acme-new/web", Name: "web"},
	}

	mapping := &manifest.Mapping{
		Org: manifest.OrgMapping{From: "gh/acme", To: "gh/acme-new"},
	}
	result := validate.Compare(src, dst, mapping, validate.Options{})
	if result.HasMissing() {
		t.Errorf("no settings on source should not produce missing items; totals: %s", result.TotalsLine())
	}
}

// ---------------------------------------------------------------------------
// Compare — source no org settings
// ---------------------------------------------------------------------------

func TestCompare_SourceNoOrgSettings_Matched(t *testing.T) {
	src := minimalManifest("gh/acme")
	// No org settings on source.
	dst := minimalManifest("gh/acme-new")

	result := validate.Compare(src, dst, nil, validate.Options{})

	// Should produce a "nothing to compare" matched item, not a missing.
	if result.HasMissing() {
		t.Errorf("source with no org settings should not produce missing; totals: %s", result.TotalsLine())
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsAt(s, sub))
}

func containsAt(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// countAllItems returns total matched, missing, manual across all sections.
func countAllItems(r validate.Result) (matched, missing, manual int) {
	for _, s := range r.Sections {
		m, ms, mn := s.Counts()
		matched += m
		missing += ms
		manual += mn
	}
	return
}
