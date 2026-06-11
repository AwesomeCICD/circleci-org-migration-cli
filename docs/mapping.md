# The mapping file (`mapping.json`)

`sync` and `migrate` accept an optional `--mapping <file>` (JSON) that tells the
tool how the **source** identifiers map to the **destination**. This page
explains when you need it and what each key does.

For the end-to-end flow, see the [migration guide](guide.md).

---

## When do you need a mapping file?

`sync` infers the destination from the manifest: by default the **destination
org is the same as the source org** recorded in the manifest. That is correct
only when you are re-applying into the same org. In every other case the mapping
file overrides that inference.

You **need** a mapping file when:

- **The destination org name differs from the source.** This is the most common
  case. Without `org.to`, `sync` targets your own source org and prints a
  prominent warning. `org.to` is the **only** key strictly required to retarget.
- **The destination uses a different slug type** (e.g. OAuth `gh/acme` →
  GitHub App `circleci/<uuid>`). Project slugs change shape, so you must map
  them with the `projects` key.
- **Repos moved to a new GitHub org** and you want a single override (or a
  partial move where only some repos changed) — use `github_org` and/or
  per-project `projects` entries.

You **do not** need one when re-applying a manifest into the same org under the
same names.

> `migrate` also accepts `--mapping`, though for a straight same-name migration
> you can instead pass `--dest-org` directly on `migrate`.

---

## Schema

```json
{
  "org": { "from": "gh/acme", "to": "gh/acme-new" },
  "projects": { "gh/acme/web": "gh/acme-new/web" },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

All three keys are optional except `org.to`, which is required to retarget the
destination org.

### `org` — `{ "from", "to" }`

The source and destination **org slugs**. `org.to` is what retargets the
destination; without it, `sync` runs against the source org. `from` documents
the source (it should match the manifest's org).

```json
"org": { "from": "gh/acme", "to": "gh/acme-new" }
```

For a cross-type move the `to` slug changes shape:

```json
"org": { "from": "gh/acme", "to": "circleci/22222222-2222-2222-2222-222222222222" }
```

### `projects` — `{ "<src-slug>": "<dst-slug>" }`

Remaps individual **project slugs** from source to destination. Required when a
project's slug cannot be derived from the org slug alone — most importantly for
**GitHub App destinations**, whose project slug is
`circleci/<org-id>/<project-id>` rather than `gh/<org>/<repo>`.

```json
"projects": {
  "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
  "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api"
}
```

For a simple same-type move where only the org name changes, the tool derives
destination slugs automatically and you can usually omit `projects`.

### `github_org` — `{ "from", "to" }`

Rewrites the **GitHub repository owner** when repos have moved to a new GitHub
org. Use it (with `--github-token`) instead of, or alongside, the
`--dest-github-org` flag.

- An explicit `github_org` entry in the mapping **overrides** the
  `--dest-github-org` flag.
- Use per-project `projects` entries for a **partial** move where only some
  repos changed GitHub org.

```json
"github_org": { "from": "acme", "to": "acme-new" }
```

---

## Full example (cross-type, repos moved)

```json
{
  "org": {
    "from": "gh/acme",
    "to": "circleci/22222222-2222-2222-2222-222222222222"
  },
  "projects": {
    "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/web",
    "gh/acme/api": "circleci/22222222-2222-2222-2222-222222222222/api"
  },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"

circleci-migrate sync \
  --manifest manifest.json \
  --secrets secrets.json \
  --mapping mapping.json \
  --apply --yes
```

---

## See also

- [Migration guide](guide.md) — the full export → capture → sync walkthrough.
- [Troubleshooting](troubleshooting.md) — including "sync targeted the wrong
  org".
- [CLI reference](cli/circleci-migrate_sync.md) — the `sync` flag table.
