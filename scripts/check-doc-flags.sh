#!/usr/bin/env bash
#
# Docs-staleness guard.
#
# Fails if a released CLI flag that appears in the generated reference under
# docs/cli/ is NOT mentioned anywhere in the hand-written walkthrough
# docs/guide.md. The generated docs/cli/ are kept current by the `docs-check`
# CI gate, so they are the source of truth for "what flags shipped"; the
# hand-written guide drifts behind the code (this is the v0.5–v0.7 drift the
# audit flagged). This guard is deliberately crude — a grep, not a parser — but
# it catches a whole released feature flag going undocumented.
#
# Mechanics:
#   1. Enumerate long flags (lines like `--flag`) from docs/cli/*.md.
#   2. Drop the ignore-list (global/auth plumbing, completion-shell flags, and
#      a few internal mechanics flags that are intentionally not in the guide).
#   3. For each remaining flag, grep docs/guide.md. Any flag with no hit fails
#      the build and is listed.
#
# Usage:
#   scripts/check-doc-flags.sh [cli-docs-dir] [guide-file]
#
set -euo pipefail

CLI_DIR="${1:-docs/cli}"
GUIDE="${2:-docs/guide.md}"

if [ ! -d "$CLI_DIR" ]; then
  echo "CLI docs dir not found: $CLI_DIR" >&2
  exit 1
fi
if [ ! -f "$GUIDE" ]; then
  echo "guide file not found: $GUIDE" >&2
  exit 1
fi

# Flags intentionally NOT required in the guide. Keep this list conservative and
# justified — only global/auth plumbing, shell-completion-only flags, and a few
# internal-mechanics flags belong here. Do NOT add a real feature flag to dodge
# the gate; document it in the guide instead.
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
  if ! grep -qE -- "(^|[^a-z0-9-])${flag}([^a-z0-9-]|\$)" "$GUIDE"; then
    missing="${missing}${flag}\n"
  fi
done

echo "check-doc-flags: checked ${checked} released flag(s) from ${CLI_DIR} against ${GUIDE}"

if [ -n "$missing" ]; then
  echo "" >&2
  echo "ERROR: the following released flag(s) are documented in ${CLI_DIR} but" >&2
  echo "absent from ${GUIDE}:" >&2
  echo "" >&2
  printf "%b" "$missing" | sed 's/^/  /' >&2
  echo "" >&2
  echo "Document each flag in ${GUIDE}, or — if it is genuinely internal/global —" >&2
  echo "add it (with justification) to the IGNORE list in scripts/check-doc-flags.sh." >&2
  exit 1
fi

echo "check-doc-flags: OK (every released feature flag is documented in ${GUIDE})"
