#!/usr/bin/env bash
#
# Docs-staleness guard.
#
# Fails if a released CLI flag that appears in the generated reference under
# docs/cli/ is NOT mentioned anywhere in the hand-written docs. The generated
# docs/cli/ are kept current by the `docs-check` CI gate, so they are the source
# of truth for "what flags shipped"; the hand-written pages drift behind the
# code. docs/reference/flags.md lists every flag one line each and is what
# normally satisfies this gate. This guard is deliberately crude — a grep, not a
# parser — but it catches a whole released feature flag going undocumented.
#
# Mechanics:
#   1. Enumerate long flags (lines like `--flag`) from docs/cli/*.md.
#   2. Drop the ignore-list (global/auth plumbing, completion-shell flags, and
#      a few internal mechanics flags that are intentionally not documented).
#   3. For each remaining flag, grep the hand-written docs (every *.md under
#      the given paths, never the generated CLI dir). Any flag with no hit fails
#      the build and is listed.
#
# Usage:
#   scripts/check-doc-flags.sh [cli-docs-dir] [doc-path ...]
#
# Each doc-path is a Markdown file or a directory searched recursively for
# *.md. With no doc-path, the default is docs/ plus README.md. The CLI docs
# directory is always excluded from the search so a flag cannot satisfy the
# gate by appearing in its own generated reference.
#
set -euo pipefail

CLI_DIR="${1:-docs/cli}"
if [ "$#" -gt 0 ]; then shift; fi
if [ "$#" -gt 0 ]; then
  DOC_PATHS=("$@")
else
  DOC_PATHS=(docs README.md)
fi

if [ ! -d "$CLI_DIR" ]; then
  echo "CLI docs dir not found: $CLI_DIR" >&2
  exit 1
fi

# Collect the hand-written doc files into one list, skipping the CLI dir.
DOC_FILES=()
cli_abs="$(cd "$CLI_DIR" && pwd)"
for p in "${DOC_PATHS[@]}"; do
  if [ -d "$p" ]; then
    while IFS= read -r f; do
      case "$(cd "$(dirname "$f")" && pwd)/" in
        "$cli_abs"/*) continue ;;
      esac
      DOC_FILES+=("$f")
    done < <(find "$p" -type f -name '*.md' | sort)
  elif [ -f "$p" ]; then
    DOC_FILES+=("$p")
  else
    echo "doc path not found: $p" >&2
    exit 1
  fi
done
if [ "${#DOC_FILES[@]}" -eq 0 ]; then
  echo "no hand-written docs found under: ${DOC_PATHS[*]}" >&2
  exit 1
fi
DOC_LABEL="${DOC_PATHS[*]}"

# Flags intentionally NOT required in the docs. Keep this list conservative and
# justified — only global/auth plumbing, shell-completion-only flags, and a few
# internal-mechanics flags belong here. Do NOT add a real feature flag to dodge
# the gate; document it in docs/reference/flags.md instead.
IGNORE="
--help
--debug
--config
--token
--source-token
--dest-token
--no-descriptions
--prefix
--branch
--poll-timeout
--usage-timeout
--identity-file
--recipient
--recipient-file
--strict
--namespace
--ssh-keys
--rawfile
"

is_ignored() {
  local flag="$1" ig
  for ig in $IGNORE; do
    [ "$flag" = "$ig" ] && return 0
  done
  return 1
}

# Enumerate long flags from the generated CLI reference. Flag lines in the
# generated markdown look like:  `      --skip-ciam   Skip ...`
flags="$(grep -rohE -- '--[a-z][a-z0-9-]+' "$CLI_DIR"/*.md \
  | grep -oE -- '--[a-z][a-z0-9-]+' \
  | sort -u)"

missing=""
checked=0
for flag in $flags; do
  if is_ignored "$flag"; then
    continue
  fi
  checked=$((checked + 1))
  # Whole-flag match: word boundaries so `--skip` does not match `--skip-ciam`.
  if ! grep -qE -- "(^|[^a-z0-9-])${flag}([^a-z0-9-]|\$)" "${DOC_FILES[@]}"; then
    missing="${missing}${flag}\n"
  fi
done

echo "check-doc-flags: checked ${checked} released flag(s) from ${CLI_DIR} against ${DOC_LABEL}"

if [ -n "$missing" ]; then
  echo "" >&2
  echo "ERROR: the following released flag(s) are documented in ${CLI_DIR} but" >&2
  echo "absent from the hand-written docs (${DOC_LABEL}):" >&2
  echo "" >&2
  printf "%b" "$missing" | sed 's/^/  /' >&2
  echo "" >&2
  echo "Document each flag in docs/reference/flags.md, or — if it is genuinely internal/global —" >&2
  echo "add it (with justification) to the IGNORE list in scripts/check-doc-flags.sh." >&2
  exit 1
fi

echo "check-doc-flags: OK (every released feature flag is documented in ${DOC_LABEL})"
