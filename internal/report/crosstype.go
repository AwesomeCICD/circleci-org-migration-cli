package report

import (
	"fmt"
	"strings"

	"github.com/AwesomeCICD/circleci-org-migration-cli/internal/manifest"
)

// isCrossTypeToApp reports whether this is a GitHub OAuth (gh/) source going to
// a GitHub App / standalone (circleci/) destination — the only move that needs
// the checklist below.
func isCrossTypeToApp(m *manifest.Manifest, opts Options) bool {
	return isOAuthSource(m) && strings.HasPrefix(opts.DestOrgSlug, "circleci/")
}

// writeCrossTypeChecklist renders the ordered, copy-pasteable checklist for an
// OAuth → App migration. Several steps are support-run staff commands and the
// order matters: GitHub App changes delete data, and a namespace/class move
// must precede recreating runner classes.
func writeCrossTypeChecklist(b *strings.Builder, m *manifest.Manifest, opts Options) {
	src := orDash(m.Source.Org.Slug)
	dest := opts.DestOrgSlug
	destShort := strings.TrimPrefix(dest, "circleci/")
	if destShort == "" {
		destShort = "<dest-org-short-id>"
	}

	fmt.Fprintf(b, "\n### 1b. Cross-type checklist: GitHub OAuth (`%s`) → GitHub App (`%s`)\n\n", src, dest)
	fmt.Fprintf(b, "This move changes org type, so several steps sit outside `sync` and some are run by CircleCI Support. Do them in this order:\n\n")

	n := 1
	step := func(format string, args ...any) {
		fmt.Fprintf(b, "%d. %s\n", n, fmt.Sprintf(format, args...))
		n++
	}

	step("**Export and capture first — before touching the GitHub App.** Complete `export`, `secrets capture` and a `sync` dry run now. " +
		"Uninstalling the CircleCI GitHub App from a GitHub org deletes that CircleCI org's GitHub App pipelines and triggers, so anything not exported by then is gone.")
	step("**Move the GitHub App connection.** A GitHub org can be connected to only ONE CircleCI org through the CircleCI GitHub App: "+
		"uninstall the app from the GitHub org's current CircleCI org (the OAuth org), then install it from the new `%s` org and connect the repos.", dest)
	step("**Transfer the plan** (Support, staff admin command): `transfer-plan \"%s\" :github \"%s\" :circleci`. "+
		"Preconditions: the destination org must be on the **Free** plan and must not be a plan-share rider. Confirm the argument forms with Support.", src, destShort)

	nsList := crossTypeNamespaces(m)
	if len(nsList) == 0 {
		step("**Move any orb / runner namespace** (Support): a namespace belongs to exactly one org, so it is moved, not recreated: "+
			"`change-namespace-owner! \"<namespace>\" \"%s\"` (never `rename-namespace`, which does not free the old name). The orbs move with it and config references need no change. Skip if the org has no namespace.", dest)
	} else {
		var cmds []string
		for _, ns := range nsList {
			cmds = append(cmds, fmt.Sprintf("`change-namespace-owner! \"%s\" \"%s\"`", ns, dest))
		}
		step("**Move the namespace(s)** (Support): a namespace belongs to exactly one org, so it is moved, not recreated: %s "+
			"(never `rename-namespace`, which does not free the old name). The orbs move with it and config references need no change. `sync` detects a still-source-owned namespace and will not republish.", strings.Join(cmds, ", "))
	}

	if len(m.RunnerResourceClasses) > 0 {
		step("**Recreate runner resource classes and re-register agents.** Each class stores the org it was created under and does NOT follow a namespace move; " +
			"with an unchanged namespace name the destination cannot create a same-named class (HTTP 409) until the source copy is deleted. " +
			"Delete the source classes (or ask Support), re-run `sync`, then issue new tokens (`circleci runner token create <class> \"<nickname>\"`) and re-register every launch-agent. " +
			"If the namespace name changes, every `resource_class:` value in config must be rewritten as well.")
	} else {
		step("**Runner resource classes** (if the org uses self-hosted runners): class attribution does not follow a namespace move — recreate each class in the new org, " +
			"issue new tokens and re-register the agents.")
	}

	step("**Repoint cloud OIDC trust (AWS / GCP / Vault).** The OIDC issuer becomes `https://oidc.circleci.com/org/<new-org-id>` and the audience is the new org id. " +
		"The `sub` claim embeds project UUIDs (`org/<org-id>/project/<project-id>/user/<user-id>`), which are NEW on recreated projects — any trust condition on `sub` must be updated too.")
	step("**IP ranges entitlement.** If the source org uses IP ranges, that entitlement is per org — ask your CircleCI account team to enable it on the new org before cutover, or allow-listed egress will break.")
	step("**SSO members default to Viewer.** Members who join the new org through SSO land with the Viewer role and cannot use contexts. " +
		"Set them to **Contributor** (or place them in a group that is) before enabling builds; `sync` only applies org roles for users already present.")
	step("**Do not build in both orgs.** Once builds are enabled on `%s`, stop the source: unfollow/disconnect the repos in `%s` (or leave its projects paused). "+
		"Building the same commits in both orgs doubles credit spend and duplicates status checks.", dest, src)

	fmt.Fprintf(b, "\n_Support-run commands (`transfer-plan`, `change-namespace-owner!`) are staff-only; open a ticket with the exact lines above._\n")
}

// crossTypeNamespaces returns the distinct orb / runner namespaces recorded in
// the manifest, in a stable order.
func crossTypeNamespaces(m *manifest.Manifest) []string {
	var out []string
	seen := map[string]bool{}
	add := func(ns string) {
		if ns != "" && !seen[ns] {
			seen[ns] = true
			out = append(out, ns)
		}
	}
	add(m.OrbNamespace)
	add(m.RunnerNamespace)
	if s := m.Source.Org.Settings; s != nil {
		add(s.OrbNamespace)
	}
	return out
}
