package exporter

import (
	"context"
	"fmt"

	cctx "github.com/AwesomeCICD/circleci-org-migration-cli/api/context"
	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

func (e *Exporter) exportContexts(ctx context.Context, m *manifest.Manifest, o *org.Organization) error {
	e.logf("Listing contexts...")
	clog.Debugf("ListContexts org_id=%s slug=%s", o.ID, o.Slug)
	contexts, err := e.Contexts.ListContexts(ctx, o.ID, o.Slug)
	if err != nil {
		return err
	}
	e.logf("  → %d context(s)", len(contexts))

	for _, c := range contexts {
		mc := manifest.Context{Name: c.Name, SourceID: c.ID, CreatedAt: c.CreatedAt}

		if vars, verr := e.Contexts.ListEnvVars(ctx, c.ID); verr != nil {
			m.AddWarning("context:"+c.Name, "env_vars_unreadable", fmt.Sprintf("could not list env vars: %v", verr))
		} else {
			for _, v := range vars {
				mc.EnvVars = append(mc.EnvVars, manifest.ContextEnvVar{Name: v.Name, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt})
			}
			if len(vars) > 0 {
				m.AddWarning("context:"+c.Name, "context_values_excluded",
					fmt.Sprintf("%d context variable value(s) are not in the manifest; capture them with the in-pipeline secrets step", len(vars)))
			}
		}

		// Restrictions (v2) now return the group name directly, so security
		// groups are derived from the group-type restrictions — no GraphQL.
		if rs, rerr := e.Contexts.ListRestrictions(ctx, c.ID); rerr != nil {
			m.AddWarning("context:"+c.Name, "restrictions_unreadable", fmt.Sprintf("could not list restrictions: %v", rerr))
		} else {
			for _, r := range rs {
				mc.Restrictions = append(mc.Restrictions, manifest.Restriction{Type: r.Type, Value: r.Value, Name: r.Name})
				if r.Type == "group" {
					mc.SecurityGroups = append(mc.SecurityGroups, manifest.Group{ID: r.Value, Name: r.Name})
					// The default "All members" group restriction (value == org ID)
					// is auto-created on every context and is synced via CIAM — no
					// manual action is required.  Only emit the warning for real
					// (non-All-members) group restrictions.
					if r.Value != o.ID {
						m.AddWarning("context:"+c.Name, "group_restriction_manual",
							fmt.Sprintf("group restriction %q must be recreated manually (group-restriction writes are not yet GA)", restrictionName(r)))
					}
				}
			}
		}

		m.Contexts = append(m.Contexts, mc)
		e.logf("  • context %q: %d var(s), %d restriction(s), %d group(s)", mc.Name, len(mc.EnvVars), len(mc.Restrictions), len(mc.SecurityGroups))
	}
	return nil
}

func restrictionName(r cctx.Restriction) string {
	if r.Name != "" {
		return r.Name
	}
	return r.Value
}
