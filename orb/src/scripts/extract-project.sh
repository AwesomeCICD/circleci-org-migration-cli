#!/bin/sh
# Capture one project's secret values from the job environment.
# Parameters are passed as environment variables by the orb command:
#   ORB_PARAM_MANIFEST           — path to the exported manifest file
#   ORB_PARAM_PROJECT_SLUG       — project slug (must match the manifest)
#   ORB_PARAM_OUTPUT             — output path override (optional; if empty the
#                                  script writes to captured/secrets-project-<safe>.json)
#   ORB_PARAM_ENCRYPT_RECIPIENT  — age/SSH public key recipient string; if
#                                  non-empty, encrypt the output bundle
#                                  (writes <output>.age instead of plaintext).
#
# SECURITY: ORB_PARAM_ENCRYPT_RECIPIENT is a PUBLIC key — safe to embed in
# environment variables. Private keys are NEVER used here.

set -e

manifest="${ORB_PARAM_MANIFEST:-manifest.json}"
project_slug="${ORB_PARAM_PROJECT_SLUG}"
encrypt_recipient="${ORB_PARAM_ENCRYPT_RECIPIENT:-}"

# Sanitize the project slug: replace every '/' with '_' so the filename is
# always a flat file rather than a nested path (slugs like "gh/org/repo"
# would otherwise cause "no such file or directory" at write time).
safe_slug=$(printf '%s' "$project_slug" | tr '/' '_')

if [ -n "${ORB_PARAM_OUTPUT:-}" ]; then
  output="$ORB_PARAM_OUTPUT"
else
  output="captured/secrets-project-${safe_slug}.json"
fi

# Ensure the output directory exists.
mkdir -p "$(dirname "$output")"

if [ -n "$encrypt_recipient" ]; then
  circleci-migrate secrets extract \
    --manifest "$manifest" \
    --project "$project_slug" \
    --output "$output" \
    --encrypt \
    --recipient "$encrypt_recipient"
else
  circleci-migrate secrets extract \
    --manifest "$manifest" \
    --project "$project_slug" \
    --output "$output"
fi
