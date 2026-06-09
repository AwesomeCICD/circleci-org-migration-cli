#!/bin/sh
# Resolve "latest" to a concrete tag from the GitHub Releases API and write it
# to /tmp/.circleci-migrate-version for use as a cache-key checksum input.
# If a specific version was requested, write it verbatim.

set -e

repo="CircleCI-Public/circleci-org-migration-cli"
requested_ver="${ORB_PARAM_VERSION:-latest}"

if [ "$requested_ver" = "latest" ]; then
  resolved=$(curl -sfL "https://api.github.com/repos/${repo}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 \
    | sed 's/.*"\(v[^"]*\)".*/\1/')
  if [ -z "$resolved" ]; then
    echo "ERROR: could not resolve latest release tag from GitHub API" >&2
    exit 1
  fi
  echo "$resolved" > /tmp/.circleci-migrate-version
else
  echo "$requested_ver" > /tmp/.circleci-migrate-version
fi

echo "Resolved circleci-migrate version: $(cat /tmp/.circleci-migrate-version)"
