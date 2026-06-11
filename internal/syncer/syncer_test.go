package syncer

import (
	"errors"
	"strings"
	"testing"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// ---------------------------------------------------------------------------
// Fake implementations
// ---------------------------------------------------------------------------

// fakeOrgResolver is a configurable OrgResolver for testing.
type fakeOrgResolver struct {
	resolveOrgID    func(slug string) (string, error)
	getOrganization func(slug string) (*org.Organization, error)
}

func (f *fakeOrgResolver) ResolveOrgID(slug string) (string, error) {
	if f.resolveOrgID != nil {
		return f.resolveOrgID(slug)
	}
	return "org-uuid-" + slug, nil
}

func (f *fakeOrgResolver) GetOrganization(slug string) (*org.Organization, error) {
	if f.getOrganization != nil {
		return f.getOrganization(slug)
	}
	// Default: return a GitHub OAuth org so existing tests are unaffected.
	return &org.Organization{ID: "org-uuid-" + slug, Slug: slug, VCSType: "github"}, nil
}

// call records one call to a ContextWriter method.
type call struct {
	method string
	args   []string
}

// fakeContextWriter records all calls for later assertion.
type fakeContextWriter struct {
	listContexts      func(ownerID, ownerSlug string) ([]cctx.Context, error)
	createContext     func(name, ownerID string) (*cctx.Context, error)
	upsertEnvVar      func(contextID, name, value string) error
	listRestrictions  func(contextID string) ([]cctx.Restriction, error)
	createRestriction func(contextID, restrictionType, restrictionValue string) error
	calls             []call
}

func (f *fakeContextWriter) ListContexts(ownerID, ownerSlug string) ([]cctx.Context, error) {
	f.calls = append(f.calls, call{"ListContexts", []string{ownerID, ownerSlug}})
	if f.listContexts != nil {
		return f.listContexts(ownerID, ownerSlug)
	}
	return nil, nil
}

func (f *fakeContextWriter) CreateContext(name, ownerID string) (*cctx.Context, error) {
	f.calls = append(f.calls, call{"CreateContext", []string{name, ownerID}})
	if f.createContext != nil {
		return f.createContext(name, ownerID)
	}
	return &cctx.Context{ID: "new-ctx-" + name, Name: name}, nil
}

func (f *fakeContextWriter) UpsertEnvVar(contextID, name, value string) error {
	f.calls = append(f.calls, call{"UpsertEnvVar", []string{contextID, name, value}})
	if f.upsertEnvVar != nil {
		return f.upsertEnvVar(contextID, name, value)
	}
	return nil
}

func (f *fakeContextWriter) ListRestrictions(contextID string) ([]cctx.Restriction, error) {
	f.calls = append(f.calls, call{"ListRestrictions", []string{contextID}})
	if f.listRestrictions != nil {
		return f.listRestrictions(contextID)
	}
	return nil, nil
}

func (f *fakeContextWriter) CreateRestriction(contextID, restrictionType, restrictionValue string) error {
	f.calls = append(f.calls, call{"CreateRestriction", []string{contextID, restrictionType, restrictionValue}})
	if f.createRestriction != nil {
		return f.createRestriction(contextID, restrictionType, restrictionValue)
	}
	return nil
}

// hasCalled returns true if a method with the given name appears in calls.
func (f *fakeContextWriter) hasCalled(method string) bool {
	for _, c := range f.calls {
		if c.method == method {
			return true
		}
	}
	return false
}

// callsTo returns all recorded calls to the named method.
func (f *fakeContextWriter) callsTo(method string) []call {
	var out []call
	for _, c := range f.calls {
		if c.method == method {
			out = append(out, c)
		}
	}
	return out
}

// fakeGroupLister is a configurable GroupLister for testing.
type fakeGroupLister struct {
	listGroups func(orgID string) ([]Group, error)
	calls      int
}

func (f *fakeGroupLister) ListGroups(orgID string) ([]Group, error) {
	f.calls++
	if f.listGroups != nil {
		return f.listGroups(orgID)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// simpleManifest builds a one-context manifest for testing.
func simpleManifest(ctxName string, varNames ...string) *manifest.Manifest {
	var vars []manifest.ContextEnvVar
	for _, n := range varNames {
		vars = append(vars, manifest.ContextEnvVar{Name: n})
	}
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source: manifest.Source{
			Org: manifest.Org{Slug: "gh/src"},
		},
		Contexts: []manifest.Context{
			{Name: ctxName, EnvVars: vars},
		},
	}
}

// bundleWith builds a SecretBundle with values for one context.
func bundleWith(ctxName string, kvPairs ...string) *manifest.SecretBundle {
	b := manifest.NewSecretBundle()
	for i := 0; i+1 < len(kvPairs); i += 2 {
		b.SetContextSecret(ctxName, kvPairs[i], kvPairs[i+1])
	}
	return b
}

// mappingTo builds a Mapping pointing at a destination org slug.
func mappingTo(destSlug string) *manifest.Mapping {
	return &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/src", To: destSlug}}
}

// ---------------------------------------------------------------------------
// Dry-run tests
// ---------------------------------------------------------------------------

// TestSyncContexts_DryRun_MissingContext verifies that when Apply=false a
// context that does not exist in the destination produces a "created" action
// with detail "would create context" and no writes occur.
func TestSyncContexts_DryRun_MissingContext(t *testing.T) {
	fw := &fakeContextWriter{}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Contexts: fw,
	}

	m := simpleManifest("deploy-prod", "API_KEY")
	bundle := bundleWith("deploy-prod", "API_KEY", "s3cr3t")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Applied must be false for a dry run.
	if rep.Applied {
		t.Error("Report.Applied should be false for dry run")
	}

	// Find the context action.
	var ctxAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "context" {
			ctxAction = &rep.Actions[i]
			break
		}
	}
	if ctxAction == nil {
		t.Fatal("expected a context action, got none")
		return
	}
	if ctxAction.Status != "created" {
		t.Errorf("context action status: got %q want %q", ctxAction.Status, "created")
	}
	if ctxAction.Detail != "would create context" {
		t.Errorf("context action detail: got %q want %q", ctxAction.Detail, "would create context")
	}

	// No mutations should have occurred.
	if fw.hasCalled("CreateContext") {
		t.Error("CreateContext must NOT be called in dry-run mode")
	}
	if fw.hasCalled("UpsertEnvVar") {
		t.Error("UpsertEnvVar must NOT be called in dry-run mode")
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Existing context
// ---------------------------------------------------------------------------

// TestSyncContexts_ExistingContext verifies that a context already present in
// the destination produces an "exists" action and CreateContext is not called.
func TestSyncContexts_ExistingContext(t *testing.T) {
	existingCtx := cctx.Context{ID: "existing-id", Name: "deploy-prod"}
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{existingCtx}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("deploy-prod")
	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ctxAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "context" {
			ctxAction = &rep.Actions[i]
			break
		}
	}
	if ctxAction == nil {
		t.Fatal("expected a context action, got none")
		return
	}
	if ctxAction.Status != "exists" {
		t.Errorf("status: got %q want %q", ctxAction.Status, "exists")
	}
	if fw.hasCalled("CreateContext") {
		t.Error("CreateContext must NOT be called for an existing context")
	}
}

// ---------------------------------------------------------------------------
// Apply=true: context creation
// ---------------------------------------------------------------------------

// TestSyncContexts_ApplyTrue_CreatesContext verifies that with Apply=true a
// missing context causes CreateContext to be called and the returned ID is used
// for subsequent UpsertEnvVar calls.
func TestSyncContexts_ApplyTrue_CreatesContext(t *testing.T) {
	createdID := "new-ctx-id-123"
	fw := &fakeContextWriter{
		createContext: func(name, ownerID string) (*cctx.Context, error) {
			return &cctx.Context{ID: createdID, Name: name}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("deploy-prod", "API_KEY")
	bundle := bundleWith("deploy-prod", "API_KEY", "secretvalue")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !rep.Applied {
		t.Error("Report.Applied should be true when Apply=true")
	}

	// CreateContext should have been called.
	if !fw.hasCalled("CreateContext") {
		t.Fatal("CreateContext must be called for a missing context when Apply=true")
	}

	// UpsertEnvVar should use the created ID.
	upserts := fw.callsTo("UpsertEnvVar")
	if len(upserts) == 0 {
		t.Fatal("UpsertEnvVar must be called after context creation")
	}
	if upserts[0].args[0] != createdID {
		t.Errorf("UpsertEnvVar contextID: got %q want %q", upserts[0].args[0], createdID)
	}
}

// ---------------------------------------------------------------------------
// Env var from bundle
// ---------------------------------------------------------------------------

// TestSyncContexts_EnvVar_SetFromBundle verifies that a variable present in
// the bundle produces a "set" action and UpsertEnvVar is called with the bundle
// value when Apply=true.
func TestSyncContexts_EnvVar_SetFromBundle(t *testing.T) {
	ctxID := "ctx-id-abc"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "DB_PASS")
	bundle := bundleWith("prod", "DB_PASS", "hunter2")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Find the env-var action.
	var varAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "context-var" {
			varAction = &rep.Actions[i]
			break
		}
	}
	if varAction == nil {
		t.Fatal("expected a context-var action, got none")
		return
	}
	if varAction.Status != "set" {
		t.Errorf("status: got %q want %q", varAction.Status, "set")
	}

	upserts := fw.callsTo("UpsertEnvVar")
	if len(upserts) == 0 {
		t.Fatal("UpsertEnvVar must be called when Apply=true and value is available")
	}
	if upserts[0].args[0] != ctxID {
		t.Errorf("UpsertEnvVar contextID: got %q want %q", upserts[0].args[0], ctxID)
	}
	if upserts[0].args[1] != "DB_PASS" {
		t.Errorf("UpsertEnvVar name: got %q want %q", upserts[0].args[1], "DB_PASS")
	}
	if upserts[0].args[2] != "hunter2" {
		t.Errorf("UpsertEnvVar value: got %q want %q", upserts[0].args[2], "hunter2")
	}
}

// ---------------------------------------------------------------------------
// Missing secret: MissingSkip
// ---------------------------------------------------------------------------

// TestSyncContexts_MissingSecret_Skip verifies that a variable absent from the
// bundle with the MissingSkip policy produces a "manual" action and no write.
func TestSyncContexts_MissingSecret_Skip(t *testing.T) {
	ctxID := "ctx-skip"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MISSING_VAR")
	// No bundle values for MISSING_VAR.
	bundle := bundleWith("prod")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: true, MissingSecrets: MissingSkip})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var varAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "context-var" {
			varAction = &rep.Actions[i]
			break
		}
	}
	if varAction == nil {
		t.Fatal("expected a context-var action, got none")
		return
	}
	if varAction.Status != "manual" {
		t.Errorf("status: got %q want %q", varAction.Status, "manual")
	}
	if fw.hasCalled("UpsertEnvVar") {
		t.Error("UpsertEnvVar must NOT be called when MissingSecrets=skip")
	}
}

// ---------------------------------------------------------------------------
// Missing secret: MissingPlaceholder
// ---------------------------------------------------------------------------

// TestSyncContexts_MissingSecret_Placeholder verifies that a missing variable
// with the MissingPlaceholder policy produces a "set" action with Apply=true
// and UpsertEnvVar is called with DefaultPlaceholder.
func TestSyncContexts_MissingSecret_Placeholder(t *testing.T) {
	ctxID := "ctx-ph"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MISSING_VAR")
	bundle := bundleWith("prod")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: true, MissingSecrets: MissingPlaceholder})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var varAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "context-var" {
			varAction = &rep.Actions[i]
			break
		}
	}
	if varAction == nil {
		t.Fatal("expected a context-var action, got none")
		return
	}
	if varAction.Status != "set" {
		t.Errorf("status: got %q want %q", varAction.Status, "set")
	}

	upserts := fw.callsTo("UpsertEnvVar")
	if len(upserts) == 0 {
		t.Fatal("UpsertEnvVar must be called when MissingSecrets=placeholder and Apply=true")
	}
	if upserts[0].args[2] != DefaultPlaceholder {
		t.Errorf("UpsertEnvVar value: got %q want %q", upserts[0].args[2], DefaultPlaceholder)
	}
}

// ---------------------------------------------------------------------------
// Restrictions: expression
// ---------------------------------------------------------------------------

// TestSyncContexts_Restriction_Expression_Created verifies that an "expression"
// restriction on a new context produces a "set" action.
func TestSyncContexts_Restriction_Expression_Created(t *testing.T) {
	ctxID := "ctx-expr"
	expr := `project.slug == "gh/myorg/api"`
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, nil // no existing restrictions
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "expression", Value: expr},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "restriction" {
			rAction = &rep.Actions[i]
			break
		}
	}
	if rAction == nil {
		t.Fatal("expected a restriction action, got none")
		return
	}
	if rAction.Status != "set" {
		t.Errorf("restriction status: got %q want %q", rAction.Status, "set")
	}

	creates := fw.callsTo("CreateRestriction")
	if len(creates) == 0 {
		t.Fatal("CreateRestriction must be called for expression restriction when Apply=true")
	}
	if creates[0].args[1] != "expression" {
		t.Errorf("CreateRestriction type: got %q want %q", creates[0].args[1], "expression")
	}
	if creates[0].args[2] != expr {
		t.Errorf("CreateRestriction value: got %q want %q", creates[0].args[2], expr)
	}
}

// TestSyncContexts_Restriction_Expression_Exists verifies that an expression
// restriction already present in the destination produces an "exists" action
// and CreateRestriction is not called.
func TestSyncContexts_Restriction_Expression_Exists(t *testing.T) {
	ctxID := "ctx-expr-exists"
	expr := `project.slug == "gh/myorg/api"`
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return []cctx.Restriction{
				{Type: "expression", Value: expr},
			}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "expression", Value: expr},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "restriction" {
			rAction = &rep.Actions[i]
			break
		}
	}
	if rAction == nil {
		t.Fatal("expected a restriction action, got none")
		return
	}
	if rAction.Status != "exists" {
		t.Errorf("restriction status: got %q want %q", rAction.Status, "exists")
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called when restriction already exists")
	}
}

// TestSyncContexts_Restriction_ProjectAndGroup_Manual verifies that a "project"
// restriction always produces a "manual" action, and that a "group" restriction
// falls back to "manual" when no GroupLister is wired (Syncer.Groups == nil) —
// the nil-safe path preserving previous behaviour. Neither calls CreateRestriction.
func TestSyncContexts_Restriction_ProjectAndGroup_Manual(t *testing.T) {
	ctxID := "ctx-manual"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-1", Name: "web"},
					{Type: "group", Value: "group-uuid-1", Name: "sec-team"},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manualCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "restriction" && a.Status == "manual" {
			manualCount++
		}
	}
	if manualCount != 2 {
		t.Errorf("expected 2 manual restriction actions, got %d", manualCount)
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called for project or group restrictions")
	}
}

// ---------------------------------------------------------------------------
// Restrictions: group (resolved via GroupLister)
// ---------------------------------------------------------------------------

// groupRestrictionManifest builds a single-context manifest with one group
// restriction named groupName.
func groupRestrictionManifest(ctxName, groupName, sourceValue string) *manifest.Manifest {
	return &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: ctxName,
				Restrictions: []manifest.Restriction{
					{Type: "group", Value: sourceValue, Name: groupName},
				},
			},
		},
	}
}

// groupRestrictionAction returns the single restriction action from the report.
func groupRestrictionAction(t *testing.T, rep *Report) Action {
	t.Helper()
	for _, a := range rep.Actions {
		if a.Kind == "restriction" {
			return a
		}
	}
	t.Fatalf("no restriction action found in report: %+v", rep.Actions)
	return Action{}
}

func TestSyncContexts_GroupRestriction_AllMembers_UsesDestOrgID(t *testing.T) {
	const ctxID = "ctx-grp"
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) { return nil, nil },
	}
	gl := &fakeGroupLister{
		listGroups: func(string) ([]Group, error) {
			t.Error("ListGroups must NOT be called for the All members group")
			return nil, nil
		},
	}
	// fakeOrgResolver returns "org-uuid-" + slug → "org-uuid-gh/dest".
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := groupRestrictionManifest("prod", "All members", "src-org-id")
	rep, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := groupRestrictionAction(t, rep)
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}
	creates := fw.callsTo("CreateRestriction")
	if len(creates) != 1 {
		t.Fatalf("expected 1 CreateRestriction call, got %d", len(creates))
	}
	if creates[0].args[1] != "group" {
		t.Errorf("restriction type: got %q want %q", creates[0].args[1], "group")
	}
	if creates[0].args[2] != "org-uuid-gh/dest" {
		t.Errorf("restriction value: got %q want dest org id %q", creates[0].args[2], "org-uuid-gh/dest")
	}
}

func TestSyncContexts_GroupRestriction_NamedGroup_FoundResolvesUUID(t *testing.T) {
	const ctxID = "ctx-grp"
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) { return nil, nil },
	}
	gl := &fakeGroupLister{
		listGroups: func(orgID string) ([]Group, error) {
			return []Group{
				{ID: "dest-grp-uuid", Name: "sec-team"},
				{ID: "other-uuid", Name: "platform"},
			}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := groupRestrictionManifest("prod", "sec-team", "src-grp-uuid")
	rep, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := groupRestrictionAction(t, rep)
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}
	creates := fw.callsTo("CreateRestriction")
	if len(creates) != 1 {
		t.Fatalf("expected 1 CreateRestriction call, got %d", len(creates))
	}
	if creates[0].args[1] != "group" || creates[0].args[2] != "dest-grp-uuid" {
		t.Errorf("CreateRestriction args: got (%q,%q) want (group,dest-grp-uuid)", creates[0].args[1], creates[0].args[2])
	}
}

func TestSyncContexts_GroupRestriction_NotFound_Manual(t *testing.T) {
	const ctxID = "ctx-grp"
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) { return nil, nil },
	}
	gl := &fakeGroupLister{
		listGroups: func(string) ([]Group, error) {
			return []Group{{ID: "other-uuid", Name: "platform"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := groupRestrictionManifest("prod", "sec-team", "src-grp-uuid")
	rep, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := groupRestrictionAction(t, rep)
	if a.Status != "manual" {
		t.Errorf("status: got %q want %q", a.Status, "manual")
	}
	if !strings.Contains(a.Detail, "not found in destination") {
		t.Errorf("detail %q should mention 'not found in destination'", a.Detail)
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called when the group is not found")
	}
}

func TestSyncContexts_GroupRestriction_Idempotent_SkipWhenPresent(t *testing.T) {
	const ctxID = "ctx-grp"
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) {
			return []cctx.Restriction{
				{Type: "group", Value: "dest-grp-uuid"},
			}, nil
		},
	}
	gl := &fakeGroupLister{
		listGroups: func(string) ([]Group, error) {
			return []Group{{ID: "dest-grp-uuid", Name: "sec-team"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := groupRestrictionManifest("prod", "sec-team", "src-grp-uuid")
	rep, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := groupRestrictionAction(t, rep)
	if a.Status != "exists" {
		t.Errorf("status: got %q want %q", a.Status, "exists")
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called when the group restriction already exists")
	}
}

func TestSyncContexts_GroupRestriction_DryRun_NoWrite(t *testing.T) {
	const ctxID = "ctx-grp"
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	gl := &fakeGroupLister{
		listGroups: func(string) ([]Group, error) {
			return []Group{{ID: "dest-grp-uuid", Name: "sec-team"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := groupRestrictionManifest("prod", "sec-team", "src-grp-uuid")
	rep, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a := groupRestrictionAction(t, rep)
	if a.Status != "set" {
		t.Errorf("status: got %q want %q", a.Status, "set")
	}
	if !strings.Contains(a.Detail, "would add") {
		t.Errorf("detail %q should mention 'would add'", a.Detail)
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called in dry-run mode")
	}
}

func TestSyncContexts_GroupRestriction_CacheLoadedOnce(t *testing.T) {
	fw := &fakeContextWriter{
		listContexts: func(string, string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: "c1", Name: "a"}, {ID: "c2", Name: "b"}}, nil
		},
		listRestrictions: func(string) ([]cctx.Restriction, error) { return nil, nil },
	}
	gl := &fakeGroupLister{
		listGroups: func(string) ([]Group, error) {
			return []Group{{ID: "dest-grp-uuid", Name: "sec-team"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw, Groups: gl}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{Name: "a", Restrictions: []manifest.Restriction{{Type: "group", Name: "sec-team"}}},
			{Name: "b", Restrictions: []manifest.Restriction{{Type: "group", Name: "sec-team"}}},
		},
	}
	if _, err := sy.SyncContexts(m, nil, mappingTo("gh/dest"), Options{Apply: true}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gl.calls != 1 {
		t.Errorf("ListGroups should be called exactly once (cached per run), got %d", gl.calls)
	}
}

// ---------------------------------------------------------------------------
// Mapping: destination slug
// ---------------------------------------------------------------------------

// TestSyncContexts_MappingUsedForDestSlug verifies that the destination slug
// from Mapping.Org.To is passed to ResolveOrgID.
func TestSyncContexts_MappingUsedForDestSlug(t *testing.T) {
	var resolvedSlug string
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			resolvedSlug = slug
			return "dest-org-id", nil
		},
	}
	fw := &fakeContextWriter{}
	sy := &Syncer{Org: fr, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
	}
	mapping := mappingTo("gh/dest-org")

	_, err := sy.SyncContexts(m, nil, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedSlug != "gh/dest-org" {
		t.Errorf("ResolveOrgID slug: got %q want %q", resolvedSlug, "gh/dest-org")
	}
}

// TestSyncContexts_NilMapping_FallsBackToSourceSlug verifies that when no
// mapping is provided the source org slug is used as the destination.
func TestSyncContexts_NilMapping_FallsBackToSourceSlug(t *testing.T) {
	var resolvedSlug string
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			resolvedSlug = slug
			return "src-org-id", nil
		},
	}
	fw := &fakeContextWriter{}
	sy := &Syncer{Org: fr, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/source-org"}},
	}

	_, err := sy.SyncContexts(m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedSlug != "gh/source-org" {
		t.Errorf("ResolveOrgID slug: got %q want %q", resolvedSlug, "gh/source-org")
	}
}

// ---------------------------------------------------------------------------
// Error paths
// ---------------------------------------------------------------------------

// TestSyncContexts_ResolveOrgIDError_ReturnsError verifies that a ResolveOrgID
// failure bubbles up as an error from SyncContexts.
func TestSyncContexts_ResolveOrgIDError_ReturnsError(t *testing.T) {
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			return "", errors.New("network failure")
		},
	}
	sy := &Syncer{Org: fr, Contexts: &fakeContextWriter{}}

	_, err := sy.SyncContexts(simpleManifest("prod"), nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error from ResolveOrgID failure, got nil")
	}
}

// TestSyncContexts_ListContextsError_ReturnsError verifies that a ListContexts
// failure is returned as an error.
func TestSyncContexts_ListContextsError_ReturnsError(t *testing.T) {
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return nil, errors.New("list contexts API down")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	_, err := sy.SyncContexts(simpleManifest("prod"), nil, nil, Options{})
	if err == nil {
		t.Fatal("expected error from ListContexts failure, got nil")
	}
}

// TestSyncContexts_CreateRestrictionError_IsErrorAction verifies that a
// CreateRestriction failure is recorded as an "error" action and does not cause
// a panic or a top-level error return.
func TestSyncContexts_CreateRestrictionError_IsErrorAction(t *testing.T) {
	ctxID := "ctx-rerr"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, nil
		},
		createRestriction: func(contextID, restrictionType, restrictionValue string) error {
			return errors.New("restriction write failed")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "expression", Value: `project.slug == "gh/org/repo"`},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("write error must not propagate from SyncContexts, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' action when CreateRestriction fails, got none")
	}
}

// TestSyncContexts_UpsertEnvVarError_IsErrorAction verifies that a
// UpsertEnvVar failure is recorded as an "error" action, not a top-level error.
func TestSyncContexts_UpsertEnvVarError_IsErrorAction(t *testing.T) {
	ctxID := "ctx-varerr"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		upsertEnvVar: func(contextID, name, value string) error {
			return errors.New("upsert failed")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MY_VAR")
	bundle := bundleWith("prod", "MY_VAR", "val")

	rep, err := sy.SyncContexts(m, bundle, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("write error must not propagate from SyncContexts, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' action when UpsertEnvVar fails, got none")
	}
}

// ---------------------------------------------------------------------------
// Report.Counts
// ---------------------------------------------------------------------------

// TestReport_Counts verifies that Counts() returns correct per-status tallies.
func TestReport_Counts(t *testing.T) {
	r := &Report{}
	r.add("context", "ctx1", "created", "")
	r.add("context-var", "ctx1/VAR1", "set", "")
	r.add("context-var", "ctx1/VAR2", "set", "")
	r.add("context-var", "ctx1/VAR3", "manual", "")
	r.add("restriction", "ctx1 [project]", "manual", "")
	r.add("restriction", "ctx1 [expression]", "error", "")

	counts := r.Counts()
	tests := []struct {
		status string
		want   int
	}{
		{"created", 1},
		{"set", 2},
		{"manual", 2},
		{"error", 1},
		{"exists", 0},
	}
	for _, tt := range tests {
		got := counts[tt.status]
		if got != tt.want {
			t.Errorf("Counts()[%q]: got %d want %d", tt.status, got, tt.want)
		}
	}
}

// TestReport_Counts_Empty verifies Counts on an empty report returns an empty
// (non-nil) map.
func TestReport_Counts_Empty(t *testing.T) {
	r := &Report{}
	c := r.Counts()
	if c == nil {
		t.Error("Counts() must return a non-nil map")
	}
	if len(c) != 0 {
		t.Errorf("Counts() on empty report: got %v want empty map", c)
	}
}

// ---------------------------------------------------------------------------
// Dry run: restriction handling
// ---------------------------------------------------------------------------

// TestSyncContexts_DryRun_ExpressionRestriction_Set verifies that in dry-run
// mode an expression restriction produces a "set" action (would add) but no
// write.
func TestSyncContexts_DryRun_ExpressionRestriction_Set(t *testing.T) {
	fw := &fakeContextWriter{}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "expression", Value: `project.slug == "gh/org/api"`},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "restriction" {
			rAction = &rep.Actions[i]
			break
		}
	}
	if rAction == nil {
		t.Fatal("expected a restriction action in dry-run")
		return
	}
	if rAction.Status != "set" {
		t.Errorf("dry-run restriction status: got %q want %q", rAction.Status, "set")
	}
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// NilBundle: all vars treated as missing
// ---------------------------------------------------------------------------

// TestSyncContexts_NilBundle_AllVarsManual verifies that when no bundle is
// provided, all env vars are treated as missing and produce "manual" actions
// (with the default MissingSkip policy).
func TestSyncContexts_NilBundle_AllVarsManual(t *testing.T) {
	ctxID := "ctx-nb"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "VAR1", "VAR2")

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true, MissingSecrets: MissingSkip})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	manualCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "context-var" && a.Status == "manual" {
			manualCount++
		}
	}
	if manualCount != 2 {
		t.Errorf("expected 2 manual env-var actions with nil bundle, got %d", manualCount)
	}
}

// ---------------------------------------------------------------------------
// Custom placeholder
// ---------------------------------------------------------------------------

// TestSyncContexts_CustomPlaceholder verifies that Options.Placeholder overrides
// DefaultPlaceholder when MissingPlaceholder policy is in use.
func TestSyncContexts_CustomPlaceholder(t *testing.T) {
	ctxID := "ctx-cph"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MY_SECRET")
	bundle := bundleWith("prod") // no value for MY_SECRET

	rep, err := sy.SyncContexts(m, bundle, nil, Options{
		Apply:          true,
		MissingSecrets: MissingPlaceholder,
		Placeholder:    "TODO_FILL_IN",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = rep

	upserts := fw.callsTo("UpsertEnvVar")
	if len(upserts) == 0 {
		t.Fatal("UpsertEnvVar must be called when MissingPlaceholder is set")
	}
	if upserts[0].args[2] != "TODO_FILL_IN" {
		t.Errorf("UpsertEnvVar value: got %q want %q", upserts[0].args[2], "TODO_FILL_IN")
	}
}

// ---------------------------------------------------------------------------
// DestOrgSlug / DestOrgID in Report
// ---------------------------------------------------------------------------

// TestSyncContexts_Report_DestFields verifies the Report records the dest slug
// and ID returned by ResolveOrgID.
func TestSyncContexts_Report_DestFields(t *testing.T) {
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			return "resolved-org-id", nil
		},
	}
	fw := &fakeContextWriter{}
	sy := &Syncer{Org: fr, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
	}
	mapping := mappingTo("gh/dest")

	rep, err := sy.SyncContexts(m, nil, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.DestOrgSlug != "gh/dest" {
		t.Errorf("DestOrgSlug: got %q want %q", rep.DestOrgSlug, "gh/dest")
	}
	if rep.DestOrgID != "resolved-org-id" {
		t.Errorf("DestOrgID: got %q want %q", rep.DestOrgID, "resolved-org-id")
	}
}

// ---------------------------------------------------------------------------
// logf coverage: Out writer
// ---------------------------------------------------------------------------

// TestSyncer_Logf_WritesToOut verifies that log output is written to the Out
// writer when one is set on the Syncer.
func TestSyncer_Logf_WritesToOut(t *testing.T) {
	var buf strings.Builder
	fw := &fakeContextWriter{}
	sy := &Syncer{
		Org:      &fakeOrgResolver{},
		Contexts: fw,
		Out:      &buf,
	}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
	}

	_, err := sy.SyncContexts(m, nil, nil, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if buf.Len() == 0 {
		t.Error("expected log output written to Out, got empty")
	}
}

// ---------------------------------------------------------------------------
// CreateContext error: ensureContext returns error
// ---------------------------------------------------------------------------

// TestSyncContexts_CreateContextError_RecordsErrorAction verifies that a
// CreateContext failure is recorded as an "error" context action and
// does not propagate as a top-level error.
func TestSyncContexts_CreateContextError_RecordsErrorAction(t *testing.T) {
	fw := &fakeContextWriter{
		createContext: func(name, ownerID string) (*cctx.Context, error) {
			return nil, errors.New("create context API down")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("broken-ctx")

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("CreateContext error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Kind == "context" && a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' context action when CreateContext fails, got none")
	}
}

// ---------------------------------------------------------------------------
// restrictionLabel: named restriction
// ---------------------------------------------------------------------------

// TestSyncContexts_Restriction_NamedProject_LabelUsesName verifies that when a
// non-expression restriction has a Name field, the label in the detail uses
// the name (exercising the r.Name != "" branch of restrictionLabel).
func TestSyncContexts_Restriction_NamedProject_LabelUsesName(t *testing.T) {
	ctxID := "ctx-named-r"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "project", Value: "proj-uuid-abc", Name: "my-project"},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "restriction" {
			rAction = &rep.Actions[i]
			break
		}
	}
	if rAction == nil {
		t.Fatal("expected a restriction action, got none")
		return
	}
	if rAction.Status != "manual" {
		t.Errorf("restriction status: got %q want %q", rAction.Status, "manual")
	}
	// The detail should contain the restriction Name, not the UUID Value.
	if !strings.Contains(rAction.Detail, "my-project") {
		t.Errorf("detail %q should mention restriction name %q", rAction.Detail, "my-project")
	}
}

// ---------------------------------------------------------------------------
// MissingPlaceholder in dry-run (ctxID == "")
// ---------------------------------------------------------------------------

// TestSyncContexts_DryRun_Placeholder_NoUpsert verifies that even with the
// MissingPlaceholder policy, UpsertEnvVar is not called in dry-run mode
// (because the context ID is empty when the context would be created).
func TestSyncContexts_DryRun_Placeholder_NoUpsert(t *testing.T) {
	fw := &fakeContextWriter{} // no existing contexts
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MY_VAR")
	bundle := bundleWith("prod") // no value for MY_VAR

	_, err := sy.SyncContexts(m, bundle, nil, Options{
		Apply:          false,
		MissingSecrets: MissingPlaceholder,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fw.hasCalled("UpsertEnvVar") {
		t.Error("UpsertEnvVar must NOT be called in dry-run mode even with MissingPlaceholder")
	}
}

// ---------------------------------------------------------------------------
// Empty Org.To in mapping falls back to source slug
// ---------------------------------------------------------------------------

// TestSyncContexts_EmptyMappingOrgTo_FallsBackToSourceSlug verifies that when
// a Mapping is provided but Org.To is empty, the source org slug is used as
// the destination (exercises the destSlug == "" fallback branch).
func TestSyncContexts_EmptyMappingOrgTo_FallsBackToSourceSlug(t *testing.T) {
	var resolvedSlug string
	fr := &fakeOrgResolver{
		resolveOrgID: func(slug string) (string, error) {
			resolvedSlug = slug
			return "fallback-org-id", nil
		},
	}
	fw := &fakeContextWriter{}
	sy := &Syncer{Org: fr, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/source-fallback"}},
	}
	// Mapping with empty Org.To
	mapping := &manifest.Mapping{Org: manifest.OrgMapping{From: "gh/source-fallback", To: ""}}

	_, err := sy.SyncContexts(m, nil, mapping, Options{Apply: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolvedSlug != "gh/source-fallback" {
		t.Errorf("ResolveOrgID slug: got %q want %q", resolvedSlug, "gh/source-fallback")
	}
}

// ---------------------------------------------------------------------------
// restrictionLabel: unnamed restriction uses Value
// ---------------------------------------------------------------------------

// TestSyncContexts_Restriction_UnnamedProject_LabelUsesValue verifies that
// when a non-expression restriction has no Name, the detail uses the Value
// (exercises the else branch of restrictionLabel).
func TestSyncContexts_Restriction_UnnamedProject_LabelUsesValue(t *testing.T) {
	ctxID := "ctx-unnamed-r"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, nil
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					// Name is intentionally empty; label should fall back to Value.
					{Type: "project", Value: "proj-uuid-xyz", Name: ""},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rAction *Action
	for i := range rep.Actions {
		if rep.Actions[i].Kind == "restriction" {
			rAction = &rep.Actions[i]
			break
		}
	}
	if rAction == nil {
		t.Fatal("expected a restriction action, got none")
		return
	}
	if rAction.Status != "manual" {
		t.Errorf("restriction status: got %q want %q", rAction.Status, "manual")
	}
	// The detail should contain the UUID value when Name is empty.
	if !strings.Contains(rAction.Detail, "proj-uuid-xyz") {
		t.Errorf("detail %q should contain value %q when Name is empty", rAction.Detail, "proj-uuid-xyz")
	}
}

// ---------------------------------------------------------------------------
// Fix #1: ListRestrictions error → per-restriction error actions + skip context
// ---------------------------------------------------------------------------

// TestSyncContexts_ListRestrictions_Error_SkipsContext verifies that when
// ListRestrictions returns an error, each restriction for that context is
// recorded as an "error" action and CreateRestriction is NOT called.
// (Previously, the error was silently swallowed → empty existing list →
// unconditional CreateRestriction → duplicates on re-run.)
func TestSyncContexts_ListRestrictions_Error_SkipsContext(t *testing.T) {
	const ctxID = "ctx-lr-err"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		listRestrictions: func(contextID string) ([]cctx.Restriction, error) {
			return nil, errors.New("list restrictions API down")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := &manifest.Manifest{
		SchemaVersion: manifest.SchemaVersion,
		Source:        manifest.Source{Org: manifest.Org{Slug: "gh/src"}},
		Contexts: []manifest.Context{
			{
				Name: "prod",
				Restrictions: []manifest.Restriction{
					{Type: "expression", Value: `project.slug == "gh/org/api"`},
					{Type: "expression", Value: `project.slug == "gh/org/web"`},
				},
			},
		},
	}

	rep, err := sy.SyncContexts(m, nil, nil, Options{Apply: true})
	if err != nil {
		t.Fatalf("ListRestrictions error must not propagate as top-level error, got: %v", err)
	}

	// CreateRestriction must NOT be called (no duplicates on re-run).
	if fw.hasCalled("CreateRestriction") {
		t.Error("CreateRestriction must NOT be called when ListRestrictions fails")
	}

	// Each restriction should have an "error" action.
	errorCount := 0
	for _, a := range rep.Actions {
		if a.Kind == "restriction" && a.Status == "error" {
			errorCount++
		}
	}
	if errorCount != 2 {
		t.Errorf("expected 2 restriction 'error' actions (one per restriction), got %d", errorCount)
	}
}

// ---------------------------------------------------------------------------
// MissingPlaceholder UpsertEnvVar error path
// ---------------------------------------------------------------------------

// TestSyncContexts_Placeholder_UpsertError_IsErrorAction verifies that a
// UpsertEnvVar failure during placeholder write is recorded as an "error"
// action, not a top-level error.
func TestSyncContexts_Placeholder_UpsertError_IsErrorAction(t *testing.T) {
	ctxID := "ctx-ph-err"
	fw := &fakeContextWriter{
		listContexts: func(ownerID, ownerSlug string) ([]cctx.Context, error) {
			return []cctx.Context{{ID: ctxID, Name: "prod"}}, nil
		},
		upsertEnvVar: func(contextID, name, value string) error {
			return errors.New("upsert placeholder failed")
		},
	}
	sy := &Syncer{Org: &fakeOrgResolver{}, Contexts: fw}

	m := simpleManifest("prod", "MISSING_VAR")
	bundle := bundleWith("prod") // no value for MISSING_VAR

	rep, err := sy.SyncContexts(m, bundle, nil, Options{
		Apply:          true,
		MissingSecrets: MissingPlaceholder,
	})
	if err != nil {
		t.Fatalf("placeholder upsert error must not propagate, got: %v", err)
	}

	hasError := false
	for _, a := range rep.Actions {
		if a.Kind == "context-var" && a.Status == "error" {
			hasError = true
		}
	}
	if !hasError {
		t.Error("expected an 'error' context-var action when placeholder UpsertEnvVar fails")
	}
}
