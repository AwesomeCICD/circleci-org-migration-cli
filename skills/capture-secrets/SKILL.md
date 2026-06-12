---
name: capture-secrets
description: >
  Capture CircleCI secret values that the API cannot expose. Trigger phrases:
  "capture CircleCI secrets", "extract CircleCI context secrets",
  "capture environment variable values from CircleCI", "migrate CircleCI
  secrets", "run secrets capture", "secrets aren't migrating",
  "my env vars are empty after migration". Covers the secrets capture / extract
  decision tree, encryption options, restricted-context handling, and the
  rotation step after cutover.
---

# Capture Secrets

The CircleCI API **never returns environment-variable values** — every value is
masked in every API response. `circleci-migrate secrets capture` works around
this by running a short-lived pipeline inside the source org, where the platform
injects the real values into the job environment.

---

## Prerequisites gate

**STOP AND ASK if any of the following is missing — never fabricate values:**

- [ ] `manifest.json` exists (produced by `circleci-migrate export`)
- [ ] Source org CircleCI API token (`CIRCLECI_SOURCE_TOKEN`) — needs permission to trigger pipelines
- [ ] A project in the source org to use as the "host project" for context extraction (any project works)
- [ ] Decision on encryption: use `--generate-key` (fresh keypair, recommended) or an existing SSH key?
- [ ] Decision on storage: CircleCI artifact (default), S3, or both?

---

## Task-progress checklist

- [ ] Manifest loaded; context and project list reviewed
- [ ] Encryption option decided and key material ready
- [ ] Host project identified
- [ ] `secrets capture` triggered and pipeline completed
- [ ] Bundle downloaded and decrypted to `secrets.json`
- [ ] `secrets.json` confirmed present and non-empty
- [ ] After migration confirmed healthy: secrets rotated and `secrets.json` deleted

---

## Guardrails

- **Never print or display the contents of `secrets.json` or any encrypted bundle.** These files contain plaintext secret values.
- **Never run `secrets capture` with `--no-encrypt` for production secrets.** Plaintext artifacts persist in CircleCI for at least 1 day and there is no delete-artifact API.
- **Always recommend `--artifact-retention-days 1`** to minimize the in-CircleCI exposure window.
- **Use a private project** as the host project. Never use a public project for the extraction pipeline.
- **Do NOT commit `secrets.json` to version control.** It is written with `0600` permissions; keep it that way.
- **After the migration is confirmed healthy, rotate every captured value.** Treat every captured value as potentially exposed once written to a file or artifact, even if encrypted.
- **Never fabricate context names or variable names.** Only reference what appears in `manifest.json`.

---

## Decision tree: which capture method?

```
Do you want CLI-orchestrated capture (no committed config needed)?
  YES → Use `secrets capture` (recommended for most cases)
  NO  → Use the orb-based approach (committed config, more pipeline control)

For `secrets capture`:
  Do you have an existing SSH key you want to use for encryption?
    YES → --ssh-public-key ~/.ssh/id_ed25519.pub --ssh-private-key ~/.ssh/id_ed25519
    NO  → --generate-key (creates a fresh age keypair)

  Are any contexts restricted (group/expression restrictions)?
    YES and you want to skip them → --skip-restricted-contexts (default: true)
    YES and you want to lift restrictions temporarily → --remove-restrictions (explicit opt-in)

  Do you need SSH private key capture as well as env vars?
    YES (default) → SSH key extraction is on by default
    NO  → --no-ssh-keys

  Where should the bundle be stored?
    CircleCI artifact only (default) → omit --storage
    S3 only → --storage s3 --s3-bucket <bucket> [--s3-prefix migration/]
    Both   → --storage both --s3-bucket <bucket>
```

---

## Recommended: interactive guided capture

On an interactive TTY, launch the 6-step guided walkthrough:

```bash
circleci-migrate secrets capture
```

The walkthrough prompts for: manifest path, contexts/projects to capture, host
project, encryption, storage, and artifact retention.

---

## Non-interactive (CI-safe) — encrypted bundle, 1-day retention

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

`--enable-trigger` enables `api-trigger-with-config` if not already on, and
restores it after the run. Add it if capture fails with
`api-trigger-with-config disabled`.

---

## Existing SSH key for encryption

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt \
  --ssh-public-key ~/.ssh/id_ed25519.pub \
  --ssh-private-key ~/.ssh/id_ed25519 \
  --artifact-retention-days 1 \
  --enable-trigger \
  --output secrets.json
```

---

## Scope to specific contexts or projects

```bash
# Only specific contexts
circleci-migrate secrets capture \
  --manifest manifest.json \
  --context deploy-prod \
  --context shared \
  --host-project gh/acme/web \
  --encrypt --generate-key \
  --output secrets.json

# Only specific projects (for project env vars)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --project gh/acme/web \
  --encrypt --generate-key \
  --output secrets.json
```

---

## Restricted contexts

If a context has group/expression restrictions that block the inline pipeline:

```bash
# Skip restricted contexts (attach a warning; those vars need manual entry later)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --skip-restricted-contexts \
  --encrypt --generate-key \
  --output secrets.json

# Temporarily lift restrictions (explicit opt-in; restrictions are restored after)
circleci-migrate secrets capture \
  --manifest manifest.json \
  --remove-restrictions \
  --encrypt --generate-key \
  --output secrets.json
```

For uncaptured variables, use `--missing-secrets placeholder` in `sync` so the
variable name is created and can be filled in manually.

---

## S3 storage

```bash
circleci-migrate secrets capture \
  --manifest manifest.json \
  --encrypt --generate-key \
  --storage s3 \
  --s3-bucket my-migration-bucket \
  --s3-prefix migration/ \
  --output secrets.json
```

Requires the `aws` CLI and AWS credentials configured in the source org project.

---

## Orb-based alternative (committed config, full pipeline control)

For large numbers of contexts or when you need full pipeline customization,
commit `manifest.json` to a repo in the source org and use the migration orb.

Each job must reference **exactly one context** under the `context:` key — this
is how CircleCI injects the real values. Never mix two contexts in one job.

```yaml
# .circleci/config.yml in your SOURCE org
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-deploy-prod
          context_name: deploy-prod
          context: [deploy-prod]
      - migrate/extract_context:
          name: extract-shared
          context_name: shared
          context: [shared]
      - migrate/merge:
          name: merge-secrets
          requires: [extract-deploy-prod, extract-shared]
```

For many contexts, use a matrix with an explicit `alias`:

```yaml
version: "2.1"
orbs:
  migrate: awesomecicd/circleci-org-migration@0.8.0
workflows:
  capture-secrets:
    jobs:
      - migrate/extract_context:
          name: extract-<< matrix.context_name >>
          context: [<< matrix.context_name >>]
          matrix:
            alias: extract_contexts
            parameters:
              context_name: [deploy-prod, shared, build, staging]
      - migrate/merge:
          name: merge-secrets
          requires: [extract_contexts]
```

Download `secrets.json` from the `merge` job's Artifacts tab. If the bundle is
age-encrypted, decrypt it locally:

```bash
circleci-migrate secrets decrypt \
  --identity-file ./migration-identity.age \
  secrets.json.age
```

---

## Namespace overlap window

If the orb reference in the config (`awesomecicd/circleci-org-migration`) cannot
be resolved in the destination org, delegate to
[apply-inline-orbs](../apply-inline-orbs/SKILL.md) to inline the orb source
before the capture pipeline runs.

---

## After the migration: rotation and cleanup

Once the destination migration is confirmed healthy:

1. **Rotate every captured secret value** — treat every captured value as
   potentially exposed. Update the value in the destination org and wherever
   it is consumed.
2. Delete `secrets.json` from your local machine.
3. Delete the CircleCI build artifacts from the extraction run (if the 1-day
   retention window has not already expired them).
4. Delete any local shells or logs that may contain captured values.

---

## Troubleshooting

**`api-trigger-with-config disabled`:** add `--enable-trigger`. The flag
temporarily enables unversioned pipeline configs and restores the setting after.

**`0 context(s), 0 value(s)` in merged bundle:** the manifest path or context
names are wrong, or the capture ran against the wrong org. Verify `manifest.json`
is from the source org and re-run.

**Context restricted — pipeline rejected:** use `--skip-restricted-contexts`
or `--remove-restrictions`. Check the capture output for warnings listing the
skipped contexts.

**Matrix missing alias:** when using the orb matrix pattern, the `merge` job
must depend on the matrix `alias`, not on individual job names. Add
`alias: extract_contexts` under `matrix:` and use that alias in `requires:`.

---

## See also

- [docs/guide.md § Step 2 — Capture secret values](../../docs/guide.md#5-step-2--capture-secret-values)
- [docs/cutover-runbook.md § Step 2](../../docs/cutover-runbook.md#step-2--capture-secret-values-recommended-guided-secrets-capture)
- [CLI reference: secrets capture](../../docs/cli/circleci-migrate_secrets_capture.md)
- [CLI reference: secrets decrypt](../../docs/cli/circleci-migrate_secrets_decrypt.md)
- [CLI reference: secrets merge](../../docs/cli/circleci-migrate_secrets_merge.md)
