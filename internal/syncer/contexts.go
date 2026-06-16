package syncer

import (
	"context"
	"fmt"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// SyncContexts recreates the manifest's contexts (and their captured variable
// values and restrictions) in the destination org. The destination org slug is
// mapping.Org.To.
func (s *Syncer) SyncContexts(ctx context.Context, m *manifest.Manifest, bundle *manifest.SecretBundle, mapping *manifest.Mapping, opts Options) (*Report, error) {
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
		return nil, fmt.Errorf("resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID
	s.logf("Destination org: %s (id %s)%s", destSlug, destOrgID, dryRunSuffix(opts.Apply))

	clog.Debugf("ListContexts dest_org_id=%s", destOrgID)
	existing, err := s.Contexts.ListContexts(ctx, destOrgID, "")
	if err != nil {
		return nil, fmt.Errorf("listing destination contexts: %w", err)
	}
	clog.Debugf("found %d existing context(s) in destination", len(existing))
	byName := map[string]cctx.Context{}
	for _, c := range existing {
		byName[c.Name] = c
	}

	// groupCache memoizes the destination group list (name → UUID) for the run.
	// nil until first needed; the bool guards a single lookup even on failure.
	var groupCache map[string]string
	groupCacheLoaded := false

	// Build a source-project-UUID → source-slug index once for the run.
	// Used by the deferred project-restriction pass to remap project restrictions.
	srcUUIDToSlug := buildSrcUUIDToSlug(m)

	// Reset any project restrictions deferred by a previous SyncContexts call on
	// this Syncer instance, then accumulate fresh ones for this run. They are
	// applied later by ApplyDeferredProjectRestrictions (after SyncProjects).
	s.deferredProjectRestrictions = nil

	for _, c := range m.Contexts {
		ctxID, err := s.ensureContext(ctx, report, c.Name, destOrgID, byName, opts)
		if err != nil {
			report.add("context", c.Name, "error", err.Error())
			continue
		}
		s.syncContextVars(ctx, report, c, bundle, ctxID, opts)
		s.syncContextRestrictions(ctx, report, c, ctxID, destOrgID, &groupCache, &groupCacheLoaded, srcUUIDToSlug, mapping, opts)
	}
	return report, nil
}

// buildSrcUUIDToSlug builds a map from source project UUID (SourceID) to slug
// for every project in the manifest that has a SourceID set. Used to remap
// project-type context restrictions from the source org's UUID to the
// destination project's slug / UUID.
func buildSrcUUIDToSlug(m *manifest.Manifest) map[string]string {
	out := make(map[string]string, len(m.Projects))
	for _, p := range m.Projects {
		if p.SourceID != "" {
			out[p.SourceID] = p.Slug
		}
	}
	return out
}

// ensureContext returns the destination context ID, creating it if absent.
// In dry-run mode a missing context yields an empty ID (nothing to write into).
func (s *Syncer) ensureContext(ctx context.Context, report *Report, name, destOrgID string, byName map[string]cctx.Context, opts Options) (string, error) {
	if c, ok := byName[name]; ok {
		report.add("context", name, "exists", "reusing existing context")
		return c.ID, nil
	}
	if !opts.Apply {
		report.add("context", name, "created", "would create context")
		return "", nil
	}
	created, err := s.Contexts.CreateContext(ctx, name, destOrgID)
	if err != nil {
		return "", err
	}
	report.add("context", name, "created", "created context")
	byName[name] = *created
	return created.ID, nil
}

func (s *Syncer) syncContextVars(ctx context.Context, report *Report, c manifest.Context, bundle *manifest.SecretBundle, ctxID string, opts Options) {
	values := map[string]string{}
	if bundle != nil {
		values = bundle.ContextSecrets[c.Name]
	}
	for _, v := range c.EnvVars {
		target := c.Name + "/" + v.Name
		val, ok := values[v.Name]
		if !ok {
			if opts.MissingSecrets == MissingPlaceholder {
				if err := s.writeVar(ctx, ctxID, v.Name, opts.placeholder(), opts.Apply); err != nil {
					report.add("context-var", target, "error", err.Error())
					continue
				}
				report.add("context-var", target, "set", "placeholder — value not captured; replace manually")
			} else {
				report.add("context-var", target, "manual", "value not captured; set manually")
			}
			continue
		}
		if err := s.writeVar(ctx, ctxID, v.Name, val, opts.Apply); err != nil {
			report.add("context-var", target, "error", err.Error())
			continue
		}
		report.add("context-var", target, "set", "value set from bundle")
	}
}

func (s *Syncer) writeVar(ctx context.Context, ctxID, name, value string, apply bool) error {
	if !apply || ctxID == "" {
		return nil // dry run, or context that would be created
	}
	return s.Contexts.UpsertEnvVar(ctx, ctxID, name, value)
}

func (s *Syncer) syncContextRestrictions(ctx context.Context, report *Report, c manifest.Context, ctxID, destOrgID string, groupCache *map[string]string, groupCacheLoaded *bool, srcUUIDToSlug map[string]string, mapping *manifest.Mapping, opts Options) {
	var existing []cctx.Restriction
	if opts.Apply && ctxID != "" {
		rs, err := s.Contexts.ListRestrictions(ctx, ctxID)
		if err != nil {
			for _, r := range c.Restrictions {
				target := c.Name + " [" + r.Type + "]"
				report.add("restriction", target, "error", fmt.Sprintf("list existing restrictions: %v", err))
			}
			return
		}
		existing = rs
	}
	for _, r := range c.Restrictions {
		target := c.Name + " [" + r.Type + "]"
		switch r.Type {
		case "expression":
			s.syncExpressionRestriction(ctx, report, target, ctxID, existing, r, opts)
		case "group":
			s.syncGroupRestriction(ctx, report, target, ctxID, destOrgID, existing, r, groupCache, groupCacheLoaded, opts)
		case "project":
			// Defer: the destination project does not exist yet (contexts sync
			// before projects). Collect the work and apply it after SyncProjects
			// via ApplyDeferredProjectRestrictions, so a single run resolves it.
			s.deferredProjectRestrictions = append(s.deferredProjectRestrictions, deferredProjectRestriction{
				target:        target,
				ctxID:         ctxID,
				existing:      existing,
				restriction:   r,
				srcUUIDToSlug: srcUUIDToSlug,
				mapping:       mapping,
			})
		default:
			// Unknown restriction type — manual handling.
			report.add("restriction", target, "manual", fmt.Sprintf("%s restriction %q must be recreated manually", r.Type, restrictionLabel(r)))
		}
	}
}

func (s *Syncer) syncExpressionRestriction(ctx context.Context, report *Report, target, ctxID string, existing []cctx.Restriction, r manifest.Restriction, opts Options) {
	if hasExpressionRestriction(existing, r.Value) {
		report.add("restriction", target, "exists", "expression restriction already present")
		return
	}
	if !opts.Apply || ctxID == "" {
		report.add("restriction", target, "set", "would add expression restriction")
		return
	}
	if err := s.Contexts.CreateRestriction(ctx, ctxID, "expression", r.Value); err != nil {
		report.add("restriction", target, "error", err.Error())
		return
	}
	report.add("restriction", target, "set", "added expression restriction")
}

// syncGroupRestriction resolves a source group restriction (captured by name) to
// a destination group UUID and recreates it. The special "All members" group's
// UUID equals the destination org id; other groups are matched by name against
// the destination group list. When no GroupLister is wired (s.Groups == nil) the
// restriction falls back to "manual", preserving the previous behaviour.
func (s *Syncer) syncGroupRestriction(ctx context.Context, report *Report, target, ctxID, destOrgID string, existing []cctx.Restriction, r manifest.Restriction, groupCache *map[string]string, groupCacheLoaded *bool, opts Options) {
	name := restrictionLabel(r)

	if s.Groups == nil {
		report.add("restriction", target, "manual", fmt.Sprintf("group restriction %q must be recreated manually", name))
		return
	}

	destUUID, resolved := s.resolveDestGroup(ctx, name, destOrgID, groupCache, groupCacheLoaded)
	if !resolved {
		report.add("restriction", target, "manual", fmt.Sprintf("group %q not found in destination — create it, then re-run", name))
		return
	}

	if hasGroupRestriction(existing, destUUID) {
		report.add("restriction", target, "exists", fmt.Sprintf("group restriction %q already present", name))
		return
	}
	if !opts.Apply || ctxID == "" {
		report.add("restriction", target, "set", fmt.Sprintf("would add group restriction %q", name))
		return
	}
	if err := s.Contexts.CreateRestriction(ctx, ctxID, "group", destUUID); err != nil {
		report.add("restriction", target, "error", err.Error())
		return
	}
	report.add("restriction", target, "set", fmt.Sprintf("added group restriction %q", name))
}

// resolveDestGroup returns the destination UUID for a group named name. The
// "All members" group resolves to the destination org id; other names are looked
// up in the destination group list (loaded once and cached for the run).
func (s *Syncer) resolveDestGroup(ctx context.Context, name, destOrgID string, groupCache *map[string]string, groupCacheLoaded *bool) (string, bool) {
	if name == "All members" {
		return destOrgID, true
	}
	if !*groupCacheLoaded {
		*groupCacheLoaded = true
		*groupCache = map[string]string{}
		if groups, err := s.Groups.ListGroups(ctx, destOrgID); err == nil {
			for _, g := range groups {
				(*groupCache)[g.Name] = g.ID
			}
		}
	}
	uuid, ok := (*groupCache)[name]
	return uuid, ok
}

// ApplyDeferredProjectRestrictions applies the project-type context restrictions
// that SyncContexts deferred. It MUST run AFTER SyncProjects, so the destination
// projects exist and can be resolved to UUIDs. Each restriction action is
// appended to report. When no project restrictions were deferred this is a
// no-op (and returns the report unchanged).
//
// The per-restriction remap path is:
//
//  1. source project UUID (r.Value) → source slug (via the srcUUIDToSlug index
//     built from manifest.Project.SourceID entries)
//  2. source slug → dest slug (via mapping.ResolveProjectSlug)
//  3. dest slug → dest project UUID (via s.Projects.GetProject)
//  4. CreateRestriction on the dest context with the dest UUID.
//
// If any step fails — including the destination project genuinely not existing
// (e.g. it was not migrated) — the restriction falls back to "manual" with a
// clear message explaining which step failed and what the operator should do.
//
// Dry-run (opts.Apply == false) reports "set"/"would add" without writing, the
// same as the other restriction types.
func (s *Syncer) ApplyDeferredProjectRestrictions(ctx context.Context, report *Report, opts Options) *Report {
	if report == nil {
		report = &Report{Applied: opts.Apply}
	}
	for _, d := range s.deferredProjectRestrictions {
		s.applyDeferredProjectRestriction(ctx, report, d, opts)
	}
	return report
}

func (s *Syncer) applyDeferredProjectRestriction(ctx context.Context, report *Report, d deferredProjectRestriction, opts Options) {
	srcUUID := d.restriction.Value
	label := restrictionLabel(d.restriction)
	target := d.target

	// Step 1: source UUID → source slug.
	srcSlug, ok := d.srcUUIDToSlug[srcUUID]
	if !ok {
		report.add("restriction", target, "manual",
			fmt.Sprintf("project restriction %q (source UUID %q) not found in manifest — "+
				"ensure the project was exported, then re-run", label, srcUUID))
		return
	}

	// Step 2: source slug → dest slug.
	mapping := d.mapping
	if mapping == nil {
		mapping = manifest.IdentityMapping(srcSlug)
	}
	destSlug, ok := mapping.ResolveProjectSlug(srcSlug)
	if !ok {
		report.add("restriction", target, "manual",
			fmt.Sprintf("project restriction %q: no destination mapping for source slug %q — "+
				"add a projects entry to your mapping file, then re-run", label, srcSlug))
		return
	}

	// Step 3: dest slug → dest project UUID. By the time this runs the projects
	// step has executed, so a missing project means the project was not migrated.
	destProj, err := s.Projects.GetProject(ctx, destSlug)
	if err != nil || destProj == nil || destProj.ID == "" {
		if err != nil {
			report.add("restriction", target, "manual",
				fmt.Sprintf("project restriction %q: destination project %q not found (%v) — "+
					"migrate that project, then re-run", label, destSlug, err))
		} else {
			report.add("restriction", target, "manual",
				fmt.Sprintf("project restriction %q: destination project %q returned no UUID — "+
					"migrate that project, then re-run", label, destSlug))
		}
		return
	}
	destUUID := destProj.ID

	// Idempotency: skip if the restriction is already present.
	if hasProjectRestriction(d.existing, destUUID) {
		report.add("restriction", target, "exists", fmt.Sprintf("project restriction %q already present", label))
		return
	}

	if !opts.Apply || d.ctxID == "" {
		report.add("restriction", target, "set", fmt.Sprintf("would add project restriction %q", label))
		return
	}

	if err := s.Contexts.CreateRestriction(ctx, d.ctxID, "project", destUUID); err != nil {
		report.add("restriction", target, "error", err.Error())
		return
	}
	report.add("restriction", target, "set", fmt.Sprintf("added project restriction %q", label))
}

func hasExpressionRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "expression" && e.Value == value {
			return true
		}
	}
	return false
}

func hasGroupRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "group" && e.Value == value {
			return true
		}
	}
	return false
}

func hasProjectRestriction(existing []cctx.Restriction, value string) bool {
	for _, e := range existing {
		if e.Type == "project" && e.Value == value {
			return true
		}
	}
	return false
}

func restrictionLabel(r manifest.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}
