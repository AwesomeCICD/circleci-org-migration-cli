# The mapping file

`mapping.json` tells the CLI where the destination is. You need it whenever the destination org differs from the source, and the only line that is always required is `org.to`.

Without it, `sync` targets the source org named in the manifest and prints a warning. That is correct only when you re-apply into the same org.

## When you need it

| Situation | What to put in `mapping.json` |
|---|---|
| Destination org name differs from the source | `org` with `from` and `to`. |
| Slug type changes, such as `gh/acme` to `circleci/<id>` | `org.to` set to the destination slug. `sync` finds or creates App projects by name. |
| Repos moved to a new GitHub org | `github_org` with `from` and `to`, or per-project `projects` entries. |
| Same org, same names | No mapping file. |

`migrate` also accepts `--mapping`. For a straight same-name move you can pass `--dest-org` to `migrate` instead.

## Schema

```json
{
  "org": { "from": "gh/acme", "to": "circleci/22222222-2222-2222-2222-222222222222" },
  "projects": { "gh/acme/web": "circleci/22222222-2222-2222-2222-222222222222/3f1c0b52-7d0e-4a57-9a53-0a6d4f1e9b10" },
  "github_org": { "from": "acme", "to": "acme-new" }
}
```

All three keys are optional except `org.to`.

| Key | Meaning |
|---|---|
| `org` | Source and destination org slugs. `to` retargets the destination. `from` should match the manifest. |
| `projects` | Source project slug to destination project slug. Optional for most moves. |
| `github_org` | Rewrites the GitHub owner of every repo. Overrides `--dest-github-org`. Needs `--github-token`. |

An entry in `projects` whose key has no `/` maps a context name to a destination context name. This applies to `secrets transfer --mapping`.

## Project entries are optional

A GitHub App project slug is `circleci/<org-id>/<project-id>`, and the project ID exists only after the project does. You cannot write these entries up front.

You do not have to. For a source project with no entry, project-type context restrictions and `secrets transfer --include-project-vars` match the destination project by name. An entry always wins over a name match. Add entries only to review a match or override it.

## Generate it

Generate the file from the manifest. For an OAuth destination it fills in every project. For a GitHub App destination, write the `org` line first, run `sync --apply --skip-pipelines` to create the projects, then generate.

```bash
circleci-migrate mapping generate --manifest manifest.json --dest-org "$DEST_ORG" --output mapping.json
```

Check: `mapping.json` has `org.from` set to your source and `org.to` set to the destination.

`mapping generate` writes only `org` and `projects`. Add `github_org` back by hand if you use it.

## Partial repo moves

When only some repos changed GitHub org, use `projects` entries for those repos. Use `github_org` when every repo moved. Resolving the new repo IDs needs a GitHub token with repo read:

```bash
export GITHUB_TOKEN="<github-pat-with-repo-read>"
circleci-migrate sync --manifest manifest.json --mapping mapping.json --apply
```

Check: no project shows `manual` for a wrong `external_id`.

## See also

- [How it works](how-it-works.md)
- [Troubleshooting](../reference/troubleshooting.md)
- [`sync` flags](../cli/circleci-migrate_sync.md)
