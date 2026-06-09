// Package report renders a human-readable summary and a saved audit document
// from an export manifest. The summary is printed to the terminal; the audit
// is written to disk so a migration can be reviewed and verified after the fact.
package report

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/CircleCI-Public/circleci-org-migration-cli/internal/manifest"
)

// Summary returns a concise, human-readable overview of an export for printing
// to the terminal.
func Summary(m *manifest.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "CircleCI export summary\n")
	fmt.Fprintf(&b, "  Source org : %s  (%s)\n", orDash(m.Source.Org.Name), orDash(m.Source.Org.Slug))
	if m.Source.Org.ID != "" {
		fmt.Fprintf(&b, "  Org ID     : %s\n", m.Source.Org.ID)
	}
	fmt.Fprintf(&b, "  Host       : %s\n", orDash(m.Source.Host))
	if m.GeneratedAt != "" {
		fmt.Fprintf(&b, "  Generated  : %s\n", m.GeneratedAt)
	}

	cv := countContextVars(m)
	pv := countProjectVars(m)
	fmt.Fprintf(&b, "\n  Contexts   : %d  (%d variable name(s))\n", len(m.Contexts), cv)
	fmt.Fprintf(&b, "  Projects   : %d  (%d variable name(s))\n", len(m.Projects), pv)

	byCode := warningsByCode(m)
	fmt.Fprintf(&b, "  Warnings   : %d\n", len(m.Warnings))
	if len(byCode) > 0 {
		for _, c := range sortedKeys(byCode) {
			fmt.Fprintf(&b, "    - %s (%d)\n", c, byCode[c])
		}
	}

	fmt.Fprintf(&b, "\n  Note: secret VALUES are never exported via API. Use the in-pipeline\n")
	fmt.Fprintf(&b, "  secrets step to capture them. See the audit report for details.\n")
	return b.String()
}

// Markdown returns a full audit document describing everything captured and
// every warning, suitable for saving and reviewing.
func Markdown(m *manifest.Manifest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# CircleCI migration audit\n\n")
	if m.GeneratedAt != "" {
		fmt.Fprintf(&b, "- Generated: `%s`\n", m.GeneratedAt)
	}
	if m.ToolVersion != "" {
		fmt.Fprintf(&b, "- Tool: `%s`\n", m.ToolVersion)
	}
	fmt.Fprintf(&b, "- Source org: `%s` (`%s`)\n", orDash(m.Source.Org.Name), orDash(m.Source.Org.Slug))
	if m.Source.Org.ID != "" {
		fmt.Fprintf(&b, "- Org ID: `%s`\n", m.Source.Org.ID)
	}
	fmt.Fprintf(&b, "- Host: `%s`\n", orDash(m.Source.Host))
	if s := m.Source.Org.Settings; s != nil && s.RequireContextGroupRestriction != nil {
		fmt.Fprintf(&b, "- Require context group restriction: `%t`\n", *s.RequireContextGroupRestriction)
	}

	fmt.Fprintf(&b, "\n## Summary\n\n")
	fmt.Fprintf(&b, "| Resource | Count | Variable names |\n|---|---:|---:|\n")
	fmt.Fprintf(&b, "| Contexts | %d | %d |\n", len(m.Contexts), countContextVars(m))
	fmt.Fprintf(&b, "| Projects | %d | %d |\n", len(m.Projects), countProjectVars(m))
	fmt.Fprintf(&b, "| Warnings | %d | |\n", len(m.Warnings))

	// Contexts
	fmt.Fprintf(&b, "\n## Contexts\n\n")
	if len(m.Contexts) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	}
	for _, c := range m.Contexts {
		fmt.Fprintf(&b, "### `%s`\n\n", c.Name)
		fmt.Fprintf(&b, "- Environment variables (%d): %s\n", len(c.EnvVars), joinNames(contextVarNames(c)))
		if len(c.Restrictions) > 0 {
			fmt.Fprintf(&b, "- Restrictions:\n")
			for _, r := range c.Restrictions {
				label := r.Name
				if label == "" {
					label = r.Value
				}
				fmt.Fprintf(&b, "  - `%s`: %s\n", r.Type, label)
			}
		}
		if len(c.SecurityGroups) > 0 {
			names := make([]string, 0, len(c.SecurityGroups))
			for _, g := range c.SecurityGroups {
				names = append(names, g.Name)
			}
			fmt.Fprintf(&b, "- Security groups: %s\n", joinNames(names))
		}
		fmt.Fprintf(&b, "\n")
	}

	// Projects
	fmt.Fprintf(&b, "## Projects\n\n")
	if len(m.Projects) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	}
	for _, p := range m.Projects {
		fmt.Fprintf(&b, "### `%s`\n\n", p.Slug)
		if p.VCS.DefaultBranch != "" {
			fmt.Fprintf(&b, "- Default branch: `%s`\n", p.VCS.DefaultBranch)
		}
		fmt.Fprintf(&b, "- Environment variables (%d): %s\n", len(p.EnvVars), joinNames(projectVarNames(p)))
		if p.Settings != nil {
			fmt.Fprintf(&b, "- Advanced settings: %s\n", joinNames(setSettings(p.Settings)))
		}
		if len(p.CheckoutKeys) > 0 {
			fmt.Fprintf(&b, "- Checkout keys: %d\n", len(p.CheckoutKeys))
		}
		if len(p.Webhooks) > 0 {
			fmt.Fprintf(&b, "- Webhooks: %d\n", len(p.Webhooks))
		}
		if len(p.Schedules) > 0 {
			fmt.Fprintf(&b, "- Schedules: %d\n", len(p.Schedules))
		}
		fmt.Fprintf(&b, "\n")
	}

	// Warnings
	fmt.Fprintf(&b, "## Warnings & manual follow-ups\n\n")
	if len(m.Warnings) == 0 {
		fmt.Fprintf(&b, "_None._\n")
	} else {
		fmt.Fprintf(&b, "| Scope | Code | Detail |\n|---|---|---|\n")
		for _, w := range m.Warnings {
			fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", w.Scope, w.Code, escapePipes(w.Message))
		}
	}
	return b.String()
}

// SaveMarkdown writes the audit document to path.
func SaveMarkdown(m *manifest.Manifest, path string) error {
	if err := os.WriteFile(path, []byte(Markdown(m)), 0o644); err != nil {
		return fmt.Errorf("writing audit report %s: %w", path, err)
	}
	return nil
}

// --- helpers ---------------------------------------------------------------

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func countContextVars(m *manifest.Manifest) int {
	n := 0
	for _, c := range m.Contexts {
		n += len(c.EnvVars)
	}
	return n
}

func countProjectVars(m *manifest.Manifest) int {
	n := 0
	for _, p := range m.Projects {
		n += len(p.EnvVars)
	}
	return n
}

func contextVarNames(c manifest.Context) []string {
	names := make([]string, 0, len(c.EnvVars))
	for _, v := range c.EnvVars {
		names = append(names, v.Name)
	}
	return names
}

func projectVarNames(p manifest.Project) []string {
	names := make([]string, 0, len(p.EnvVars))
	for _, v := range p.EnvVars {
		names = append(names, v.Name)
	}
	return names
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return "_none_"
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = "`" + n + "`"
	}
	return strings.Join(quoted, ", ")
}

func setSettings(s *manifest.AdvancedSettings) []string {
	var out []string
	add := func(name string, v *bool) {
		if v != nil {
			out = append(out, fmt.Sprintf("%s=%t", name, *v))
		}
	}
	add("autocancel_builds", s.AutocancelBuilds)
	add("build_fork_prs", s.BuildForkPRs)
	add("build_prs_only", s.BuildPRsOnly)
	add("disable_ssh", s.DisableSSH)
	add("forks_receive_secret_env_vars", s.ForksReceiveSecretEnvVars)
	add("oss", s.OSS)
	add("set_github_status", s.SetGitHubStatus)
	add("setup_workflows", s.SetupWorkflows)
	add("write_settings_requires_admin", s.WriteSettingsRequiresAdmin)
	if len(s.PROnlyBranchOverrides) > 0 {
		out = append(out, "pr_only_branch_overrides=["+strings.Join(s.PROnlyBranchOverrides, ",")+"]")
	}
	return out
}

func warningsByCode(m *manifest.Manifest) map[string]int {
	byCode := map[string]int{}
	for _, w := range m.Warnings {
		byCode[w.Code]++
	}
	return byCode
}

func sortedKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func escapePipes(s string) string {
	return strings.ReplaceAll(s, "|", "\\|")
}
