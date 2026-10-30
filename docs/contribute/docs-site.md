# Docs site

The docs site is Hugo and the Hextra theme, built from the Markdown in this repository. CircleCI builds it on every branch and publishes it to GitHub Pages from `main`.

## Preview it

Install Hugo extended 0.168.0 and Go, then run:

```bash
make site-serve     # stage the Markdown and serve http://localhost:1313/
make site-check     # build, then check every link and heading anchor offline (needs lychee 0.24.2)
```

Restage after you edit a page: stop the server and run `make site-serve` again. `site/stage.sh` copies the pages into `site/content/` (not tracked). It never changes the sources.

Check: `make site` ends with no warning. A warning fails the build.

## Add or move a page

List the page in `site/nav.txt`, in the group where it belongs. `site/stage.sh` fails on a `docs/**/*.md` file that nav.txt misses and on a listed file that does not exist. `docs/cli/*.md` is generated: never edit it by hand.

Site-only pages, such as the plan builder, live in `site/pages/` and are staged at the top of the site (`site/pages/plan.md` is `/plan/`).

## How CI builds and publishes it

The workflow `docs-site` in `.circleci/continue_config.yml` has two jobs:

- `docs-site-build` runs on every branch with no context. It tests the plan builder (`node --test site/tests/`), stages the Markdown, runs `hugo --minify --panicOnWarning` and checks the links with `site/check.sh`. Hugo, lychee and Node are pinned and SHA-256 verified.
- `docs-site-publish` runs on `main` only, after the build. It pushes the built site to the `gh-pages` branch with `site/publish.sh`. Without `GITHUB_TOKEN` it publishes nothing and says so.

## One-time setup

1. Create a CircleCI context named `docs-site-publish` with `GITHUB_TOKEN`, a fine-grained token with Contents: write on this repository.
2. Merge to `main`. The first publish creates the `gh-pages` branch.
3. Turn on GitHub Pages with `gh-pages` and `/` as the source:

```bash
echo '{"source":{"branch":"gh-pages","path":"/"}}' \
  | gh api -X POST "repos/AwesomeCICD/circleci-org-migration-cli/pages" --input -
```

If Pages already exists, use `-X PUT`. Send the JSON body as shown: with `-f "source[path]=/"` the API ignores the path.

Check: `https://awesomecicd.github.io/circleci-org-migration-cli/` loads, in light and dark mode.

## Moving to CircleCI-Labs

The site works the same under the CircleCI-Labs organization. Change these four things:

1. In `site/hugo.yaml`, set `baseURL` to `https://circleci-labs.github.io/<repository>/` and the GitHub menu `url` to the new repository. The footer, `stage.sh` and `publish.sh` read that URL.
2. Change the site link in `README.md`.
3. Create the `docs-site-publish` context for the new project, merge to `main`, and enable Pages again with the `gh api` command above (new owner in the path).
4. Add an entry for the project to `data/projects.yaml` in `CircleCI-Labs/circleci-labs.github.io`, then build and push that hub.

GitHub redirects the repository URL after a transfer, but not the old Pages path. Update the links that point to the old site.

## Upgrade a pinned tool

The versions of Hugo, lychee and Node are in the `docs-site` command and the `docs-site-build` job. A new version needs its new SHA-256 value too, from the release checksums. The Hextra version is in `site/go.mod` and `site/go.sum`.
