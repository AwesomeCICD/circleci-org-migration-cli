package report

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// writeRunbook appends the customer-facing cutover runbook to b: the recommended
// order, what sync automates, the manual steps (data-driven from the manifest),
// what does not transfer, and the external-pin reminder.
func writeRunbook(b *strings.Builder, m *manifest.Manifest) {
	fmt.Fprintf(b, "\n## Cutover runbook\n\n")
	fmt.Fprintf(b, "This report is your migration plan. Work through the steps below in order;\n")
	fmt.Fprintf(b, "the manual steps and data-loss notes are tailored to what this export contains.\n")

	writeCutoverOrder(b)
	writeDetailedCutoverCommands(b, m)
	writeAutomatedBySync(b)
	writeManualSteps(b, m)
	writeDataLoss(b, m)
	writeExternalPins(b)
}

// writeCutoverOrder renders the recommended, numbered cutover sequence.
func writeCutoverOrder(b *strings.Builder) {
	fmt.Fprintf(b, "\n### 1. Recommended cutover order\n\n")
	fmt.Fprintf(b, "1. **Export the source org** — done; this report is the result. Review it before continuing.\n")
	fmt.Fprintf(b, "2. **Capture secret values** — run the in-pipeline `secrets` orb/step (or `secrets capture`) in the source org to collect context and project env-var values. They are never exported via the API.\n")
	fmt.Fprintf(b, "3. **`sync --apply`** — creates the destination resources. New projects are created **paused**: App triggers are disabled and OAuth onboarding is not followed, so no builds run yet.\n")
	fmt.Fprintf(b, "4. **Validate the destination** — confirm contexts, env-var names, project settings, webhooks, schedules, and group restrictions look correct against this report.\n")
	fmt.Fprintf(b, "5. **Enable builds** — turn the destination live (`sync --yes`, the interactive prompt, or re-enable triggers / follow projects).\n")
	fmt.Fprintf(b, "6. **Rotate the captured secrets** — once builds are healthy, rotate every value you captured in step 2 and delete the extraction artifacts (`secrets.json` and any logs).\n")
	fmt.Fprintf(b, "7. **Update external pins** — repoint anything that references the old org (see the last section).\n")
}

// writeDetailedCutoverCommands renders the copy-pasteable command sequence for
// the migration (#C). Uses the manifest's source org slug where available so
// the commands are immediately actionable for the operator.
func writeDetailedCutoverCommands(b *strings.Builder, m *manifest.Manifest) {
	orgSlug := m.Source.Org.Slug
	usedPlaceholder := false
	if orgSlug == "" {
		orgSlug = "<source-org-slug>"
		usedPlaceholder = true
	}

	sshFlag := ""
	sshNote := ""
	if hasAdditionalSSHKeys(m) {
		sshFlag = " --ssh-keys"
		sshNote = " + additional SSH private keys"
	}

	fmt.Fprintf(b, "\n### 1a. Copy-pasteable command sequence\n\n")
	fmt.Fprintf(b, "Run these in order. `sync` reads the destination org from the manifest — pass\n")
	fmt.Fprintf(b, "`--mapping mapping.json` to target a different org and/or rename projects.\n\n")
	fmt.Fprintf(b, "```sh\n")
	fmt.Fprintf(b, "# Step 1 — export the source org to a manifest (already done; this report is the result)\n")
	fmt.Fprintf(b, "circleci-migrate export --source-org %s -o manifest.json\n\n", orgSlug)

	fmt.Fprintf(b, "# Step 2 — capture secret VALUES (context + project env-var values%s).\n", sshNote)
	fmt.Fprintf(b, "#   Triggers an in-pipeline job in the source org; the artifact is age-encrypted.\n")
	fmt.Fprintf(b, "circleci-migrate secrets capture --manifest manifest.json%s -o secrets.json\n\n", sshFlag)

	fmt.Fprintf(b, "# Step 3 — dry-run sync (preview the plan; writes nothing)\n")
	fmt.Fprintf(b, "circleci-migrate sync --manifest manifest.json --secrets secrets.json\n\n")

	fmt.Fprintf(b, "# Step 4 — apply (create destination resources + inject captured values)\n")
	fmt.Fprintf(b, "circleci-migrate sync --manifest manifest.json --secrets secrets.json --apply\n\n")

	fmt.Fprintf(b, "# Step 5 — validate the destination, then rotate every captured secret value.\n")
	fmt.Fprintf(b, "# Delete secrets.json and any pipeline artifacts that contain secret material.\n")
	fmt.Fprintf(b, "```\n\n")
	footnote := "_The destination org comes from the manifest; use `--mapping mapping.json` to target a different org (e.g. `gh/acme-new` or `circleci/<new-org-uuid>`) or rename projects."
	if usedPlaceholder {
		footnote += " Replace `<source-org-slug>` above with the real source org slug."
	}
	footnote += "_\n"
	fmt.Fprintf(b, "%s", footnote)
}

// writeAutomatedBySync lists what `sync --apply` handles end-to-end.
func writeAutomatedBySync(b *strings.Builder) {
	fmt.Fprintf(b, "\n### 2. Automated by `sync --apply`\n\n")
	fmt.Fprintf(b, "- Contexts and their environment variables (names; values from the capture step).\n")
	fmt.Fprintf(b, "- Project settings, environment variables, webhooks, and scheduled pipelines.\n")
	fmt.Fprintf(b, "- Project- and org-level OIDC custom claims (audience / TTL).\n")
	fmt.Fprintf(b, "- Org settings: feature flags, OIDC, URL-orb allow list, config policies, technical/security contacts.\n")
	fmt.Fprintf(b, "- Project creation: OAuth orgs are onboarded by following the project; App orgs get their pipeline definitions and triggers recreated.\n")
	fmt.Fprintf(b, "- Context group restrictions, mapped onto destination CIAM groups.\n")
	fmt.Fprintf(b, "- CIAM **org-level** role grants and groups (standalone circleci-type orgs only; users matched by email, falling back to username — invite users to the destination first). Per-project CIAM grants are a manual step (see below).\n")
}

// hasNonDefaultGroupRestrictions reports whether any context in the manifest
// has at least one group restriction that is NOT the default "All members"
// group (type=="group", value!=orgID).  These are real access restrictions
// that must be re-applied manually because:
//   - Group restrictions are only supported on GitHub OAuth ("gh/…") orgs.
//   - They cannot be created via the API on standalone ("circleci/…") or
//     Bitbucket orgs (fails with "This is only supported for OAuth orgs.").
//   - VCS team IDs are org-specific and do not map across orgs.
func hasNonDefaultGroupRestrictions(m *manifest.Manifest) bool {
	orgID := m.Source.Org.ID
	for _, c := range m.Contexts {
		for _, r := range c.Restrictions {
			if r.Type == "group" && r.Value != orgID {
				return true
			}
		}
	}
	return false
}
