#!/bin/sh
# Capture one context's secret values from the job environment.
# Parameters are passed as environment variables by the orb command:
#   ORB_PARAM_MANIFEST      — path to the exported manifest file
#   ORB_PARAM_CONTEXT_NAME  — context name (must match the manifest)
#   ORB_PARAM_OUTPUT        — output path for the secrets bundle

set -e

manifest="${ORB_PARAM_MANIFEST:-manifest.json}"
context_name="${ORB_PARAM_CONTEXT_NAME}"
output="${ORB_PARAM_OUTPUT:-secrets.json}"

circleci-migrate secrets extract \
  --manifest "$manifest" \
  --context "$context_name" \
  --output "$output"
