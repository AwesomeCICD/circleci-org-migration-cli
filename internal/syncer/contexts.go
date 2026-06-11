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

	for _, c := range m.Contexts {
		ctxID, err := s.ensureContext(ctx, report, c.Name, destOrgID, byName, opts)
		if err != nil {
			report.add("context", c.Name, "error", err.Error())
			continue
		}
		s.syncContextVars(ctx, report, c, bundle, ctxID, opts)
		s.syncContextRestrictions(ctx, report, c, ctxID, destOrgID, &groupCache, &groupCacheLoaded, opts)
	}
	return report, nil
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

func (s *Syncer) syncContextRestrictions(ctx context.Context, report *Report, c manifest.Context, ctxID, destOrgID string, groupCache *map[string]string, groupCacheLoaded *bool, opts Options) {
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
		default:
			// project-type values are source-org UUIDs (need remap) and have no
			// name-based equivalent in the destination — manual handling.
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

func restrictionLabel(r manifest.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}
