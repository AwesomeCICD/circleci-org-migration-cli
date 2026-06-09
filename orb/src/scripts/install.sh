#!/bin/sh
# Install circleci-migrate from GitHub Releases.
# Parameters are passed as environment variables by the orb command:
#   ORB_PARAM_VERSION      — release tag or "latest"
#   ORB_PARAM_INSTALL_DIR  — destination directory (e.g. /usr/local/bin)
#   ORB_PARAM_FORCE_INSTALL — "true" to reinstall even if already present

set -e

repo="CircleCI-Public/circleci-org-migration-cli"
ver="${ORB_PARAM_VERSION:-latest}"
install_dir="${ORB_PARAM_INSTALL_DIR:-/usr/local/bin}"
force="${ORB_PARAM_FORCE_INSTALL:-false}"

# Skip if already installed (and force is not set), so a cache-restore hit
# does not need to run the download at all.
if [ "$force" != "true" ] && command -v circleci-migrate > /dev/null 2>&1; then
  echo "circleci-migrate already installed: $(circleci-migrate version)"
  exit 0
fi

# Detect OS and architecture.
os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64)          arch="amd64" ;;
  aarch64|arm64)   arch="arm64" ;;
  *)
    echo "ERROR: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

# Resolve "latest" to a concrete tag.
if [ "$ver" = "latest" ]; then
  ver=$(curl -sfL "https://api.github.com/repos/${repo}/releases/latest" \
    | grep -o '"tag_name": *"[^"]*"' | head -1 \
    | sed 's/.*"\(v[^"]*\)".*/\1/')
  if [ -z "$ver" ]; then
    echo "ERROR: could not resolve latest release tag from GitHub API" >&2
    exit 1
  fi
fi

v="${ver#v}"
url="https://github.com/${repo}/releases/download/${ver}/circleci-migrate_${v}_${os}_${arch}.tar.gz"

echo "Downloading ${url}"
tmp=$(mktemp -d)
curl -sfL "$url" | tar -xz -C "$tmp"
bin=$(find "$tmp" -type f -name circleci-migrate | head -1)
sudo install -m 0755 "$bin" "${install_dir}/circleci-migrate"
rm -rf "$tmp"

circleci-migrate version
