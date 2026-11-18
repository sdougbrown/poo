#!/usr/bin/env bash
# install.sh — bootstrapping installer for poo
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/sdougbrown/poo/main/install.sh | sh
#
# POSIX sh. Downloads the latest release binary for your platform from GitHub
# Releases and installs it to ~/.local/bin (or a directory of your choice via
# BINDIR).

set -eu

OWNER="sdougbrown"
REPO="poo"
BINDIR="${BINDIR:-${HOME}/.local/bin}"

# --- helpers --------------------------------------------------------------

err()   { printf 'install: %s\n' "$*" >&2; exit 1; }
info()  { printf '  %s\n' "$*"; }

# --- detect platform ------------------------------------------------------

OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
  Darwin) OS="darwin"  ;;
  Linux)  OS="linux"   ;;
  *) err "unsupported OS: $OS" ;;
esac

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) err "unsupported architecture: $ARCH" ;;
esac

info "platform: ${OS}/${ARCH}"

# --- resolve latest release tag -------------------------------------------

printf '  resolving latest release...\n'
TAG="$(curl -fsSL "https://api.github.com/repos/${OWNER}/${REPO}/releases/latest" \
  | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')"

if [ -z "$TAG" ]; then
  err "could not determine latest release tag"
fi

info "latest release: ${TAG}"

# --- download archive -----------------------------------------------------

# goreleaser archive naming: poo_linux_amd64.tar.gz, poo_darwin_arm64.tar.gz
ASSET="poo_${OS}_${ARCH}.tar.gz"
BASE="https://github.com/${OWNER}/${REPO}/releases/download/${TAG}"

TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

printf '  downloading %s...\n' "$ASSET"
curl -fsSL -o "${TMPDIR}/${ASSET}" "${BASE}/${ASSET}" \
  || err "download failed: ${BASE}/${ASSET}"

# --- verify checksum ------------------------------------------------------

printf '  verifying checksum...\n'
curl -fsSL -o "${TMPDIR}/checksums.txt" "${BASE}/checksums.txt" \
  || err "could not download ${BASE}/checksums.txt"

EXPECT="$(grep " ${ASSET}\$" "${TMPDIR}/checksums.txt" | awk '{print $1}')"
[ -n "$EXPECT" ] || err "checksums.txt has no entry for ${ASSET}"

case "$OS" in
  darwin) SHASUM="shasum -a 256" ;;
  linux)  SHASUM="sha256sum"   ;;
esac
ACTUAL="$($SHASUM "${TMPDIR}/${ASSET}" | awk '{print $1}')"
[ "$ACTUAL" = "$EXPECT" ] \
  || err "checksum mismatch for ${ASSET}: expected ${EXPECT}, got ${ACTUAL}"

# --- extract & install ----------------------------------------------------

printf '  extracting...\n'
tar -xzf "${TMPDIR}/${ASSET}" -C "$TMPDIR"

mkdir -p "$BINDIR"
install -m 0755 "${TMPDIR}/poo" "${BINDIR}/poo"

info "installed poo ${TAG} → ${BINDIR}/poo"

# --- path hint ------------------------------------------------------------

case ":${PATH}:" in
  *":${BINDIR}:"*)
    info "ready: poo --help"
    ;;
  *)
    printf '\n'
    printf '  Add %s to your PATH to use poo:\n' "$BINDIR"
    printf '    export PATH="%s:$PATH"\n' "$BINDIR"
    printf '\n'
    ;;
esac
