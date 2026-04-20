#!/usr/bin/env bash
# Keel installer — downloads a prebuilt binary from GitHub Releases.
#
#   curl -fsSL https://raw.githubusercontent.com/smaan712gb/keel/main/install.sh | bash
#
# Env vars:
#   KEEL_VERSION      version tag to install (default: latest release)
#   KEEL_INSTALL_DIR  where to drop the binary (default: $HOME/.local/bin)
#
# Windows users: use WSL / Git Bash, or `go install github.com/smaan712gb/keel/cmd/keel@latest`.

set -euo pipefail

REPO="smaan712gb/keel"
INSTALL_DIR="${KEEL_INSTALL_DIR:-$HOME/.local/bin}"
VERSION="${KEEL_VERSION:-}"

say()  { printf '\033[1;36m>>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31mxx\033[0m %s\n' "$*" >&2; exit 1; }

need() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }
need curl
need tar
need uname
need mktemp

# --- Detect platform ---------------------------------------------------------

OS="$(uname -s)"
case "$OS" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*) die "Windows shell detected. Use 'go install github.com/smaan712gb/keel/cmd/keel@latest' or WSL." ;;
  *) die "unsupported OS: $OS" ;;
esac

ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) die "unsupported architecture: $ARCH" ;;
esac

# --- Resolve version ---------------------------------------------------------

if [ -z "$VERSION" ]; then
  say "Resolving latest release..."
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -E '"tag_name"' | head -n 1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || die "could not resolve latest version (is the GitHub API reachable?)"
fi
# Strip leading v for archive name, keep as-is for the tag in the URL.
vnum="${VERSION#v}"

ARCHIVE="keel_${vnum}_${os}_${arch}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases/download/${VERSION}"
ARCHIVE_URL="${BASE_URL}/${ARCHIVE}"
CHECKSUMS_URL="${BASE_URL}/checksums.txt"

say "Installing keel ${VERSION} (${os}/${arch})"

# --- Download + verify -------------------------------------------------------

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

say "Downloading ${ARCHIVE}..."
curl -fsSL -o "${tmp}/${ARCHIVE}" "${ARCHIVE_URL}" \
  || die "download failed: ${ARCHIVE_URL}"

say "Verifying checksum..."
curl -fsSL -o "${tmp}/checksums.txt" "${CHECKSUMS_URL}" \
  || die "could not fetch checksums.txt"

expected="$(grep "${ARCHIVE}\$" "${tmp}/checksums.txt" | awk '{print $1}')"
[ -n "$expected" ] || die "checksum for ${ARCHIVE} missing from checksums.txt"

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "${tmp}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  actual="$(shasum -a 256 "${tmp}/${ARCHIVE}" | awk '{print $1}')"
else
  die "need sha256sum or shasum to verify the download"
fi

[ "$expected" = "$actual" ] || die "checksum mismatch: expected $expected got $actual"

# --- Extract + install -------------------------------------------------------

tar -xzf "${tmp}/${ARCHIVE}" -C "$tmp"
[ -f "${tmp}/keel" ] || die "archive did not contain a 'keel' binary"

mkdir -p "$INSTALL_DIR"
install -m 0755 "${tmp}/keel" "${INSTALL_DIR}/keel"
say "Installed: ${INSTALL_DIR}/keel"

# --- Post-install ------------------------------------------------------------

case ":$PATH:" in
  *":${INSTALL_DIR}:"*) ;;
  *) warn "${INSTALL_DIR} is not on your PATH. Add this to your shell rc:"
     warn "    export PATH=\"${INSTALL_DIR}:\$PATH\"" ;;
esac

say "Run 'keel init' to set up state and print MCP registration snippets."
