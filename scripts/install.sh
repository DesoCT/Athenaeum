#!/bin/sh
# Installs the latest Athenaeum release binary for this machine.
#
#   curl -fsSL https://raw.githubusercontent.com/DesoCT/Athenaeum/main/scripts/install.sh | sh
#
# It downloads the newest GitHub release (pre-releases included), extracts the
# binary for this OS and architecture, and installs it to ~/.local/bin. No Git,
# no build tools, and no root are required. Override the destination with
# ATHENAEUM_BIN, or pin a version with ATHENAEUM_VERSION=v0.1.0-alpha.7.
set -eu

REPO="DesoCT/Athenaeum"
BINDIR="${ATHENAEUM_BIN:-$HOME/.local/bin}"

# --- platform detection ------------------------------------------------------

os=$(uname -s)
case "$os" in
  Darwin) os=darwin ;;
  Linux)  os=linux ;;
  *) echo "athenaeum: unsupported operating system '$os'" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  arm64 | aarch64) arch=arm64 ;;
  x86_64 | amd64)  arch=amd64 ;;
  *) echo "athenaeum: unsupported architecture '$arch'" >&2; exit 1 ;;
esac

# --- resolve the release -----------------------------------------------------

# The releases are pre-releases, which GitHub's "/releases/latest" endpoint
# skips, so the newest entry from the full list is used instead. ATHENAEUM_VERSION
# pins an exact tag and bypasses the lookup.
if [ -n "${ATHENAEUM_VERSION:-}" ]; then
  tag="$ATHENAEUM_VERSION"
else
  tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=1" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)
fi
if [ -z "$tag" ]; then
  echo "athenaeum: could not determine the latest release" >&2
  exit 1
fi
version=${tag#v}

member="athenaeum-${os}-${arch}"
asset="athenaeum-${version}-${os}-${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$tag/$asset"

# --- download and install ----------------------------------------------------

echo "athenaeum: installing $version ($os/$arch)"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

if ! curl -fsSL "$url" | tar -xzf - -C "$tmp" "$member"; then
  echo "athenaeum: failed to download or extract $url" >&2
  exit 1
fi

mkdir -p "$BINDIR"
mv -f "$tmp/$member" "$BINDIR/athenaeum"
chmod +x "$BINDIR/athenaeum"
# Clear the download quarantine so macOS runs it without a Gatekeeper prompt;
# harmless and a no-op on Linux.
xattr -c "$BINDIR/athenaeum" 2>/dev/null || true

echo "athenaeum: installed to $BINDIR/athenaeum"

# --- PATH hint ---------------------------------------------------------------

case ":$PATH:" in
  *":$BINDIR:"*) ;;
  *) echo "athenaeum: $BINDIR is not on your PATH; add it with" >&2
     echo "    export PATH=\"$BINDIR:\$PATH\"" >&2 ;;
esac

"$BINDIR/athenaeum" version
