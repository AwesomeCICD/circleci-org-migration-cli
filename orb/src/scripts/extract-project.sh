#!/bin/sh
# Capture one project's secret values from the job environment.
# Parameters are passed as environment variables by the orb command:
#   ORB_PARAM_MANIFEST      — path to the exported manifest file
#   ORB_PARAM_PROJECT_SLUG  — project slug (must match the manifest)
#   ORB_PARAM_OUTPUT        — output path for the secrets bundle

set -e

manifest="${ORB_PARAM_MANIFEST:-manifest.json}"
project_slug="${ORB_PARAM_PROJECT_SLUG}"
output="${ORB_PARAM_OUTPUT:-secrets.json}"

circleci-migrate secrets extract \
  --manifest "$manifest" \
  --project "$project_slug" \
  --output "$output"
