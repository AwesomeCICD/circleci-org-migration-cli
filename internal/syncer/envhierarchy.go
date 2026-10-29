package syncer

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// envHierarchyAPI is the private release-tracker surface SyncEnvironmentHierarchies
// needs. The org client implements it; it is asserted optionally (like
// deployReader) so a Syncer whose Org resolver lacks it degrades to "manual".
type envHierarchyAPI interface {
	ListBFFEnvironments(ctx context.Context, orgUUID string) ([]org.BFFEnvironment, error)
	CreateEnvironment(ctx context.Context, orgUUID, name, integrationType, description string) (string, error)
	ListEnvironmentHierarchies(ctx context.Context, orgUUID string) ([]org.EnvHierarchySummary, error)
	GetEnvironmentHierarchyDetail(ctx context.Context, id string) (*org.EnvHierarchyDetail, error)
	ListEnvironmentHierarchyLevels(ctx context.Context, id string) ([]org.EnvHierarchyLevelRef, error)
	ResolveOrgEnvironmentHierarchy(ctx context.Context, orgUUID string) (scope, id, name string, err error)
	CreateEnvironmentHierarchy(ctx context.Context, orgUUID string, in org.EnvHierarchyCreate) (string, error)
	UpsertEnvironmentHierarchyAssignment(ctx context.Context, hierarchyID, entityType string, entityIDs []string) error
	ListDeployComponents(ctx context.Context, orgUUID string) ([]org.DeployComponent, error)
}

const (
	kindHierarchy   = "env-hierarchy"
	kindEnvironment = "env-hierarchy-environment"

	// newMarker stands in for the id of something a dry run would create.
	newMarker = "<new>"

	// assignBatch bounds one assignment PUT (the server enforces a maximum
	// batch size; stay well below any sensible limit).
	assignBatch = 25
)

// knownIntegrationTypes are the integration_type values the BFF accepts.
var knownIntegrationTypes = map[string]bool{
	"KUBERNETES_CLUSTER":       true,
	"APPLE_DEVELOPER_ACCOUNT":  true,
	"GOOGLE_DEVELOPER_ACCOUNT": true,
	"CUSTOM":                   true,
}

// integrationTypeFor maps a source environment kind (public API, e.g. "custom")
// to the BFF integration_type. An unknown kind falls back to CUSTOM; the second
// result says whether the fallback was used for a non-empty kind.
func integrationTypeFor(kind string) (string, bool) {
	k := strings.ToUpper(strings.TrimSpace(kind))
	if k == "" {
		return "CUSTOM", false
	}
	if knownIntegrationTypes[k] {
		return k, false
	}
	return "CUSTOM", true
}

// hierarchyDefs returns the hierarchies to sync: the captured list, or — for a
// manifest from an older export that only recorded the org default under org
// settings — that single hierarchy as the org default.
func hierarchyDefs(m *manifest.Manifest) []manifest.EnvironmentHierarchyDef {
	if len(m.EnvironmentHierarchies) > 0 {
		return m.EnvironmentHierarchies
	}
	if s := m.Source.Org.Settings; s != nil && s.EnvironmentHierarchy != nil {
		h := s.EnvironmentHierarchy
		return []manifest.EnvironmentHierarchyDef{{
			Name: h.Name, Description: h.Description, IsDefault: true, Levels: h.Levels,
		}}
	}
	return nil
}

// HasEnvironmentHierarchies reports whether the manifest carries any environment
// hierarchy to sync (the captured list or a legacy org-default entry).
func HasEnvironmentHierarchies(m *manifest.Manifest) bool { return len(hierarchyDefs(m)) > 0 }

// hierarchySync carries the per-run state of SyncEnvironmentHierarchies.
type hierarchySync struct {
	s         *Syncer
	api       envHierarchyAPI
	report    *Report
	orgID     string
	apply     bool
	denied    bool   // a write was rejected (401/403/404): everything else degrades to manual
	deniedMsg string // the first rejection, shown on each degraded item

	envs  map[string]string // destination environment name -> integration id
	hiers map[string]string // destination hierarchy name -> id

	orgScope, orgDefaultID, orgDefaultName string

	projs         map[string]string // destination project name -> id
	projsErr      error
	projsLoaded   bool
	comps         []org.DeployComponent
	compsErr      error
	compsLoaded   bool
	deployKindMap map[string]manifest.DeployEnvironment
}

// SyncEnvironmentHierarchies recreates the manifest's environment hierarchies in
// the destination org. It must run AFTER the projects sync (PROJECT assignments
// match destination projects by name) and before SyncDeploys.
//
// Per hierarchy: the environments (deploy integrations) its levels reference
// are created by name when missing — copying kind and description from the
// source — then the hierarchy is created with levels mapped by environment
// name, and PROJECT/COMPONENT assignments are applied by matching names.
// It is idempotent: an existing hierarchy of the same name is compared level by
// level (identical → "exists", different → "manual", never modified); an
// already-assigned project/component is skipped. A component that does not exist
// yet is "manual" (it is created by the first release). The private BFF write
// endpoints are unverified for personal tokens, so the first 401/403/404 on a
// write turns everything remaining into "manual". A dry run only reads.
func (s *Syncer) SyncEnvironmentHierarchies(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	defs := hierarchyDefs(m)
	if len(defs) == 0 {
		return report, nil
	}
	s.logf("Reconciling %d environment hierarchy(ies) → %s%s", len(defs), destSlug, dryRunSuffix(opts.Apply))

	manual := func(why string) {
		for _, d := range defs {
			report.add(kindHierarchy, d.Name, "manual", fmt.Sprintf(
				"environment hierarchy %q (levels: %s) was not recreated: %s — create it in the destination (Deploys → Environment hierarchies) "+
					"using environments of the same names", d.Name, levelNames(d.Levels), why))
		}
	}

	destOrgID, err := s.Org.ResolveOrgID(ctx, destSlug)
	if err != nil {
		manual(fmt.Sprintf("could not resolve destination org %q: %v", destSlug, err))
		return report, nil
	}
	report.DestOrgID = destOrgID

	api, ok := s.Org.(envHierarchyAPI)
	if !ok {
		manual("this client cannot manage environment hierarchies")
		return report, nil
	}

	h := &hierarchySync{s: s, api: api, report: report, orgID: destOrgID, apply: opts.Apply,
		envs: map[string]string{}, hiers: map[string]string{}, deployKindMap: map[string]manifest.DeployEnvironment{}}
	if m.Deploys != nil {
		for _, e := range m.Deploys.Environments {
			h.deployKindMap[e.Name] = e
		}
	}

	envs, err := api.ListBFFEnvironments(ctx, destOrgID)
	if err != nil {
		manual(fmt.Sprintf("could not list destination environments: %v", err))
		return report, nil
	}
	for _, e := range envs {
		h.envs[e.Name] = e.ID
	}
	hiers, err := api.ListEnvironmentHierarchies(ctx, destOrgID)
	if err != nil {
		manual(fmt.Sprintf("could not list destination environment hierarchies: %v", err))
		return report, nil
	}
	for _, e := range hiers {
		h.hiers[e.Name] = e.ID
	}
	if scope, id, name, rerr := api.ResolveOrgEnvironmentHierarchy(ctx, destOrgID); rerr != nil {
		clog.Debugf("could not resolve destination default environment hierarchy: %v", rerr)
	} else {
		h.orgScope, h.orgDefaultID, h.orgDefaultName = scope, id, name
	}

	h.ensureEnvironments(ctx, defs)
	for _, d := range defs {
		h.syncOne(ctx, d, mapping)
	}
	return report, nil
}

func levelNames(levels []manifest.EnvHierarchyLevel) string {
	ls := append([]manifest.EnvHierarchyLevel(nil), levels...)
	sort.SliceStable(ls, func(i, j int) bool { return ls[i].Position < ls[j].Position })
	parts := make([]string, 0, len(ls))
	for _, l := range ls {
		parts = append(parts, fmt.Sprintf("%d:%s", l.Position, l.IntegrationName))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// denyWrite records a rejected write and returns the manual detail for it.
func (h *hierarchySync) denyWrite(err error) {
	if h.denied {
		return
	}
	h.denied = true
	h.deniedMsg = fmt.Sprintf("the private release-tracker API rejected this token's write (%v); personal API tokens may not be able to write environments/hierarchies", err)
}

// ensureEnvironments creates every environment the levels reference that the
// destination lacks.
func (h *hierarchySync) ensureEnvironments(ctx context.Context, defs []manifest.EnvironmentHierarchyDef) {
	seen := map[string]bool{}
	for _, d := range defs {
		levels := append([]manifest.EnvHierarchyLevel(nil), d.Levels...)
		sort.SliceStable(levels, func(i, j int) bool { return levels[i].Position < levels[j].Position })
		for _, l := range levels {
			name := l.IntegrationName
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			if _, ok := h.envs[name]; ok {
				h.report.add(kindEnvironment, name, "exists", fmt.Sprintf("environment %q already exists in the destination", name))
				continue
			}
			src := h.deployKindMap[name]
			typ, fellBack := integrationTypeFor(src.Kind)
			note := ""
			if fellBack {
				note = fmt.Sprintf(" (source kind %q is not a known integration type; created as CUSTOM)", src.Kind)
			}
			if typ != "CUSTOM" {
				note += " — its settings (cluster/account connection) are not copied; reconnect it in the destination"
			}
			if !h.apply {
				h.report.add(kindEnvironment, name, "created", fmt.Sprintf("would create environment %q (type %s)%s", name, typ, note))
				h.envs[name] = newMarker
				continue
			}
			if h.denied {
				h.report.add(kindEnvironment, name, "manual", fmt.Sprintf("environment %q not created: %s — create it by hand (Deploys → Environments)", name, h.deniedMsg))
				continue
			}
			id, err := h.api.CreateEnvironment(ctx, h.orgID, name, typ, src.Description)
			if err != nil {
				if org.IsBFFWriteDenied(err) {
					h.denyWrite(err)
					h.report.add(kindEnvironment, name, "manual", fmt.Sprintf("environment %q not created: %s — create it by hand (Deploys → Environments)", name, h.deniedMsg))
					continue
				}
				if org.IsBFFConflict(err) {
					h.report.add(kindEnvironment, name, "manual", fmt.Sprintf("environment %q could not be created (name conflict): %v — check Deploys → Environments, then re-run", name, err))
					continue
				}
				h.report.add(kindEnvironment, name, "error", fmt.Sprintf("create environment %q: %v", name, err))
				continue
			}
			h.envs[name] = id
			h.report.add(kindEnvironment, name, "created", fmt.Sprintf("created environment %q (type %s)%s", name, typ, note))
		}
	}
}

// sameLevels reports whether the destination levels equal the wanted ones
// (position and environment name, in position order) and describes a mismatch.
func sameLevels(want []manifest.EnvHierarchyLevel, have []org.EnvHierarchyLevelRef) (bool, string) {
	w := append([]manifest.EnvHierarchyLevel(nil), want...)
	sort.SliceStable(w, func(i, j int) bool { return w[i].Position < w[j].Position })
	hv := append([]org.EnvHierarchyLevelRef(nil), have...)
	sort.SliceStable(hv, func(i, j int) bool { return hv[i].Position < hv[j].Position })
	var haveStr []string
	for _, l := range hv {
		haveStr = append(haveStr, fmt.Sprintf("%d:%s", l.Position, l.IntegrationName))
	}
	if len(w) != len(hv) {
		return false, fmt.Sprintf("destination has %v, the manifest has %s", haveStr, levelNames(want))
	}
	for i := range w {
		if w[i].Position != hv[i].Position || w[i].IntegrationName != hv[i].IntegrationName {
			return false, fmt.Sprintf("destination has %v, the manifest has %s", haveStr, levelNames(want))
		}
	}
	return true, ""
}

func (h *hierarchySync) syncOne(ctx context.Context, d manifest.EnvironmentHierarchyDef, mapping *manifest.Mapping) {
	target := d.Name
	levels := append([]manifest.EnvHierarchyLevel(nil), d.Levels...)
	sort.SliceStable(levels, func(i, j int) bool { return levels[i].Position < levels[j].Position })

	hierID, ok := h.hiers[d.Name]
	switch {
	case ok:
		have, err := h.api.ListEnvironmentHierarchyLevels(ctx, hierID)
		if err != nil {
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf(
				"environment hierarchy %q exists in the destination but its levels could not be read (%v) — verify them by hand; assignments were not applied", d.Name, err))
			return
		}
		if same, diff := sameLevels(levels, have); !same {
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf(
				"environment hierarchy %q already exists in the destination with different levels (%s); it was NOT modified and its assignments were NOT applied — align it manually (or delete it) and re-run sync", d.Name, diff))
			return
		}
		h.report.add(kindHierarchy, target, "exists", fmt.Sprintf("environment hierarchy %q already exists with the same levels", d.Name))
	default:
		var ok2 bool
		hierID, ok2 = h.createHierarchy(ctx, d, levels)
		if !ok2 {
			return
		}
	}

	h.assignDefault(ctx, d, hierID, ok)
	h.assignProjects(ctx, d, hierID)
	h.assignComponents(ctx, d, hierID)
}

// createHierarchy creates the hierarchy (or plans to) and returns its id.
func (h *hierarchySync) createHierarchy(ctx context.Context, d manifest.EnvironmentHierarchyDef, levels []manifest.EnvHierarchyLevel) (string, bool) {
	target := d.Name
	create := org.EnvHierarchyCreate{Name: d.Name, Description: d.Description}
	var missing []string
	for _, l := range levels {
		id, ok := h.envs[l.IntegrationName]
		if !ok || id == "" {
			missing = append(missing, l.IntegrationName)
		}
		create.Levels = append(create.Levels, org.EnvHierarchyLevelRef{Position: l.Position, IntegrationID: id, IntegrationName: l.IntegrationName})
	}
	if len(missing) > 0 {
		h.report.add(kindHierarchy, target, "manual", fmt.Sprintf(
			"environment hierarchy %q not created: environment(s) %v are not available in the destination (see the environment actions above) — create them, then re-run sync", d.Name, missing))
		return "", false
	}

	create.IsDefault = d.IsDefault
	if d.IsDefault && h.orgScope == "ORGANIZATION" && h.orgDefaultID != "" {
		create.IsDefault = false
		h.report.add(kindHierarchy, target+"/default", "manual", fmt.Sprintf(
			"environment hierarchy %q is the org default in the source, but the destination's default is already %q — it was not replaced; change it in the destination if you want %q", d.Name, h.orgDefaultName, d.Name))
	}

	if !h.apply {
		h.report.add(kindHierarchy, target, "created", fmt.Sprintf("would create environment hierarchy %q (levels: %s%s)", d.Name, levelNames(d.Levels), defaultNote(create.IsDefault)))
		return newMarker, true
	}
	if h.denied {
		h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("environment hierarchy %q not created: %s — create it by hand (Deploys → Environment hierarchies)", d.Name, h.deniedMsg))
		return "", false
	}
	id, err := h.api.CreateEnvironmentHierarchy(ctx, h.orgID, create)
	if err != nil {
		switch {
		case org.IsBFFWriteDenied(err):
			h.denyWrite(err)
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("environment hierarchy %q not created: %s — create it by hand (Deploys → Environment hierarchies)", d.Name, h.deniedMsg))
		case org.IsBFFConflict(err):
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("environment hierarchy %q could not be created (duplicate name, level position or environment): %v — check the destination and re-run", d.Name, err))
		default:
			h.report.add(kindHierarchy, target, "error", fmt.Sprintf("create environment hierarchy %q: %v", d.Name, err))
		}
		return "", false
	}
	h.hiers[d.Name] = id
	if create.IsDefault {
		h.orgScope, h.orgDefaultID, h.orgDefaultName = "ORGANIZATION", id, d.Name
	}
	h.report.add(kindHierarchy, target, "created", fmt.Sprintf("created environment hierarchy %q (levels: %s%s)", d.Name, levelNames(d.Levels), defaultNote(create.IsDefault)))
	return id, true
}

func defaultNote(isDefault bool) string {
	if isDefault {
		return "; org default"
	}
	return ""
}

// assignDefault makes an EXISTING hierarchy the org default when the source had
// it as the default and the destination has none. (A newly created hierarchy
// gets is_default at create time.)
func (h *hierarchySync) assignDefault(ctx context.Context, d manifest.EnvironmentHierarchyDef, hierID string, existed bool) {
	if !d.IsDefault || !existed {
		return
	}
	target := d.Name + "/default"
	switch {
	case h.orgScope == "ORGANIZATION" && h.orgDefaultID == hierID:
		h.report.add(kindHierarchy, target, "exists", fmt.Sprintf("environment hierarchy %q is already the destination's org default", d.Name))
	case h.orgScope == "ORGANIZATION" && h.orgDefaultID != "":
		h.report.add(kindHierarchy, target, "manual", fmt.Sprintf(
			"environment hierarchy %q is the org default in the source, but the destination's default is %q — not replaced", d.Name, h.orgDefaultName))
	default:
		h.assign(ctx, target, hierID, "ORGANIZATION", []string{h.orgID}, fmt.Sprintf("org default %q", d.Name))
	}
}

// assign performs (or plans) one assignment upsert.
func (h *hierarchySync) assign(ctx context.Context, target, hierID, entityType string, ids []string, what string) {
	if !h.apply || hierID == newMarker {
		h.report.add(kindHierarchy, target, "set", fmt.Sprintf("would assign %s", what))
		return
	}
	if h.denied {
		h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("%s not assigned: %s — assign it by hand", what, h.deniedMsg))
		return
	}
	for start := 0; start < len(ids); start += assignBatch {
		end := start + assignBatch
		if end > len(ids) {
			end = len(ids)
		}
		if err := h.api.UpsertEnvironmentHierarchyAssignment(ctx, hierID, entityType, ids[start:end]); err != nil {
			if org.IsBFFWriteDenied(err) {
				h.denyWrite(err)
				h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("%s not assigned: %s — assign it by hand", what, h.deniedMsg))
			} else {
				h.report.add(kindHierarchy, target, "error", fmt.Sprintf("assign %s: %v", what, err))
			}
			return
		}
	}
	h.report.add(kindHierarchy, target, "set", fmt.Sprintf("assigned %s", what))
}

func (h *hierarchySync) assignedSets(ctx context.Context, hierID string) (projects, components map[string]bool) {
	projects, components = map[string]bool{}, map[string]bool{}
	if hierID == newMarker {
		return
	}
	d, err := h.api.GetEnvironmentHierarchyDetail(ctx, hierID)
	if err != nil {
		clog.Debugf("could not read existing assignments of hierarchy %s: %v", hierID, err)
		return
	}
	for _, id := range d.AssignedProjectIDs {
		projects[id] = true
	}
	for _, id := range d.AssignedComponentIDs {
		components[id] = true
	}
	return
}

func (h *hierarchySync) loadProjects(ctx context.Context) {
	if h.projsLoaded {
		return
	}
	h.projsLoaded = true
	h.projs = map[string]string{}
	if h.s.Projects == nil {
		h.projsErr = fmt.Errorf("no project client")
		return
	}
	ps, err := h.s.Projects.ListOrgProjects(ctx, h.orgID)
	if err != nil {
		h.projsErr = err
		return
	}
	for _, p := range ps {
		h.projs[p.Name] = p.ID
	}
}

func (h *hierarchySync) assignProjects(ctx context.Context, d manifest.EnvironmentHierarchyDef, hierID string) {
	if len(d.Projects) == 0 {
		return
	}
	h.loadProjects(ctx)
	already, _ := h.assignedSets(ctx, hierID)
	var ids, names []string
	for _, ref := range d.Projects {
		target := d.Name + "/project:" + ref.Name
		id := h.projs[ref.Name]
		switch {
		case h.projsErr != nil:
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("project %q not assigned: could not list destination projects (%v) — assign hierarchy %q to it by hand", ref.Name, h.projsErr, d.Name))
		case id == "":
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("project %q not found in the destination — re-run sync after the project exists, or assign hierarchy %q to it by hand", ref.Name, d.Name))
		case already[id]:
			h.report.add(kindHierarchy, target, "exists", fmt.Sprintf("project %q is already assigned to hierarchy %q", ref.Name, d.Name))
		default:
			ids = append(ids, id)
			names = append(names, ref.Name)
		}
	}
	if len(ids) > 0 {
		h.assign(ctx, d.Name+"/projects", hierID, "PROJECT", ids, fmt.Sprintf("hierarchy %q to project(s) %s", d.Name, strings.Join(names, ", ")))
	}
}

func (h *hierarchySync) loadComponents(ctx context.Context) {
	if h.compsLoaded {
		return
	}
	h.compsLoaded = true
	h.comps, h.compsErr = h.api.ListDeployComponents(ctx, h.orgID)
}

func (h *hierarchySync) assignComponents(ctx context.Context, d manifest.EnvironmentHierarchyDef, hierID string) {
	if len(d.Components) == 0 {
		return
	}
	h.loadComponents(ctx)
	h.loadProjects(ctx)
	_, already := h.assignedSets(ctx, hierID)
	var ids, names []string
	for _, ref := range d.Components {
		label := ref.Name
		if ref.ProjectName != "" {
			label = ref.ProjectName + "/" + ref.Name
		}
		target := d.Name + "/component:" + label
		wantProj := ""
		if ref.ProjectName != "" {
			wantProj = h.projs[ref.ProjectName]
		}
		id := ""
		for _, c := range h.comps {
			if c.Name == ref.Name && c.ArchivedAt == "" && (ref.ProjectName == "" || (wantProj != "" && c.ProjectID == wantProj)) {
				id = c.ID
				break
			}
		}
		switch {
		case h.compsErr != nil:
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf("component %q not assigned: could not list destination components (%v) — assign hierarchy %q to it by hand", label, h.compsErr, d.Name))
		case id == "":
			h.report.add(kindHierarchy, target, "manual", fmt.Sprintf(
				"component %q does not exist in the destination yet — re-run sync after the first release creates the component (`circleci run release plan --component-name=%q`), or assign hierarchy %q to it by hand", label, ref.Name, d.Name))
		case already[id]:
			h.report.add(kindHierarchy, target, "exists", fmt.Sprintf("component %q is already assigned to hierarchy %q", label, d.Name))
		default:
			ids = append(ids, id)
			names = append(names, label)
		}
	}
	if len(ids) > 0 {
		h.assign(ctx, d.Name+"/components", hierID, "COMPONENT", ids, fmt.Sprintf("hierarchy %q to component(s) %s", d.Name, strings.Join(names, ", ")))
	}
}
