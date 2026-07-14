#!/usr/bin/env bash
#
# AxonRouter-Go installer
#
# Downloads the latest (or a pinned) release binary from GitHub and installs it
# into a directory on your PATH.
#
# Usage:
# curl -fsSL https://raw.githubusercontent.com/rickicode/AxonRouter-Go/master/installer.sh | bash
# ./installer.sh # latest release, auto OS/arch detection
# ./installer.sh --version v1.2.3 # specific tag
# ./installer.sh --to /usr/local/bin
# ./installer.sh --service # install a systemd service (Linux only)
#
# The release workflow (.github/workflows/release.yml) builds and uploads assets
# named "axonrouter-<os>-<arch>[.exe]" for windows/linux and darwin (amd64+arm64).

set -euo pipefail

REPO="rickicode/AxonRouter-Go"
API="https://api.github.com/repos/${REPO}"
VERSION="" # empty => latest
INSTALL_DIR="" # empty => pick a writable dir on PATH
INSTALL_SERVICE=false
BIN_NAME="axonrouter"

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

# ---- service mode defaults --------------------------------------------------
if [[ "$INSTALL_SERVICE" == true ]]; then
  [[ "$GOOS" == "linux" ]] || err "--service is only supported on Linux"
  [[ "$EUID" -eq 0 ]] || err "--service must be run as root (e.g. sudo bash installer.sh --service)"
  command -v systemctl >/dev/null 2>&1 || err "systemctl not found; cannot install systemd service"
  INSTALL_DIR="/opt/axonrouter"
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
install_systemd() {
  local svc_user="axonrouter"
  local data_dir="/var/lib/axonrouter"

  if ! id -u "$svc_user" >/dev/null 2>&1; then
    useradd --system --home "$data_dir" --create-home "$svc_user" || err "failed to create ${svc_user} user"
  fi

  mkdir -p "$data_dir"
  chown -R "${svc_user}:${svc_user}" "$data_dir"

  cat > /etc/systemd/system/axonrouter.service <<EOF
[Unit]
Description=AxonRouter-Go API Proxy
After=network.target

[Service]
Type=simple
User=${svc_user}
  WorkingDirectory=${data_dir}
  ExecStart=${INSTALLED}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now axonrouter
  info "Created /etc/systemd/system/axonrouter.service"
  info "Service is running. Check status with: systemctl status axonrouter"
}

# ---- install systemd service on Linux ---------------------------------------
if [[ "$INSTALL_SERVICE" == true ]]; then
  install_systemd
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

if command -v "$TARGET" >/dev/null 2>&1 || [[ ":$PATH:" == *":${INSTALL_DIR}:"* ]]; then
  info "Done. Run it with: ${TARGET}"
else
  update_shell_path "$INSTALL_DIR"
fi
