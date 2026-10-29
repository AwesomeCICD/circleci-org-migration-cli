package exporter

import (
	"context"
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// envHierarchyReader is the private release-tracker read surface needed to
// capture environment hierarchies. The org client implements it; it is asserted
// optionally so an Exporter whose Org lacks it simply skips the capture.
type envHierarchyReader interface {
	ListEnvironmentHierarchies(ctx context.Context, orgUUID string) ([]org.EnvHierarchySummary, error)
	GetEnvironmentHierarchyDetail(ctx context.Context, id string) (*org.EnvHierarchyDetail, error)
	ListEnvironmentHierarchyLevels(ctx context.Context, id string) ([]org.EnvHierarchyLevelRef, error)
	ResolveOrgEnvironmentHierarchy(ctx context.Context, orgUUID string) (scope, id, name string, err error)
}

// exportEnvironmentHierarchies captures every environment hierarchy (levels by
// environment name, and where each is assigned) into m.EnvironmentHierarchies.
// Best-effort: a read failure becomes a manifest warning and never fails the
// export. Project/component ids are resolved to names through the already
// exported m.Projects (Project.SourceID) and the Deploys components.
func (e *Exporter) exportEnvironmentHierarchies(ctx context.Context, m *manifest.Manifest, o *org.Organization) {
	r, ok := e.Org.(envHierarchyReader)
	if !ok || o == nil || o.ID == "" {
		return
	}
	hs, err := r.ListEnvironmentHierarchies(ctx, o.ID)
	if err != nil {
		m.AddWarning("org", "environment_hierarchies_unreadable",
			fmt.Sprintf("could not list environment hierarchies: %v", err))
		return
	}
	if len(hs) == 0 {
		return
	}

	// The org default is whatever resolves with no project/component.
	defaultID := ""
	if scope, id, _, rerr := r.ResolveOrgEnvironmentHierarchy(ctx, o.ID); rerr != nil {
		clog.Debugf("could not resolve org default environment hierarchy: %v", rerr)
	} else if scope == "ORGANIZATION" {
		defaultID = id
	}

	projByID := make(map[string]manifest.Project, len(m.Projects))
	for _, p := range m.Projects {
		if p.SourceID != "" {
			projByID[p.SourceID] = p
		}
	}
	type compInfo struct{ name, projectID string }
	comps := map[string]compInfo{}
	if e.Deploy != nil {
		if cs, cerr := e.Deploy.ListDeployComponents(ctx, o.ID); cerr != nil {
			clog.Debugf("could not list components for hierarchy assignments: %v", cerr)
		} else {
			for _, c := range cs {
				comps[c.ID] = compInfo{name: c.Name, projectID: c.ProjectID}
			}
		}
	}

	for _, h := range hs {
		def := manifest.EnvironmentHierarchyDef{
			Name:        h.Name,
			Description: h.Description,
			IsDefault:   defaultID != "" && h.ID == defaultID,
		}
		levels, lerr := r.ListEnvironmentHierarchyLevels(ctx, h.ID)
		if lerr != nil {
			m.AddWarning("org", "environment_hierarchy_levels_unreadable",
				fmt.Sprintf("could not read levels of environment hierarchy %q: %v", h.Name, lerr))
			continue
		}
		for _, l := range levels {
			def.Levels = append(def.Levels, manifest.EnvHierarchyLevel{Position: l.Position, IntegrationName: l.IntegrationName})
		}

		detail, derr := r.GetEnvironmentHierarchyDetail(ctx, h.ID)
		if derr != nil {
			m.AddWarning("org", "environment_hierarchy_assignments_unreadable",
				fmt.Sprintf("could not read where environment hierarchy %q is assigned: %v", h.Name, derr))
		} else {
			for _, pid := range detail.AssignedProjectIDs {
				p, found := projByID[pid]
				if !found {
					m.AddWarning("org", "environment_hierarchy_project_unresolved",
						fmt.Sprintf("environment hierarchy %q is assigned to project %s, which is not in this export — assign it by hand", h.Name, pid))
					continue
				}
				def.Projects = append(def.Projects, manifest.EnvHierarchyProjectRef{Slug: p.Slug, Name: p.Name})
			}
			for _, cid := range detail.AssignedComponentIDs {
				c, found := comps[cid]
				if !found {
					m.AddWarning("org", "environment_hierarchy_component_unresolved",
						fmt.Sprintf("environment hierarchy %q is assigned to component %s, which could not be named — assign it by hand", h.Name, cid))
					continue
				}
				ref := manifest.EnvHierarchyComponentRef{Name: c.name}
				if p, pf := projByID[c.projectID]; pf {
					ref.ProjectSlug, ref.ProjectName = p.Slug, p.Name
				}
				def.Components = append(def.Components, ref)
			}
		}
		m.EnvironmentHierarchies = append(m.EnvironmentHierarchies, def)
	}
	clog.Debugf("environment hierarchies captured: %d", len(m.EnvironmentHierarchies))
}
