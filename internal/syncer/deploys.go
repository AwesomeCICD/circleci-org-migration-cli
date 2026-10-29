package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// deployReader reads the destination org's Deploys resources so the syncer can
// tell what already exists. The org client implements it; it is asserted
// optionally so a Syncer whose Org resolver lacks it simply cannot verify.
type deployReader interface {
	ListDeployEnvironments(ctx context.Context, orgUUID string) ([]org.DeployEnvironment, error)
	ListDeployComponents(ctx context.Context, orgUUID string) ([]org.DeployComponent, error)
}

// SyncDeploys reconciles the Deploys (release tracker) configuration captured
// in the manifest with the destination org.
//
// The public v3 Deploys API is EXPERIMENTAL and READ-ONLY — environments,
// components and deploy settings can be listed but there is no create endpoint
// (release-tracker docs/apispec/public/deploy-*.md). Environments and components
// are instead created implicitly: `circleci run release plan --environment-name
// ... --component-name ...` creates a missing environment/component on first
// use, and environments can also be created in the UI (Deploys → Environments).
// So this step never writes. It READS the destination so that:
//
//   - an environment/component that already exists is reported "exists";
//   - everything else is a "manual" action naming the item (and, for
//     components, the destination project it maps to), with how to create it;
//   - per-project deploy settings (deploy/rollback pipelines) are always manual:
//     pipeline UUIDs are source-specific.
//
// It is dry-run safe (reads only) and a no-op when the manifest has no Deploys.
func (s *Syncer) SyncDeploys(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	d := m.Deploys
	if d == nil || (len(d.Environments) == 0 && len(d.Components) == 0 && len(d.ProjectSettings) == 0) {
		clog.Debugf("manifest has no deploys; skipping deploy sync")
		return report, nil
	}

	s.logf("Reconciling Deploys (%d environment(s), %d component(s), %d project setting(s)) → %s [read-only: the Deploys API has no create endpoint]",
		len(d.Environments), len(d.Components), len(d.ProjectSettings), destSlug)

	destOrgID, err := s.Org.ResolveOrgID(ctx, destSlug)
	if err != nil {
		return nil, fmt.Errorf("SyncDeploys: resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID

	// Destination state (best-effort reads).
	destEnvs := map[string]bool{}
	destComps := map[string]string{} // component name + "\x00" + project id
	readable := false
	if rd, ok := s.Org.(deployReader); ok {
		envs, eerr := rd.ListDeployEnvironments(ctx, destOrgID)
		comps, cerr := rd.ListDeployComponents(ctx, destOrgID)
		if eerr != nil || cerr != nil {
			clog.Debugf("could not read destination deploys: envs=%v components=%v", eerr, cerr)
		} else {
			readable = true
			for _, e := range envs {
				destEnvs[e.Name] = true
			}
			for _, c := range comps {
				destComps[c.Name+"\x00"+c.ProjectID] = c.ID
			}
		}
	}

	// Destination projects by name (for the component → project mapping).
	destProjByName := map[string]string{}
	if s.Projects != nil {
		if projs, perr := s.Projects.ListOrgProjects(ctx, destOrgID); perr != nil {
			clog.Debugf("could not list destination projects for deploy mapping: %v", perr)
		} else {
			for _, p := range projs {
				destProjByName[p.Name] = p.ID
			}
		}
	}

	for _, env := range d.Environments {
		target := env.Name
		if destEnvs[env.Name] {
			report.add("deploy-environment", target, "exists",
				fmt.Sprintf("deploy environment %q already exists in the destination org", env.Name))
			continue
		}
		kind := env.Kind
		if kind == "" {
			kind = "custom"
		}
		report.add("deploy-environment", target, "manual", fmt.Sprintf(
			"create deploy environment %q (kind %q%s) in the destination org: Deploys → Environments → Create environment integration. "+
				"The public Deploys API has no create endpoint; an environment is also created automatically the first time a job runs "+
				"`circleci run release plan --environment-name=%q`.%s",
			env.Name, kind, describeSuffix(env.Description), env.Name, readableSuffix(readable)))
	}

	for _, c := range d.Components {
		target := c.Name
		if c.Archived {
			report.add("deploy-component", target, "skipped",
				fmt.Sprintf("deploy component %q is archived in the source org and is not migrated", c.Name))
			continue
		}
		projName := componentProjectName(c, mapping)
		destProjID := ""
		if projName != "" {
			destProjID = destProjByName[projName]
		}
		if _, ok := destComps[c.Name+"\x00"+destProjID]; ok && readable {
			report.add("deploy-component", target, "exists",
				fmt.Sprintf("deploy component %q already exists in the destination org", c.Name))
			continue
		}
		projNote := "it is not tied to a project"
		switch {
		case projName != "" && destProjID != "":
			projNote = fmt.Sprintf("its project %q exists in the destination", projName)
		case projName != "":
			projNote = fmt.Sprintf("its project %q does not exist in the destination yet — run the projects sync first", projName)
		}
		report.add("deploy-component", target, "manual", fmt.Sprintf(
			"deploy component %q must be recreated in the destination org (%s). The public Deploys API has no create endpoint; "+
				"a component is created automatically by the first `circleci run release plan --component-name=%q` run in the destination project "+
				"(or by the release agent). Deploy history and version records do not transfer.",
			c.Name, projNote, c.Name))
	}

	for _, ps := range d.ProjectSettings {
		name := ps.ProjectName
		if name == "" {
			name = ps.ProjectSlug
		}
		report.add("deploy-settings", name, "manual", fmt.Sprintf(
			"project %q has %d deploy/rollback pipeline(s) configured; re-run the Deploys setup (Deploys → Set up) on the destination project — "+
				"pipeline UUIDs are source-specific and cannot be replayed",
			name, ps.PipelineCount))
	}

	return report, nil
}

// componentProjectName returns the source project's repo/project name for a
// component, preferring the mapped destination slug's last segment when the
// mapping resolves it.
func componentProjectName(c manifest.DeployComponent, mapping *manifest.Mapping) string {
	if c.ProjectSlug != "" && mapping != nil {
		if dst, ok := mapping.ResolveProjectSlug(c.ProjectSlug); ok {
			if i := strings.LastIndex(dst, "/"); i >= 0 && i < len(dst)-1 {
				return dst[i+1:]
			}
		}
	}
	return c.ProjectName
}

func describeSuffix(desc string) string {
	if desc == "" {
		return ""
	}
	return fmt.Sprintf(", description %q", desc)
}

func readableSuffix(readable bool) string {
	if readable {
		return ""
	}
	return " (the destination's existing environments could not be read, so this may already exist)"
}
