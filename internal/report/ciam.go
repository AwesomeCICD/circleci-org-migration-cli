package report

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// writeCIAMSection renders the CIAM roles and groups section of the report.
// Only shown for circleci-type source orgs where CIAM data was captured.
func writeCIAMSection(b *strings.Builder, m *manifest.Manifest) {
	if m.CIAM == nil {
		return
	}
	fmt.Fprintf(b, "\n## CIAM roles and groups\n\n")
	fmt.Fprintf(b, "_Only present for standalone (`circleci`-type) orgs. Users are identified by email when available, otherwise by username; groups by name._\n\n")
	fmt.Fprintf(b, "Reference: [Manage roles and permissions](https://circleci.com/docs/guides/permissions-authentication/manage-roles-and-permissions/) | [Manage groups](https://circleci.com/docs/guides/permissions-authentication/manage-groups/)\n\n")

	ciam := m.CIAM

	if len(ciam.OrgRoles) > 0 {
		fmt.Fprintf(b, "### Org-level roles (%d)\n\n", len(ciam.OrgRoles))
		fmt.Fprintf(b, "| User (email or username) | Username | Role |\n|---|---|---|\n")
		for _, r := range ciam.OrgRoles {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", ciamUserCell(r.Email, r.Username), orDash(r.Username), r.Role)
		}
		fmt.Fprintf(b, "\n")
	}

	if len(ciam.Groups) > 0 {
		fmt.Fprintf(b, "### CIAM groups (%d)\n\n", len(ciam.Groups))
		fmt.Fprintf(b, "| Name | Description | Member count |\n|---|---|---:|\n")
		for _, g := range ciam.Groups {
			fmt.Fprintf(b, "| `%s` | %s | %d |\n", g.Name, orDash(g.Description), len(g.MemberEmails))
		}
		fmt.Fprintf(b, "\n_Note: group membership is not available from the groups list API. Membership must be verified and recreated manually on the destination._\n\n")
	}

	if len(ciam.ProjectUserGrants) > 0 {
		fmt.Fprintf(b, "### Per-project user role grants (%d)\n\n", len(ciam.ProjectUserGrants))
		fmt.Fprintf(b, "| Project | User (email or username) | Role |\n|---|---|---|\n")
		for _, g := range ciam.ProjectUserGrants {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", g.ProjectName, ciamUserCell(g.Email, g.Username), g.Role)
		}
		fmt.Fprintf(b, "\n")
	}

	if len(ciam.ProjectGroupGrants) > 0 {
		fmt.Fprintf(b, "### Per-project group role grants (%d)\n\n", len(ciam.ProjectGroupGrants))
		fmt.Fprintf(b, "| Project | Group | Role |\n|---|---|---|\n")
		for _, g := range ciam.ProjectGroupGrants {
			fmt.Fprintf(b, "| `%s` | `%s` | `%s` |\n", g.ProjectName, g.GroupName, g.Role)
		}
		fmt.Fprintf(b, "\n")
	}
}
