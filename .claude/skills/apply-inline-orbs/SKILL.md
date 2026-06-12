---
name: apply-inline-orbs
description: >
  Inline private CircleCI orb references into pipeline config files. Trigger
  phrases: "inline circleci orb", "orb can't be resolved in destination org",
  "apply-inline-orbs", "circleci orb namespace overlap", "awesomecicd orb not
  found", "private orb not resolving", "inline orb source into config",
  "post-cutover revert orb inline". Wraps `circleci-migrate orb inline` for
  the per-repo PR loop and the post-cutover revert step.
---

# Apply Inline Orbs

During the migration **namespace overlap window**, the source org's private orb
(`awesomecicd/circleci-org-migration`) may not be resolvable in the destination
org. `circleci-migrate orb inline` replaces the external orb reference with the
orb's YAML source inline, removing the namespace dependency.

**This is a temporary workaround.** Revert the inlining once the namespace has
been transferred or the orb has been published in the destination namespace.

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing:**

- [ ] The `.circleci/config.yml` file path (or repository) to rewrite
- [ ] `CIRCLECI_SOURCE_TOKEN` or `--token` — used to fetch the orb YAML source
- [ ] Confirmation of which orb namespace(s) to inline (or "inline all")
- [ ] A branch and PR workflow for each affected repository
- [ ] Confirmation that this is a **temporary** change and will be reverted post-cutover

---

## Task-progress checklist

- [ ] Identified all repositories with the orb reference to inline
- [ ] For each repo: `orb inline` applied and output reviewed
- [ ] For each repo: PR opened with the inlined config
- [ ] PRs merged; inlined configs confirmed working
- [ ] Post-cutover: orb namespace resolved in destination → PRs opened to revert inlining
- [ ] Revert PRs merged; orb references restored to external format

---

## Guardrails

- **Never commit a permanently inlined config without a revert plan.** Inlining is a temporary workaround; the config grows significantly and drifts as the orb updates.
- **YAML comments and anchors are NOT preserved** by the inline round-trip serialization. Warn the user before running inline on a config with significant comments or YAML anchors.
- **Never run on production branches directly.** Always create a PR.
- **After the namespace overlap window closes, revert.** Keeping an inlined config long-term means you won't receive orb updates (bug fixes, new features).
- **Never fabricate orb versions or config content.** The CLI fetches the live orb source; do not hand-write it.

---

## Basic usage

Inline all orb references in a config:

```bash
circleci-migrate orb inline \
  --config .circleci/config.yml \
  --output .circleci/config.inlined.yml
```

Inline only a specific namespace (e.g. `awesomecicd`):

```bash
circleci-migrate orb inline \
  --config .circleci/config.yml \
  --namespace awesomecicd \
  --output .circleci/config.yml
```

The `--output` flag defaults to stdout if omitted. Passing the same path as
`--config` rewrites the file in-place.

---

## Per-repo PR loop

For each repository that needs orb inlining:

1. Clone the repository (or check out the branch that needs the change).
2. Run `orb inline`:
   ```bash
   circleci-migrate orb inline \
     --config .circleci/config.yml \
     --namespace awesomecicd \
     --output .circleci/config.yml
   ```
3. Review the diff (`git diff .circleci/config.yml`) to confirm the orb stanza
   was replaced with inline YAML source.
4. Commit the change to a new branch and open a PR:
   ```bash
   git checkout -b inline-orb-temporary
   git add .circleci/config.yml
   git commit -m "chore: inline awesomecicd orb (temporary, pre-migration)"
   git push origin inline-orb-temporary
   ```
5. Confirm the pipeline passes with the inlined config before merging.
6. Track the PR in the checklist.

---

## Post-cutover revert

Once the orb namespace is resolved in the destination org (the namespace has
been transferred or the orb has been published in the destination):

1. For each repository that was inlined, open a revert PR:
   ```bash
   git checkout -b revert-orb-inline
   git revert <inline-commit-sha>
   git push origin revert-orb-inline
   ```
   Or manually restore the original `orbs:` stanza in the config and remove the
   inline YAML block.
2. Confirm the pipeline resolves the orb reference in the destination org.
3. Merge the revert PR.

---

## Capture pipeline use case

If the extraction pipeline itself (for `secrets capture`) uses an orb that is
not resolvable, inline it before running capture:

```bash
# Inline the orb in the extraction config
circleci-migrate orb inline \
  --config .circleci/config.yml \
  --namespace awesomecicd \
  --output .circleci/config.yml

# Then run secrets capture against the source org
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --output secrets.json
```

---

## Troubleshooting

**Orb not found / resolution error:** confirm the `--token` has access to fetch
the orb. The token must be for an org that can read the orb (e.g., the source org).

**YAML comments lost after inline:** expected — the round-trip serialization
drops comments. If the config has critical comments, inline manually by copying
the orb YAML from `circleci orb source <namespace>/<orb>@<version>`.

**Config grows too large:** inlining embeds the full orb YAML. This is expected
and is why inlining is temporary. After revert the file returns to its original
size.

---

## See also

- [docs/guide.md § Orb-based alternative](../../docs/guide.md#orb-based-alternative-committed-config)
- [CLI reference: orb inline](../../docs/cli/circleci-migrate_orb_inline.md)
