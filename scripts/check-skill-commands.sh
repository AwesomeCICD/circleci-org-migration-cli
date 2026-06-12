#!/usr/bin/env bash
#
# Skill-staleness guard.
#
# Fails if a `circleci-migrate <subcommand>` invocation referenced in any
# skills/**/*.md file is NOT present in the generated CLI reference under
# docs/cli/ — the same staleness gate that protects docs/guide.md via
# scripts/check-doc-flags.sh.
#
# Mechanics:
#   1. Extract all `circleci-migrate` command invocations from skills/**/*.md.
#      Subcommands are the word(s) immediately following `circleci-migrate`
#      (up to 2 levels, e.g. `secrets capture`, `orb inline`).
#      Flags are all `--flag` tokens anywhere in skill files.
#   2. Drop the ignore-list (global/auth plumbing, shell-pipeline noise).
#   3. For each extracted subcommand, confirm the matching docs/cli/ file exists
#      (e.g. `circleci-migrate_secrets_capture.md`).
#      For each extracted flag, confirm it appears in at least one docs/cli/ file.
#   4. Report all missing items and exit 1 if any are found.
#
# Approach: pure grep/awk/sed (no bash regex capture groups) so the script
# works on bash 3.2 (macOS) as well as bash 5 (Linux CI).
#
# Usage:
#   scripts/check-skill-commands.sh [skills-dir] [cli-docs-dir]
#
set -euo pipefail

SKILLS_DIR="${1:-skills}"
CLI_DIR="${2:-docs/cli}"

if [ ! -d "$SKILLS_DIR" ]; then
  echo "skills dir not found: $SKILLS_DIR" >&2
  exit 1
fi
if [ ! -d "$CLI_DIR" ]; then
  echo "CLI docs dir not found: $CLI_DIR" >&2
  exit 1
fi

# Subcommands/words to ignore when they appear immediately after circleci-migrate.
# These are either noise patterns (shell continuations, placeholders) or the
# root `version` command which has no flags worth checking.
IGNORE_SUBCMDS="
version
--help
--debug
"

# Flags to ignore entirely — global/auth plumbing inherited by all subcommands.
# Same conservative policy as check-doc-flags.sh.
IGNORE_FLAGS="
--help
--debug
--token
--source-token
--dest-token
--host
--no-input
--yes
--apply
--output
--json
"

is_ignored_subcmd() {
  local sub="$1" ig
  for ig in $IGNORE_SUBCMDS; do
    [ "$sub" = "$ig" ] && return 0
  done
  return 1
}

is_ignored_flag() {
  local flag="$1" ig
  for ig in $IGNORE_FLAGS; do
    [ "$flag" = "$ig" ] && return 0
  done
  return 1
}

# Build the expected docs/cli filename for a subcommand path.
# e.g. "export"           → "circleci-migrate_export.md"
#      "secrets capture"  → "circleci-migrate_secrets_capture.md"
#      "orb inline"       → "circleci-migrate_orb_inline.md"
subcmd_to_docfile() {
  local sub="$1"
  # Replace spaces with underscores, prepend binary prefix.
  local slug
  slug="$(echo "$sub" | tr ' ' '_')"
  echo "${CLI_DIR}/circleci-migrate_${slug}.md"
}

# Collect skill markdown files.
skill_files="$(find "$SKILLS_DIR" -name '*.md' -type f | sort)"

if [ -z "$skill_files" ]; then
  echo "check-skill-commands: no skill files found under $SKILLS_DIR" >&2
  exit 1
fi

# -----------------------------------------------------------------------
# Step 1: Extract all unique `circleci-migrate <subcmd> [<subcmd2>]` patterns.
#
# Strategy: grep for lines containing `circleci-migrate` (not inside a
# comment-only heading or table row — but for simplicity we include all)
# then extract the subcommand words that follow.
#
# We look for up to 2-level subcommands (e.g. `secrets capture`) using a
# two-pass approach:
#   Pass A: try two-word match first.
#   Pass B: single-word match.
# We deduplicate at the end.
# -----------------------------------------------------------------------

# Extract all "circleci-migrate WORD1 [WORD2]" invocations.
# Only capture lines where WORD1/WORD2 are lowercase-alpha+hyphen (not flags).
all_subcmds="$(
  find "$SKILLS_DIR" -name '*.md' -type f \
    | xargs grep -h 'circleci-migrate' \
    | grep -oE 'circleci-migrate [a-z][a-z0-9-]+( [a-z][a-z0-9-]+)?' \
    | awk '{
        # Strip the "circleci-migrate " prefix
        sub(/^circleci-migrate /, "")
        print
      }' \
    | sort -u
)"

# -----------------------------------------------------------------------
# Step 2: Extract all flags (`--flag-name`) from all skill files.
# These are checked against the CLI reference for the relevant subcommand.
# Since associating a flag to its subcommand across multi-line blocks is
# complex, we use a simpler global check: every non-ignored flag mentioned
# in the skills must appear in at least one CLI reference doc.
# -----------------------------------------------------------------------

all_flags="$(
  find "$SKILLS_DIR" -name '*.md' -type f -print0 \
    | xargs -0 cat \
    | sed \
        -e 's|https\?://[^)[:space:]]*||g' \
        -e 's|\(\.\./\)*[^)[:space:]]*#[^)[:space:]]*||g' \
        -e 's|#[0-9][^)[:space:]]*||g' \
        -e 's/<[a-z][a-z0-9_-]*>//g' \
    | grep -oE -- '--[a-z][a-z0-9-]+' \
    | grep -v -- '-$' \
    | sort -u
)"

# -----------------------------------------------------------------------
# Step 3: Check subcommands.
# -----------------------------------------------------------------------

missing_subcmds=""
checked_subcmds=0

while IFS= read -r subcmd; do
  [ -z "$subcmd" ] && continue

  # Split into word1 and word2.
  word1="${subcmd%% *}"
  word2=""
  if [ "$subcmd" != "$word1" ]; then
    word2="${subcmd#* }"
  fi

  if is_ignored_subcmd "$word1"; then
    continue
  fi

  checked_subcmds=$((checked_subcmds + 1))

  if [ -n "$word2" ] && ! is_ignored_subcmd "$word2"; then
    # Try two-level subcommand first.
    docfile="$(subcmd_to_docfile "$word1 $word2")"
    if [ ! -f "$docfile" ]; then
      # Fall back to single-level (word2 might be a positional arg).
      single="$(subcmd_to_docfile "$word1")"
      if [ ! -f "$single" ]; then
        missing_subcmds="${missing_subcmds}  circleci-migrate ${subcmd}\n"
      fi
    fi
  else
    docfile="$(subcmd_to_docfile "$word1")"
    if [ ! -f "$docfile" ]; then
      missing_subcmds="${missing_subcmds}  circleci-migrate ${subcmd}\n"
    fi
  fi
done <<EOF
$all_subcmds
EOF

# -----------------------------------------------------------------------
# Step 4: Check flags.
# For each non-ignored flag, confirm it appears in at least one CLI doc.
# -----------------------------------------------------------------------

missing_flags=""
checked_flags=0

while IFS= read -r flag; do
  [ -z "$flag" ] && continue

  if is_ignored_flag "$flag"; then
    continue
  fi

  checked_flags=$((checked_flags + 1))

  # Whole-flag match across all CLI reference docs.
  if ! grep -rlqE -- "(^|[^a-z0-9-])${flag}([^a-z0-9-]|\$)" "${CLI_DIR}"/*.md 2>/dev/null; then
    missing_flags="${missing_flags}  ${flag}\n"
  fi
done <<EOF
$all_flags
EOF

# -----------------------------------------------------------------------
# Step 5: Report.
# -----------------------------------------------------------------------

echo "check-skill-commands: checked ${checked_subcmds} subcommand(s) and ${checked_flags} flag(s) from ${SKILLS_DIR}"

errors=""
if [ -n "$missing_subcmds" ]; then
  errors="${errors}\nMissing subcommand docs (no matching docs/cli/ file):\n${missing_subcmds}"
fi
if [ -n "$missing_flags" ]; then
  errors="${errors}\nMissing flags (not found in any ${CLI_DIR}/*.md):\n${missing_flags}"
fi

if [ -n "$errors" ]; then
  echo "" >&2
  echo "ERROR: skill files reference commands or flags absent from ${CLI_DIR}:" >&2
  printf "%b" "$errors" >&2
  echo "" >&2
  echo "Either the skills reference a deprecated/renamed command, or the CLI" >&2
  echo "reference needs to be regenerated ('make docs'). Update the skill to" >&2
  echo "use the current command name, or add the item to the IGNORE list in" >&2
  echo "scripts/check-skill-commands.sh if it is genuine shell noise." >&2
  exit 1
fi

echo "check-skill-commands: OK (all referenced subcommands and flags exist in ${CLI_DIR})"
