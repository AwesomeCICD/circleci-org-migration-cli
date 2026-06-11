package syncer

import (
	"context"
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// CIAMWriter is the subset of CIAM write operations the syncer needs.
// When nil on the Syncer, CIAM sync is skipped.
type CIAMWriter interface {
	// ListOrgRoleGrants returns existing org-level role grants for the email→userId map.
	ListOrgRoleGrants(ctx context.Context, orgID string) ([]CIAMRoleGrant, error)
	// SetOrgUserRole assigns an org-level CIAM role to a user.
	SetOrgUserRole(ctx context.Context, orgID, userID, role string) error
	// ListGroups returns existing groups (name→ID map for idempotency).
	ListGroups(ctx context.Context, orgID string) ([]CIAMGroupInfo, error)
	// CreateGroup creates a new CIAM group.
	CreateGroup(ctx context.Context, orgID, name, description string) (string, error)
	// AddUsersToGroup adds users to a group by userID.
	AddUsersToGroup(ctx context.Context, orgID, groupID string, userIDs []string) error
	// SetProjectUserRole assigns a project-level CIAM role to a user.
	SetProjectUserRole(ctx context.Context, orgID, projectID, userID, role string) error
	// AddProjectGroupRole grants a role to groups on a project.
	AddProjectGroupRole(ctx context.Context, orgID, projectID string, groupIDs []string, role string) error
}

// CIAMRoleGrant carries the identity of an existing destination role grant
// returned by ListOrgRoleGrants. Email is frequently EMPTY in the CIAM
// role-grants API response, so Username and UserID are also kept so source
// grants can be matched even when no email is available on either side.
type CIAMRoleGrant struct {
	UserID   string
	Email    string
	Username string
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
//   - Resolves destination user IDs from ListOrgRoleGrants by email, then by
//     username, then by userID (the CIAM API often returns an empty email).
//   - Applies org-level roles for matched users.
//   - Records per-project role grants (user + group) as "manual": the dest
//     project UUID is not reliably mappable from the source, so they cannot be
//     applied automatically (see #179).
//   - Unmatched users (not yet in dest org) emit "manual" actions.
//   - Dry-run: no writes, plans are recorded in the report.
func (s *Syncer) SyncCIAM(ctx context.Context, m *manifest.Manifest, mapping *manifest.Mapping, opts Options) (*Report, error) {
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

	destOrgID, err := s.Org.ResolveOrgID(ctx, destSlug)
	if err != nil {
		return nil, fmt.Errorf("SyncCIAM: resolving destination org %q: %w", destSlug, err)
	}
	report.DestOrgID = destOrgID

	// Gate: destination must be circleci-type.
	destOrg, err := s.Org.GetOrganization(ctx, destSlug)
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

	// ── Step 1: Build the destination user resolver from role grants ─────────
	// The CIAM role-grants API often returns an empty email, so we index dest
	// users by email, username, AND userID and match source grants by whichever
	// identity is available.
	resolver := s.buildUserResolver(ctx, report, destOrgID)

	// ── Step 2: Create groups (idempotent by name) ───────────────────────────
	s.syncCIAMGroups(ctx, report, ciam, destOrgID, resolver, opts)

	// ── Step 3: Apply org-level role grants ──────────────────────────────────
	s.syncCIAMOrgRoles(ctx, report, ciam, destOrgID, resolver, opts)

	// ── Step 4: Record project role grants as manual (see #179) ──────────────
	s.syncCIAMProjectGrants(ctx, report, ciam)

	return report, nil
}

// ciamUserResolver maps the various portable identities of destination org
// users to their userID. The CIAM role-grants API frequently returns an empty
// email, so a source grant is matched by email first, then by username, then by
// userID — whichever is present on the source grant and known on the dest side.
type ciamUserResolver struct {
	byEmail    map[string]string // lower(email) → userID
	byUsername map[string]string // lower(username) → userID
	byUserID   map[string]string // userID → userID (membership check)
}

// resolve returns the destination userID for a source grant identified by some
// combination of email, username, and userID. The second return value is a
// short label describing how the match was made ("email", "username", "user_id")
// for use in report messages; it is empty when no match was found.
func (r *ciamUserResolver) resolve(email, username, userID string) (string, string) {
	if r == nil {
		return "", ""
	}
	if email != "" {
		if uid, ok := r.byEmail[strings.ToLower(email)]; ok {
			return uid, "email"
		}
	}
	if username != "" {
		if uid, ok := r.byUsername[strings.ToLower(username)]; ok {
			return uid, "username"
		}
	}
	if userID != "" {
		if uid, ok := r.byUserID[userID]; ok {
			return uid, "user_id"
		}
	}
	return "", ""
}

// buildUserResolver fetches the dest org's existing role grants and indexes
// them by email, username, and userID. On error an "error" action is recorded
// and an empty resolver is returned (subsequent steps will emit "manual" for
// every source user).
func (s *Syncer) buildUserResolver(ctx context.Context, report *Report, destOrgID string) *ciamUserResolver {
	r := &ciamUserResolver{
		byEmail:    map[string]string{},
		byUsername: map[string]string{},
		byUserID:   map[string]string{},
	}
	grants, err := s.CIAM.ListOrgRoleGrants(ctx, destOrgID)
	if err != nil {
		report.add("ciam", "user_resolve", "error",
			fmt.Sprintf("could not list destination org role grants to resolve users: %v", err))
		return r
	}
	for _, g := range grants {
		if g.Email != "" {
			r.byEmail[strings.ToLower(g.Email)] = g.UserID
		}
		if g.Username != "" {
			r.byUsername[strings.ToLower(g.Username)] = g.UserID
		}
		if g.UserID != "" {
			r.byUserID[g.UserID] = g.UserID
		}
	}
	return r
}

// ciamUserLabel returns the best human-readable identifier for a source CIAM
// user grant: email when present, then username, then a placeholder. Used in
// report messages so empty-email grants are not rendered as an empty string.
func ciamUserLabel(email, username string) string {
	if email != "" {
		return email
	}
	if username != "" {
		return username
	}
	return "(unknown user)"
}

// syncCIAMGroups creates groups that don't already exist in the dest org and
// returns a name→ID map for all groups (existing + newly created) so project
// grants can reference them.
func (s *Syncer) syncCIAMGroups(ctx context.Context, report *Report, ciam *manifest.CIAMData, destOrgID string, resolver *ciamUserResolver, opts Options) map[string]string {
	if len(ciam.Groups) == 0 {
		return map[string]string{}
	}

	// Fetch existing groups once for idempotency.
	existingGroups, err := s.CIAM.ListGroups(ctx, destOrgID)
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
			newID, createErr := s.CIAM.CreateGroup(ctx, destOrgID, g.Name, g.Description)
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
			// Group membership is captured by email only; fall back to nothing
			// else here since usernames are not available on group members.
			if uid, _ := resolver.resolve(email, "", ""); uid != "" {
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
				if addErr := s.CIAM.AddUsersToGroup(ctx, destOrgID, groupID, matchedUserIDs); addErr != nil {
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
func (s *Syncer) syncCIAMOrgRoles(ctx context.Context, report *Report, ciam *manifest.CIAMData, destOrgID string, resolver *ciamUserResolver, opts Options) {
	for _, r := range ciam.OrgRoles {
		label := ciamUserLabel(r.Email, r.Username)
		target := "ciam-org-role:" + label
		uid, _ := resolver.resolve(r.Email, r.Username, "")
		if uid == "" {
			report.add("ciam", target, "manual",
				fmt.Sprintf("user %q (role=%q) not found in destination org by email or username — invite the user first, then re-run sync", label, r.Role))
			continue
		}
		if !opts.Apply {
			report.add("ciam", target, "set",
				fmt.Sprintf("would set org role %q for %q", r.Role, label))
			continue
		}
		if err := s.CIAM.SetOrgUserRole(ctx, destOrgID, uid, r.Role); err != nil {
			report.add("ciam", target, "error",
				fmt.Sprintf("set org role %q for %q: %v", r.Role, label, err))
			continue
		}
		report.add("ciam", target, "set",
			fmt.Sprintf("set org role %q for %q", r.Role, label))
	}
}

// syncCIAMProjectGrants records per-project CIAM role grants (user + group) as
// MANUAL follow-ups. Unlike org-level roles, project-level grants cannot be
// applied automatically: the destination project UUID is assigned when the
// project is created on the destination and is not reliably mappable from the
// source project UUID, so a blind write keyed on the source UUID would target
// the wrong project (and the API accepts it without persisting a usable grant).
// Reliable project-grant apply requires resolving the destination project UUID
// by name on the destination org — tracked in #179.
func (s *Syncer) syncCIAMProjectGrants(ctx context.Context, report *Report, ciam *manifest.CIAMData) {
	const note = " — project-level CIAM grants are not applied automatically because the destination project UUID is not reliably mappable from the source (see #179); recreate this grant manually on the destination project"

	for _, g := range ciam.ProjectUserGrants {
		label := ciamUserLabel(g.Email, g.Username)
		target := "ciam-project-user:" + g.ProjectName + "/" + label
		report.add("ciam", target, "manual",
			fmt.Sprintf("grant project role %q to user %q on project %q%s", g.Role, label, g.ProjectName, note))
	}

	for _, g := range ciam.ProjectGroupGrants {
		target := "ciam-project-group:" + g.ProjectName + "/" + g.GroupName
		report.add("ciam", target, "manual",
			fmt.Sprintf("grant project role %q to group %q on project %q%s", g.Role, g.GroupName, g.ProjectName, note))
	}
}
