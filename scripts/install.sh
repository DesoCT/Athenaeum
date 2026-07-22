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

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

# The releases are pre-releases, which GitHub's "/releases/latest" endpoint
# skips. The full list is not reliably ordered newest-first either, so the tag
# with the most recent published_at is chosen rather than the first entry.
# ATHENAEUM_VERSION pins an exact tag and bypasses the lookup.
if [ -n "${ATHENAEUM_VERSION:-}" ]; then
  tag="$ATHENAEUM_VERSION"
else
  if ! curl -fsSL "https://api.github.com/repos/$REPO/releases?per_page=100" -o "$tmp/releases.json"; then
    echo "athenaeum: could not reach the GitHub releases API" >&2
    exit 1
  fi
  # Each published release contributes one tag_name and one published_at, in
  # that order, so pairing them line-by-line and sorting by the (ISO 8601) date
  # puts the newest release last.
  grep -oE '"tag_name": *"[^"]*"' "$tmp/releases.json" | sed -E 's/.*: *"([^"]*)"/\1/' > "$tmp/tags"
  grep -oE '"published_at": *"[^"]*"' "$tmp/releases.json" | sed -E 's/.*: *"([^"]*)"/\1/' > "$tmp/dates"
  tag=$(paste "$tmp/dates" "$tmp/tags" | sort | tail -n1 | cut -f2)
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
