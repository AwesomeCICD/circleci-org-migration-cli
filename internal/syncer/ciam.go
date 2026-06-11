package syncer

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// CIAMWriter is the subset of CIAM write operations the syncer needs.
// When nil on the Syncer, CIAM sync is skipped.
type CIAMWriter interface {
	// ListOrgRoleGrants returns existing org-level role grants for the email→userId map.
	ListOrgRoleGrants(orgID string) ([]CIAMRoleGrant, error)
	// SetOrgUserRole assigns an org-level CIAM role to a user.
	SetOrgUserRole(orgID, userID, role string) error
	// ListGroups returns existing groups (name→ID map for idempotency).
	ListGroups(orgID string) ([]CIAMGroupInfo, error)
	// CreateGroup creates a new CIAM group.
	CreateGroup(orgID, name, description string) (string, error)
	// AddUsersToGroup adds users to a group by userID.
	AddUsersToGroup(orgID, groupID string, userIDs []string) error
	// SetProjectUserRole assigns a project-level CIAM role to a user.
	SetProjectUserRole(orgID, projectID, userID, role string) error
	// AddProjectGroupRole grants a role to groups on a project.
	AddProjectGroupRole(orgID, projectID string, groupIDs []string, role string) error
}

// CIAMRoleGrant carries the email → userID mapping returned by ListOrgRoleGrants.
type CIAMRoleGrant struct {
	UserID string
	Email  string
}

// CIAMGroupInfo carries group name → ID info.
type CIAMGroupInfo struct {
	ID   string
	Name string
}

// SyncCIAM applies CIAM role/group data from the manifest to the destination org.
//
// Gates:
//   - Source manifest must have CIAM data (only present for circleci-type source orgs).
//   - Destination org must be circleci-type (checked via GetOrganization).
//   - CIAMWriter must be non-nil.
//
// Behaviour:
//   - Creates groups by name (idempotent via ListGroups pre-check).
//   - Resolves destination user IDs by email via ListOrgRoleGrants.
//   - Applies org-level roles for matched users.
//   - Applies project role grants (user + group) for matched users/groups.
//   - Unmatched users (not yet in dest org) emit "manual" actions.
//   - Dry-run: no writes, plans are recorded in the report.
func (s *Syncer) SyncCIAM(m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
	if mapping == nil {
		mapping = manifest.IdentityMapping(m.Source.Org.Slug)
	}
	destSlug := mapping.Org.To
	if destSlug == "" {
		destSlug = m.Source.Org.Slug
	}
	report := &Report{DestOrgSlug: destSlug, Applied: opts.Apply}

	// Gate: no CIAM data in manifest.
	if m.CIAM == nil {
		s.logf("No CIAM data in manifest — skipping CIAM sync")
		return report, nil
	}

	// Gate: CIAM writer not wired.
	if s.CIAM == nil {
		s.logf("No CIAM writer configured — skipping CIAM sync")
		return report, nil
	}

	destOrgID, err := s.Org.ResolveOrgID(destSlug)
	if err != nil {
		return nil, fmt.Errorf("SyncCIAM: resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID

	// Gate: destination must be circleci-type.
	destOrg, err := s.Org.GetOrganization(destSlug)
	if err != nil {
		return nil, fmt.Errorf("SyncCIAM: get destination org %q: %w", destSlug, err)
	}
	if strings.ToLower(destOrg.VCSType) != "circleci" {
		s.logf("Destination org %q is not circleci-type (vcs_type=%q) — skipping CIAM sync", destSlug, destOrg.VCSType)
		report.add("ciam", "destination_not_standalone", "manual",
			fmt.Sprintf("destination org %q has vcs_type=%q; CIAM sync is only supported for circleci-type destination orgs", destSlug, destOrg.VCSType))
		return report, nil
	}

	s.logf("Syncing CIAM roles and groups to %s (id %s)%s", destSlug, destOrgID, dryRunSuffix(opts.Apply))

	ciam := m.CIAM

	// ── Step 1: Build email→userID map from destination org role grants ──────
	emailToUserID := s.buildEmailToUserIDMap(report, destOrgID)

	// ── Step 2: Create groups (idempotent by name) ───────────────────────────
	// groupNameToID maps group name → destination group ID (for project grants).
	groupNameToID := s.syncCIAMGroups(report, ciam, destOrgID, emailToUserID, opts)

	// ── Step 3: Apply org-level role grants ──────────────────────────────────
	s.syncCIAMOrgRoles(report, ciam, destOrgID, emailToUserID, opts)

	// ── Step 4: Apply project role grants ────────────────────────────────────
	s.syncCIAMProjectGrants(report, ciam, destOrgID, emailToUserID, groupNameToID, m, mapping, opts)

	return report, nil
}

// buildEmailToUserIDMap fetches the dest org's existing role grants and returns
// a map of email (lowercase) → userID. On error an "error" action is recorded
// and an empty map is returned (subsequent steps will emit "manual" for all users).
func (s *Syncer) buildEmailToUserIDMap(report *Report, destOrgID string) map[string]string {
	grants, err := s.CIAM.ListOrgRoleGrants(destOrgID)
	if err != nil {
		report.add("ciam", "email_resolve", "error",
			fmt.Sprintf("could not list destination org role grants to build email→userID map: %v", err))
		return map[string]string{}
	}
	m := make(map[string]string, len(grants))
	for _, g := range grants {
		if g.Email != "" {
			m[strings.ToLower(g.Email)] = g.UserID
		}
	}
	return m
}

// syncCIAMGroups creates groups that don't already exist in the dest org and
// returns a name→ID map for all groups (existing + newly created) so project
// grants can reference them.
func (s *Syncer) syncCIAMGroups(report *Report, ciam *manifest.CIAMData, destOrgID string, emailToUserID map[string]string, opts Options) map[string]string {
	if len(ciam.Groups) == 0 {
		return map[string]string{}
	}

	// Fetch existing groups once for idempotency.
	existingGroups, err := s.CIAM.ListGroups(destOrgID)
	if err != nil {
		report.add("ciam", "groups_list", "error",
			fmt.Sprintf("could not list destination groups: %v", err))
		return map[string]string{}
	}
	nameToID := make(map[string]string, len(existingGroups)+len(ciam.Groups))
	for _, g := range existingGroups {
		nameToID[g.Name] = g.ID
	}

	for _, g := range ciam.Groups {
		target := "ciam-group:" + g.Name

		if id, exists := nameToID[g.Name]; exists {
			report.add("ciam", target, "exists", fmt.Sprintf("group %q already exists in destination (id %s)", g.Name, id))
			// Group already exists — add members below if needed.
		} else {
			if !opts.Apply {
				report.add("ciam", target, "created", fmt.Sprintf("would create group %q", g.Name))
				// In dry-run we don't have a real ID, so skip member assignment.
				continue
			}
			newID, createErr := s.CIAM.CreateGroup(destOrgID, g.Name, g.Description)
			if createErr != nil {
				report.add("ciam", target, "error", fmt.Sprintf("create group %q: %v", g.Name, createErr))
				continue
			}
			nameToID[g.Name] = newID
			report.add("ciam", target, "created", fmt.Sprintf("created group %q (id %s)", g.Name, newID))
		}

		// Add matched members to the group.
		if len(g.MemberEmails) == 0 {
			continue
		}
		groupID := nameToID[g.Name]
		if groupID == "" {
			continue
		}
		var matchedUserIDs []string
		var unmatchedEmails []string
		for _, email := range g.MemberEmails {
			if uid, ok := emailToUserID[strings.ToLower(email)]; ok {
				matchedUserIDs = append(matchedUserIDs, uid)
			} else {
				unmatchedEmails = append(unmatchedEmails, email)
			}
		}
		if len(unmatchedEmails) > 0 {
			report.add("ciam", target+"/members", "manual",
				fmt.Sprintf("group %q: %d member(s) not found in destination by email (invite them first): %s",
					g.Name, len(unmatchedEmails), strings.Join(unmatchedEmails, ", ")))
		}
		if len(matchedUserIDs) > 0 {
			if !opts.Apply {
				report.add("ciam", target+"/members", "set",
					fmt.Sprintf("would add %d member(s) to group %q", len(matchedUserIDs), g.Name))
			} else {
				if addErr := s.CIAM.AddUsersToGroup(destOrgID, groupID, matchedUserIDs); addErr != nil {
					report.add("ciam", target+"/members", "error",
						fmt.Sprintf("add members to group %q: %v", g.Name, addErr))
				} else {
					report.add("ciam", target+"/members", "set",
						fmt.Sprintf("added %d member(s) to group %q", len(matchedUserIDs), g.Name))
				}
			}
		}
	}

	return nameToID
}

// syncCIAMOrgRoles applies org-level CIAM role grants. Users matched by email
// get their roles set; unmatched users emit "manual" actions.
func (s *Syncer) syncCIAMOrgRoles(report *Report, ciam *manifest.CIAMData, destOrgID string, emailToUserID map[string]string, opts Options) {
	for _, r := range ciam.OrgRoles {
		target := "ciam-org-role:" + r.Email
		uid, ok := emailToUserID[strings.ToLower(r.Email)]
		if !ok {
			report.add("ciam", target, "manual",
				fmt.Sprintf("user %q (role=%q) not found in destination org — invite the user first, then re-run sync", r.Email, r.Role))
			continue
		}
		if !opts.Apply {
			report.add("ciam", target, "set",
				fmt.Sprintf("would set org role %q for %q", r.Role, r.Email))
			continue
		}
		if err := s.CIAM.SetOrgUserRole(destOrgID, uid, r.Role); err != nil {
			report.add("ciam", target, "error",
				fmt.Sprintf("set org role %q for %q: %v", r.Role, r.Email, err))
			continue
		}
		report.add("ciam", target, "set",
			fmt.Sprintf("set org role %q for %q", r.Role, r.Email))
	}
}

// syncCIAMProjectGrants applies per-project CIAM role grants (user + group).
func (s *Syncer) syncCIAMProjectGrants(report *Report, ciam *manifest.CIAMData, destOrgID string, emailToUserID, groupNameToID map[string]string, m *manifest.Manifest, mapping *manifest.Mapping, opts Options) {
	// Build a source-slug → dest-project-ID map from the manifest projects.
	// We need the destination project UUID; the syncer's project mapper works
	// from slugs, so we look up by project name or slug match.
	srcSlugToDestID := buildSrcSlugToDestProjectID(m, mapping)

	// Per-project user grants.
	for _, g := range ciam.ProjectUserGrants {
		destProjID, ok := srcSlugToDestID[g.ProjectSlug]
		if !ok {
			target := "ciam-project-user:" + g.ProjectName + "/" + g.Email
			report.add("ciam", target, "manual",
				fmt.Sprintf("project %q not found in destination (slug %q) — create the project first, then re-run", g.ProjectName, g.ProjectSlug))
			continue
		}
		target := "ciam-project-user:" + g.ProjectName + "/" + g.Email
		uid, ok := emailToUserID[strings.ToLower(g.Email)]
		if !ok {
			report.add("ciam", target, "manual",
				fmt.Sprintf("user %q (project=%q, role=%q) not found in destination org — invite the user first, then re-run", g.Email, g.ProjectName, g.Role))
			continue
		}
		if !opts.Apply {
			report.add("ciam", target, "set",
				fmt.Sprintf("would set project role %q for %q on %q", g.Role, g.Email, g.ProjectName))
			continue
		}
		if err := s.CIAM.SetProjectUserRole(destOrgID, destProjID, uid, g.Role); err != nil {
			report.add("ciam", target, "error",
				fmt.Sprintf("set project role %q for %q on %q: %v", g.Role, g.Email, g.ProjectName, err))
			continue
		}
		report.add("ciam", target, "set",
			fmt.Sprintf("set project role %q for %q on %q", g.Role, g.Email, g.ProjectName))
	}

	// Per-project group grants.
	for _, g := range ciam.ProjectGroupGrants {
		destProjID, ok := srcSlugToDestID[g.ProjectSlug]
		if !ok {
			target := "ciam-project-group:" + g.ProjectName + "/" + g.GroupName
			report.add("ciam", target, "manual",
				fmt.Sprintf("project %q not found in destination (slug %q) — create the project first, then re-run", g.ProjectName, g.ProjectSlug))
			continue
		}
		target := "ciam-project-group:" + g.ProjectName + "/" + g.GroupName
		groupID, ok := groupNameToID[g.GroupName]
		if !ok {
			report.add("ciam", target, "manual",
				fmt.Sprintf("group %q (project=%q, role=%q) not found in destination — create the group first, then re-run", g.GroupName, g.ProjectName, g.Role))
			continue
		}
		if !opts.Apply {
			report.add("ciam", target, "set",
				fmt.Sprintf("would grant project role %q to group %q on %q", g.Role, g.GroupName, g.ProjectName))
			continue
		}
		if err := s.CIAM.AddProjectGroupRole(destOrgID, destProjID, []string{groupID}, g.Role); err != nil {
			report.add("ciam", target, "error",
				fmt.Sprintf("grant project role %q to group %q on %q: %v", g.Role, g.GroupName, g.ProjectName, err))
			continue
		}
		report.add("ciam", target, "set",
			fmt.Sprintf("granted project role %q to group %q on %q", g.Role, g.GroupName, g.ProjectName))
	}
}

// buildSrcSlugToDestProjectID builds a map of source project slug → destination
// project UUID from the manifest's project list plus any explicit project mapping.
// For circleci-type dest orgs the project's own SourceID is the UUID; for explicit
// mappings the mapped value is used.
func buildSrcSlugToDestProjectID(m *manifest.Manifest, mapping *manifest.Mapping) map[string]string {
	result := make(map[string]string, len(m.Projects))
	for _, p := range m.Projects {
		if p.SourceID == "" {
			continue
		}
		// For circleci-type same-org or same-type destination the source project UUID
		// is the destination project UUID (projects are shared by UUID, not recreated).
		// When an explicit slug→slug mapping exists and a dest project UUID is needed,
		// we use the source SourceID as best-effort.
		result[p.Slug] = p.SourceID
	}
	// Apply explicit slug→slug overrides from the mapping; use the mapped dest slug
	// as the "project ID" (best-effort for project grant lookups).
	if mapping != nil {
		for srcSlug, dstSlug := range mapping.Projects {
			if dstSlug != "" {
				result[srcSlug] = dstSlug
			}
		}
	}
	return result
}
