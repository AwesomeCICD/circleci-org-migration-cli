package exporter

import (
	"context"
	"fmt"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// DeployAPI is the subset of the org client (Deploys v3 reads) the exporter needs. When Deploy
// is nil on the Exporter, deploy capture is silently skipped.
type DeployAPI interface {
	ListDeployEnvironments(ctx context.Context, orgID string) ([]org.DeployEnvironment, error)
	GetDeployEnvironment(ctx context.Context, id string) (*org.DeployEnvironment, error)
	ListDeployComponents(ctx context.Context, orgID string) ([]org.DeployComponent, error)
	GetDeploySettings(ctx context.Context, projectID string) (*org.DeploySettings, error)
}

// exportDeploys captures the org's Deploys configuration into m.Deploys.
// Every read is best-effort (the public deploy API is experimental): a failure
// becomes a manifest warning and never fails the export. Component → project
// links are resolved to the project's source slug via the already-exported
// m.Projects (matched on the project UUID in Project.SourceID).
func (e *Exporter) exportDeploys(ctx context.Context, m *manifest.Manifest, o *org.Organization) {
	if e.Deploy == nil || o == nil || o.ID == "" {
		return
	}

	d := &manifest.Deploys{}

	envs, err := e.Deploy.ListDeployEnvironments(ctx, o.ID)
	if err != nil {
		m.AddWarning("org", "deploy_environments_unreadable",
			fmt.Sprintf("could not list deploy environments: %v", err))
	}
	for _, env := range envs {
		me := manifest.DeployEnvironment{
			Name:        env.Name,
			Description: env.Description,
			Kind:        env.Kind,
			Labels:      mapDeployLabels(env.Labels),
		}
		// The list omits kind/description; fetch the single resource for them.
		if full, gerr := e.Deploy.GetDeployEnvironment(ctx, env.ID); gerr != nil {
			clog.Debugf("could not read deploy environment %q detail: %v", env.Name, gerr)
		} else if full != nil {
			me.Description = full.Description
			me.Kind = full.Kind
			if len(full.Labels) > 0 {
				me.Labels = mapDeployLabels(full.Labels)
			}
		}
		d.Environments = append(d.Environments, me)
	}

	byID := make(map[string]manifest.Project, len(m.Projects))
	for _, p := range m.Projects {
		if p.SourceID != "" {
			byID[p.SourceID] = p
		}
	}

	comps, err := e.Deploy.ListDeployComponents(ctx, o.ID)
	if err != nil {
		m.AddWarning("org", "deploy_components_unreadable",
			fmt.Sprintf("could not list deploy components: %v", err))
	}
	for _, c := range comps {
		mc := manifest.DeployComponent{
			Name:     c.Name,
			Archived: c.ArchivedAt != "",
			Labels:   mapDeployLabels(c.Labels),
		}
		if p, ok := byID[c.ProjectID]; ok {
			mc.ProjectSlug = p.Slug
			mc.ProjectName = p.Name
		} else if c.ProjectID != "" {
			m.AddWarning("org", "deploy_component_project_unresolved",
				fmt.Sprintf("deploy component %q references project %s which is not in this export; its project link is not captured", c.Name, c.ProjectID))
		}
		d.Components = append(d.Components, mc)
	}

	for _, p := range m.Projects {
		if p.SourceID == "" {
			continue
		}
		st, serr := e.Deploy.GetDeploySettings(ctx, p.SourceID)
		if serr != nil {
			clog.Debugf("could not read deploy settings for %s: %v", p.Slug, serr)
			continue
		}
		if st != nil && len(st.PipelineIDs) > 0 {
			d.ProjectSettings = append(d.ProjectSettings, manifest.DeployProjectSettings{
				ProjectSlug:   p.Slug,
				ProjectName:   p.Name,
				PipelineCount: len(st.PipelineIDs),
			})
		}
	}

	if len(d.Environments) == 0 && len(d.Components) == 0 && len(d.ProjectSettings) == 0 {
		return
	}
	m.Deploys = d
	e.logf("  → captured %d deploy environment(s), %d component(s), %d project deploy setting(s)",
		len(d.Environments), len(d.Components), len(d.ProjectSettings))
}

func mapDeployLabels(in []org.DeployLabel) []manifest.DeployLabel {
	if len(in) == 0 {
		return nil
	}
	out := make([]manifest.DeployLabel, 0, len(in))
	for _, l := range in {
		out = append(out, manifest.DeployLabel{Key: l.Key, Value: l.Value})
	}
	return out
}
