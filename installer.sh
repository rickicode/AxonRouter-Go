#!/usr/bin/env bash
#
# AxonRouter-Go installer
#
# Downloads the latest (or a pinned) release binary from GitHub and installs it
# into a directory on your PATH.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/main/installer.sh | bash
#   ./installer.sh                 # latest release, auto OS/arch detection
#   ./installer.sh --version v1.2.3 # specific tag
#   ./installer.sh --to /usr/local/bin
#
# The release workflow (.github/workflows/release.yml) builds and uploads assets
# named "axonrouter-<os>-<arch>[.exe]" for windows/linux and darwin (amd64+arm64).

set -euo pipefail

REPO="rickicode/AxonRouter-Go"
API="https://api.github.com/repos/${REPO}"
VERSION=""            # empty => latest
INSTALL_DIR=""       # empty => pick a writable dir on PATH
BIN_NAME="axonrouter"

err()  { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# ---- parse args -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --to)      INSTALL_DIR="$2"; shift 2 ;;
    -h|--help)
      grep '^#' "$0" | sed 's/^#\{1,2\} //'; exit 0 ;;
    *) err "unknown argument: $1" ;;
  esac
done

# ---- detect OS --------------------------------------------------------------
OS="$(uname -s)"
case "$OS" in
  Linux)  GOOS="linux" ;;
  Darwin) GOOS="darwin" ;;
  MINGW*|MSYS*|CYGWIN*|Windows_NT) GOOS="windows" ;;
  *) err "unsupported OS: $OS" ;;
esac

# ---- detect architecture ----------------------------------------------------
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) GOARCH="amd64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  armv7l|armv6l) GOARCH="arm" ;;
  i386|i686) GOARCH="386" ;;
  *) err "unsupported architecture: $ARCH" ;;
esac

EXT=""
if [[ "$GOOS" == "windows" ]]; then EXT=".exe"; fi
ASSET="axonrouter-${GOOS}-${GOARCH}${EXT}"
TARGET="${BIN_NAME}${EXT}"

# ---- resolve release --------------------------------------------------------
if [[ -z "$VERSION" ]]; then
  info "Resolving latest release for ${GOOS}/${GOARCH}..."
  VERSION="$(curl -fsSL "${API}/releases/latest" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  [[ -n "$VERSION" ]] || err "could not determine latest release tag"
else
  info "Using pinned version ${VERSION}"
fi
info "Target asset: ${ASSET} (${VERSION})"

URL="https://github.com/${REPO}/releases/download/${VERSION}/${ASSET}"
DOWNLOAD_TO="$(mktemp -d)/${ASSET}"

info "Downloading ${URL}"
if ! curl -fsSL "$URL" -o "$DOWNLOAD_TO"; then
  err "download failed. The release ${VERSION} may not include an asset for ${GOOS}/${GOARCH}."
fi

# ---- choose install directory ----------------------------------------------
if [[ -z "$INSTALL_DIR" ]]; then
  for d in "${HOME}/.local/bin" /usr/local/bin; do
    if [[ -d "$d" ]] && [[ -w "$d" ]]; then INSTALL_DIR="$d"; break; fi
  done
  if [[ -z "$INSTALL_DIR" ]]; then
    INSTALL_DIR="${HOME}/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi
fi
mkdir -p "$INSTALL_DIR"

# ---- install ----------------------------------------------------------------
INSTALLED="${INSTALL_DIR}/${TARGET}"
if [[ "$GOOS" == "windows" ]]; then
  cp "$DOWNLOAD_TO" "$INSTALLED"
else
  install -m 0755 "$DOWNLOAD_TO" "$INSTALLED"
fi
info "Installed to ${INSTALLED}"

# ---- verify on PATH ---------------------------------------------------------
if command -v "$TARGET" >/dev/null 2>&1 || [[ ":$PATH:" == *":${INSTALL_DIR}:"* ]]; then
  info "Done. Run it with: ${TARGET}"
else
  echo "note: ${INSTALL_DIR} is not on your PATH. Add it with:"
  echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
fi
