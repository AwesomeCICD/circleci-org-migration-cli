#!/usr/bin/env bash
#
# Post-release smoke test for circleci-migrate.
#
# Downloads the published GitHub Release tarball for the given tag, extracts
# it, and runs a series of sanity checks against the real shipped binary:
#
#   1. circleci-migrate version
#   2. circleci-migrate --help
#   3. secrets merge against two minimal inline fixture bundles
#   4. README curl install snippet (verbatim)
#
# Called by the `release-smoke` CircleCI job immediately after `goreleaser`
# uploads release assets. The job passes CIRCLE_TAG as $1; the optional $2
# selects the platform tuple (default: linux_amd64).
#
# Usage:
#   scripts/smoke-release.sh <tag> [<os>_<arch>]
#   scripts/smoke-release.sh v1.2.3
#   scripts/smoke-release.sh v1.2.3 linux_arm64
#
# Exit codes: 0 = all checks pass; non-zero = first failing step.
set -euo pipefail

TAG="${1:?Usage: smoke-release.sh <tag> [os_arch]}"
PLATFORM="${2:-linux_amd64}"

REPO="AwesomeCICD/circleci-org-migration-cli"
VERSION="${TAG#v}"   # strip leading 'v'  → e.g. "1.2.3"
ARCHIVE="circleci-migrate_${VERSION}_${PLATFORM}.tar.gz"
URL="https://github.com/${REPO}/releases/download/${TAG}/${ARCHIVE}"

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

step() { echo ""; echo "==> $*"; }

# ---------------------------------------------------------------------------
# 1. Download the release tarball (with retries — the asset may take a moment
#    to appear after GoReleaser finishes uploading).
# ---------------------------------------------------------------------------
step "Download ${URL}"
TARBALL="${WORK_DIR}/${ARCHIVE}"
MAX_ATTEMPTS=10
DELAY=15
attempt=0
while true; do
  attempt=$(( attempt + 1 ))
  if curl -fL --connect-timeout 20 --max-time 120 -o "$TARBALL" "$URL"; then
    echo "Download succeeded on attempt ${attempt}."
    break
  fi
  if [ "$attempt" -ge "$MAX_ATTEMPTS" ]; then
    echo "ERROR: Failed to download ${URL} after ${MAX_ATTEMPTS} attempts." >&2
    exit 1
  fi
  echo "Attempt ${attempt} failed; retrying in ${DELAY}s..."
  sleep "$DELAY"
done

# ---------------------------------------------------------------------------
# 2. Extract and locate the binary.
# ---------------------------------------------------------------------------
step "Extract ${ARCHIVE}"
EXTRACT_DIR="${WORK_DIR}/extract"
mkdir -p "$EXTRACT_DIR"
tar -xzf "$TARBALL" -C "$EXTRACT_DIR"

BIN="${EXTRACT_DIR}/circleci-migrate"
if [ ! -x "$BIN" ]; then
  echo "ERROR: binary not found or not executable at ${BIN}" >&2
  ls -la "$EXTRACT_DIR"
  exit 1
fi
echo "Binary: $(ls -lh "$BIN")"

# ---------------------------------------------------------------------------
# 3. version + help
# ---------------------------------------------------------------------------
step "circleci-migrate version"
"$BIN" version

step "circleci-migrate --help"
"$BIN" --help

# ---------------------------------------------------------------------------
# 4. secrets merge — two minimal inline fixture bundles
#    SecretBundle schema_version must match the compiled constant ("1").
# ---------------------------------------------------------------------------
step "secrets merge (fixture bundles)"
FIXTURE_DIR="${WORK_DIR}/fixtures"
mkdir -p "$FIXTURE_DIR"

# Bundle A: one context secret
printf '%s\n' \
  '{"schema_version":"1","context_secrets":{"smoke-ctx-a":{"SMOKE_VAR_A":"value-a"}}}' \
  > "${FIXTURE_DIR}/bundle-a.json"

# Bundle B: one project secret (different context)
printf '%s\n' \
  '{"schema_version":"1","context_secrets":{"smoke-ctx-b":{"SMOKE_VAR_B":"value-b"}}}' \
  > "${FIXTURE_DIR}/bundle-b.json"

MERGED="${FIXTURE_DIR}/merged.json"
"$BIN" secrets merge \
  -o "$MERGED" \
  "${FIXTURE_DIR}/bundle-a.json" \
  "${FIXTURE_DIR}/bundle-b.json"

# Validate the merged output contains both variables.
if ! grep -q "SMOKE_VAR_A" "$MERGED"; then
  echo "ERROR: merged bundle missing SMOKE_VAR_A" >&2; exit 1
fi
if ! grep -q "SMOKE_VAR_B" "$MERGED"; then
  echo "ERROR: merged bundle missing SMOKE_VAR_B" >&2; exit 1
fi
echo "secrets merge: OK (SMOKE_VAR_A and SMOKE_VAR_B present in merged output)"

# ---------------------------------------------------------------------------
# 5. README curl install snippet (verbatim)
#    The README installs to /usr/local/bin; we redirect to a scratch dir
#    to avoid requiring sudo in CI while still exercising the exact download
#    path (VERSION resolved from the GitHub API → tarball → binary).
# ---------------------------------------------------------------------------
step "README curl install snippet (scratch dir)"
SCRATCH="${WORK_DIR}/readme-install"
mkdir -p "$SCRATCH"

README_VERSION=$(curl -sfL \
  "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | cut -d'"' -f4)
echo "README_VERSION resolved to: ${README_VERSION}"

curl -sfL "https://github.com/${REPO}/releases/download/${README_VERSION}/circleci-migrate_${README_VERSION#v}_linux_amd64.tar.gz" \
  | tar -xz -C "$SCRATCH"

README_BIN="${SCRATCH}/circleci-migrate"
if [ ! -x "$README_BIN" ]; then
  echo "ERROR: README install did not produce an executable binary at ${README_BIN}" >&2
  ls -la "$SCRATCH"
  exit 1
fi
"$README_BIN" version
echo "README curl install snippet: OK"

# ---------------------------------------------------------------------------
# All checks passed.
# ---------------------------------------------------------------------------
echo ""
echo "All release smoke checks passed for tag ${TAG} (${PLATFORM})."
