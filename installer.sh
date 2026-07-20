#!/usr/bin/env bash
#
# AxonRouter-Go installer
#
# Downloads the latest (or a pinned) release binary from GitHub and installs it
# into ~/.local/bin by default.
#
# On Linux with systemctl available, automatically installs and starts a
# systemd service:
#   - root  → system service (systemctl status axonrouter)
#   - user  → user service (systemctl --user status axonrouter) + linger
#
# Usage:
# curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | bash
# ./installer.sh # latest release, auto OS/arch detection, auto service
# ./installer.sh --version v1.2.3 # specific tag
# ./installer.sh --to /usr/local/bin
# ./installer.sh --no-service # skip systemd service installation
#
# The release workflow (.github/workflows/release.yml) builds and uploads assets
# named "axonrouter-<os>-<arch>[.exe]" for windows/linux and darwin (amd64+arm64).

set -euo pipefail

REPO="rickicode/AxonRouter-Go"
API="https://api.github.com/repos/${REPO}"
VERSION="" # empty => latest
INSTALL_DIR="" # empty => ~/.local/bin
SKIP_SERVICE=false
BIN_NAME="axonrouter"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

err()  { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# ---- parse args -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --to) INSTALL_DIR="$2"; shift 2 ;;
    --no-service) SKIP_SERVICE=true; shift ;;
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
  VERSION="$(set +o pipefail; curl -fsSL "${API}/releases/latest" | grep '"tag_name":' | head -n1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
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
	INSTALL_DIR="$DEFAULT_INSTALL_DIR"
fi

if ! mkdir -p "$INSTALL_DIR" 2>/dev/null; then
	err "could not create ${INSTALL_DIR}. Re-run with sudo or use --to <writable-dir>."
fi

if [[ ! -w "$INSTALL_DIR" ]]; then
	echo "error: ${INSTALL_DIR} is not writable." >&2
	echo >&2
	echo "To install system-wide, re-run with sudo:" >&2
	echo " sudo ./installer.sh" >&2
	echo "Or specify a custom directory:" >&2
	echo " ./installer.sh --to ${INSTALL_DIR}" >&2
	exit 1
fi

# ---- install binary ---------------------------------------------------------
INSTALLED="${INSTALL_DIR}/${TARGET}"
if [[ "$GOOS" == "windows" ]]; then
  cp "$DOWNLOAD_TO" "$INSTALLED"
else
  install -m 0755 "$DOWNLOAD_TO" "$INSTALLED"
fi
info "Installed to ${INSTALLED}"

# ---- install systemd service (Linux only, auto-detect) ----------------------
install_systemd_service() {
  [[ "$GOOS" == "linux" ]] || return 0
  [[ "$SKIP_SERVICE" == false ]] || return 0
  command -v systemctl >/dev/null 2>&1 || return 0

  info "Installing systemd service..."
  if ! "${INSTALLED}" --service install; then
    echo "warning: service installation failed, skipping." >&2
    return 0
  fi

  # Enable linger for normal users so service starts on boot without login
  if [[ "$EUID" -ne 0 ]]; then
    if command -v loginctl >/dev/null 2>&1; then
      info "Enabling linger for user '$(whoami)' (service starts on boot)..."
      loginctl enable-linger "$(whoami)" 2>/dev/null || true
    fi
  fi
}

install_systemd_service

# ---- manage PATH ------------------------------------------------------------
update_shell_path() {
  local dir="$1"
  local rc=""

  case "$(basename "${SHELL:-}")" in
    bash) rc="${HOME}/.bashrc" ;;
    zsh)  rc="${HOME}/.zshrc" ;;
  esac

  if [[ -z "$rc" ]]; then
    echo "note: ${dir} is not on your PATH. Add it manually:"
    echo " export PATH=\"${dir}:\$PATH\""
    return
  fi

  mkdir -p "$(dirname "$rc")"
  if [[ -f "$rc" ]] && grep -qF "export PATH=\"${dir}:\$PATH\"" "$rc" 2>/dev/null; then
    info "PATH entry for ${dir} already exists in ${rc}"
  else
    printf '\n# Added by AxonRouter installer\nexport PATH="%s:$PATH"\n' "$dir" >> "$rc"
    info "Added ${dir} to PATH in ${rc}"
  fi

  echo "To use it in this terminal, run:"
  echo "  source ${rc}"
  echo "Or open a new shell."
}

# ---- summary ----------------------------------------------------------------
echo
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " AxonRouter ${VERSION} installed"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Binary:    ${INSTALLED}"
echo "  OS/Arch:   ${GOOS}/${GOARCH}"
if [[ "$GOOS" == "linux" ]] && command -v systemctl >/dev/null 2>&1 && [[ "$SKIP_SERVICE" == false ]]; then
  if [[ "$EUID" -eq 0 ]]; then
    echo "  Service:   systemctl status axonrouter"
    echo "  Logs:      journalctl -u axonrouter -f"
  else
    echo "  Service:   systemctl --user status axonrouter"
    echo "  Logs:      journalctl --user -u axonrouter -f"
  fi
fi
echo "  Help:      ${TARGET} --help"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if ! command -v "$TARGET" >/dev/null 2>&1 && [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  update_shell_path "$INSTALL_DIR"
fi
