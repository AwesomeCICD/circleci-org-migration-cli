package exporter

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/api/org"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/clog"
	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// exportCIAM captures CIAM role/group data for standalone (circleci-type) orgs.
// For VCS-type orgs (GitHub OAuth, Bitbucket) CIAM roles are managed by the VCS
// provider and are not migratable via this tool; we emit a single info note.
// All reads are best-effort: failures add warnings and never abort the export.
func (e *Exporter) exportCIAM(ctx context.Context, m *manifest.Manifest, o *org.Organization) {
	if strings.ToLower(o.VCSType) != "circleci" {
		// VCS-type orgs: roles come from the VCS and are not managed here.
		clog.Debugf("exportCIAM: org %s has vcs_type=%q; CIAM roles come from VCS — not captured", o.ID, o.VCSType)
		return
	}
	if o.ID == "" {
		clog.Debugf("exportCIAM: org ID empty; skipping CIAM export")
		return
	}

	e.logf("Exporting CIAM roles and groups (circleci-type org)...")
	ciam := &manifest.CIAMData{}
	hasAny := false

	// ── Org-level role grants ────────────────────────────────────────────────
	orgGrants, err := e.Org.ListOrgRoleGrants(ctx, o.ID)
	if err != nil {
		m.AddWarning("ciam", "org_role_grants_unreadable",
			fmt.Sprintf("could not read org CIAM role grants: %v", err))
	} else {
		// Build an email → userID map for group-membership resolution.
		emailToUserID := make(map[string]string, len(orgGrants))
		for _, g := range orgGrants {
			if g.Email != "" {
				emailToUserID[g.Email] = g.UserID
			}
			ciam.OrgRoles = append(ciam.OrgRoles, manifest.CIAMOrgRole{
				Email:    g.Email,
				Username: g.Username,
				Role:     g.Role,
			})
		}
		if len(orgGrants) > 0 {
			hasAny = true
		}
		e.logf("  → %d org role grant(s)", len(orgGrants))

		// ── Groups ────────────────────────────────────────────────────────────
		groups, gerr := e.Org.ListGroups(ctx, o.ID)
		if gerr != nil {
			m.AddWarning("ciam", "groups_unreadable",
				fmt.Sprintf("could not read org CIAM groups: %v", gerr))
		} else {
			for _, g := range groups {
				if g.ID == o.ID {
					// Skip the default "All members" group (auto-created on every org).
					continue
				}
				cg := manifest.CIAMGroup{Name: g.Name}
				// MemberEmails: not directly available from the group list; the API
				// does not return members in the groups response. Group membership is
				// best-effort — we note this limitation and suggest manual verification.
				ciam.Groups = append(ciam.Groups, cg)
			}
			if len(ciam.Groups) > 0 {
				hasAny = true
				m.AddWarning("ciam", "group_membership_not_captured",
					"CIAM group member emails are not available from the groups list API; "+
						"group membership must be verified and recreated manually on the destination")
			}
			e.logf("  → %d CIAM group(s)", len(ciam.Groups))
		}
	}

	// ── Per-project role grants ──────────────────────────────────────────────
	for i := range m.Projects {
		p := &m.Projects[i]
		if p.SourceID == "" {
			continue
		}
		projName := p.Name
		if projName == "" {
			projName = p.Slug
		}

		// Per-project user role grants.
		userGrants, uerr := e.Org.ListProjectUserRoleGrants(ctx, o.ID, p.SourceID)
		if uerr != nil {
			m.AddWarning("ciam", "project_user_role_grants_unreadable",
				fmt.Sprintf("project %q: could not read project user CIAM role grants: %v", projName, uerr))
		} else {
			for _, g := range userGrants {
				ciam.ProjectUserGrants = append(ciam.ProjectUserGrants, manifest.CIAMProjectUserGrant{
					ProjectName: projName,
					ProjectSlug: p.Slug,
					Email:       g.Email,
					Username:    g.Username,
					Role:        g.Role,
				})
			}
			if len(userGrants) > 0 {
				hasAny = true
			}
		}

		// Per-project group role grants.
		groupGrants, egerr := e.Org.ListProjectGroupRoleGrants(ctx, o.ID, p.SourceID)
		if egerr != nil {
			m.AddWarning("ciam", "project_group_role_grants_unreadable",
				fmt.Sprintf("project %q: could not read project group CIAM role grants: %v", projName, egerr))
		} else {
			// Resolve group IDs to group names using the captured ciam.Groups list.
			groupIDToName := make(map[string]string, len(ciam.Groups))
			// Also include the "All members" group mapping.
			groupIDToName[o.ID] = "All members"
			// We need to re-fetch the raw group list to get IDs (ciam.Groups has names only).
			rawGroups, rgerr := e.Org.ListGroups(ctx, o.ID)
			if rgerr == nil {
				for _, rg := range rawGroups {
					groupIDToName[rg.ID] = rg.Name
				}
			}
			for _, g := range groupGrants {
				groupName := groupIDToName[g.GroupID]
				if groupName == "" {
					groupName = g.GroupID // fallback to ID if name not found
				}
				ciam.ProjectGroupGrants = append(ciam.ProjectGroupGrants, manifest.CIAMProjectGroupGrant{
					ProjectName: projName,
					ProjectSlug: p.Slug,
					GroupName:   groupName,
					Role:        g.Role,
				})
			}
			if len(groupGrants) > 0 {
				hasAny = true
			}
		}
	}

	if hasAny {
		m.CIAM = ciam
		e.logf("  → CIAM data captured (%d org roles, %d groups, %d project user grants, %d project group grants)",
			len(ciam.OrgRoles), len(ciam.Groups), len(ciam.ProjectUserGrants), len(ciam.ProjectGroupGrants))
	} else {
		e.logf("  → no CIAM data found (no role grants or groups)")
	}
}
