#!/usr/bin/env bash
#
# AxonRouter-Go installer
#
# Downloads the latest (or a pinned) release binary from GitHub and installs it
# into ~/axonrouter/bin by default.
#
# Usage:
# curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | bash
# ./installer.sh # latest release, auto OS/arch detection
# ./installer.sh --version v1.2.3 # specific tag
# ./installer.sh --to ~/axonrouter/bin
# ./installer.sh --service # install a systemd service (Linux only)
#
# The release workflow (.github/workflows/release.yml) builds and uploads assets
# named "axonrouter-<os>-<arch>[.exe]" for windows/linux and darwin (amd64+arm64).

set -euo pipefail

REPO="rickicode/AxonRouter-Go"
API="https://api.github.com/repos/${REPO}"
VERSION="" # empty => latest
INSTALL_DIR="" # empty => ~/.local/bin
INSTALL_SERVICE=false
BIN_NAME="axonrouter"
DEFAULT_INSTALL_DIR="${HOME}/.local/bin"

err()  { echo "error: $*" >&2; exit 1; }
info() { echo "==> $*"; }

# ---- parse args -------------------------------------------------------------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --version) VERSION="$2"; shift 2 ;;
    --to) INSTALL_DIR="$2"; shift 2 ;;
    --service) INSTALL_SERVICE=true; shift ;;
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

# ---- service mode guard (fail fast before any download) --------------------------------------------------
if [[ "$INSTALL_SERVICE" == true ]]; then
  [[ "$GOOS" == "linux" ]] || err "--service is only supported on Linux"
  if [[ "$EUID" -ne 0 ]]; then
    echo "error: --service must be run as root." >&2
    echo >&2
    echo "Re-run the installer through sudo, for example:" >&2
    echo " curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | sudo bash -s -- --service" >&2
    echo "or, if you already downloaded this script:" >&2
    echo " sudo ./installer.sh --service" >&2
    exit 1
  fi
  command -v systemctl >/dev/null 2>&1 || err "systemctl not found; cannot install systemd service"
	INSTALL_DIR="${DEFAULT_INSTALL_DIR}"
fi

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

# ---- install ----------------------------------------------------------------
INSTALLED="${INSTALL_DIR}/${TARGET}"
if [[ "$GOOS" == "windows" ]]; then
  cp "$DOWNLOAD_TO" "$INSTALLED"
else
  install -m 0755 "$DOWNLOAD_TO" "$INSTALLED"
fi
info "Installed to ${INSTALLED}"

# ---- install systemd service on Linux using the binary's --startup install-root
if [[ "$INSTALL_SERVICE" == true ]]; then
  info "Installing systemd service via ${INSTALLED} --startup install-root"
  if ! "${INSTALLED}" --startup install-root; then
    err "service installation failed"
  fi

  echo
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo " AxonRouter ${VERSION} service installed"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo " Binary:    ${INSTALLED}"
  echo " Status:    systemctl status axonrouter"
  echo " Uninstall: ${INSTALLED} --startup uninstall"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  exit 0
fi

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

echo
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo " AxonRouter ${VERSION} installed"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "  Binary:    ${INSTALLED}"
echo "  OS/Arch:   ${GOOS}/${GOARCH}"
echo "  Run:       ${TARGET}"
echo "  Help:      ${TARGET} --help"
echo " Service: ${TARGET} --startup install"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"

if ! command -v "$TARGET" >/dev/null 2>&1 && [[ ":$PATH:" != *":${INSTALL_DIR}:"* ]]; then
  update_shell_path "$INSTALL_DIR"
fi
