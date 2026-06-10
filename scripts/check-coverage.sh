#!/usr/bin/env bash
#
# Fail if total test coverage is below a threshold.
#
# Usage:
#   scripts/check-coverage.sh [coverage-profile] [threshold-percent]
#
# Threshold resolution order: $2 arg, then $COVERAGE_THRESHOLD, then default.
set -euo pipefail

PROFILE="${1:-coverage.out}"
THRESHOLD="${2:-${COVERAGE_THRESHOLD:-85}}"

if [ ! -f "$PROFILE" ]; then
  echo "coverage profile not found: $PROFILE" >&2
  exit 1
fi

total="$(go tool cover -func="$PROFILE" | awk '/^total:/ {gsub(/%/,"",$3); print $3}')"

if [ -z "$total" ]; then
  echo "could not determine total coverage from $PROFILE" >&2
  exit 1
fi

echo "Total coverage: ${total}% (threshold: ${THRESHOLD}%)"

awk -v t="$total" -v th="$THRESHOLD" 'BEGIN { exit (t+0 < th+0) ? 1 : 0 }' || {
  echo "FAIL: coverage ${total}% is below the ${THRESHOLD}% threshold." >&2
  exit 1
}

echo "OK: coverage meets the threshold."
