#!/usr/bin/env bash
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

info()  { printf "${GREEN}[+]${NC} %s\n" "$*"; }
warn()  { printf "${YELLOW}[!]${NC} %s\n" "$*"; }
fail()  { printf "${RED}[x]${NC} %s\n" "$*"; exit 1; }

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
INSTALL_DIR="/opt/PangeaVPN"
DAEMON_BIN="/usr/local/bin/pangea-daemon"
DESKTOP_FILE="/usr/share/applications/pangeavpn.desktop"
ICON_DIR="/usr/share/icons/hicolor/256x256/apps"
SERVICE_FILE="/etc/systemd/system/pangea-daemon.service"

# --- Detect package manager ---
if command -v apt-get &>/dev/null; then
  PM=apt
elif command -v dnf &>/dev/null; then
  PM=dnf
elif command -v pacman &>/dev/null; then
  PM=pacman
else
  PM=unknown
fi

install_pkg() {
  case "$PM" in
    apt)    sudo apt-get install -y "$@" ;;
    dnf)    sudo dnf install -y "$@" ;;
    pacman) sudo pacman -S --needed --noconfirm "$@" ;;
    *)      fail "Unknown package manager. Install manually: $*" ;;
  esac
}

# --- Check Node.js ---
info "Checking Node.js..."
if command -v node &>/dev/null; then
  info "Found Node.js $(node -v)"
else
  fail "Node.js not found. Install Node.js 18+ from https://nodejs.org or your package manager."
fi

if ! command -v npm &>/dev/null; then
  fail "npm not found. It should come with Node.js — check your installation."
fi

# --- Check Go ---
info "Checking Go..."
if command -v go &>/dev/null; then
  info "Found $(go version | awk '{print $3}')"
elif [ -x /usr/local/go/bin/go ]; then
  export PATH="$PATH:/usr/local/go/bin"
  info "Found Go at /usr/local/go/bin/go"
else
  fail "Go not found. Install Go 1.22+ from https://go.dev/dl/"
fi

# --- Install system dependencies ---
info "Installing system dependencies..."
case "$PM" in
  apt)    install_pkg iproute2 wireguard-tools libfuse2 ;;
  dnf)    install_pkg iproute wireguard-tools fuse-libs ;;
  pacman) install_pkg iproute2 wireguard-tools fuse2 ;;
  *)      warn "Unknown package manager — make sure iproute2, wireguard-tools, and libfuse2 are installed." ;;
esac

# --- Build ---
cd "$REPO_ROOT"

info "Installing npm dependencies..."
npm install

info "Building project..."
npm run build

# --- Install app ---
info "Installing PangeaVPN to $INSTALL_DIR..."
sudo mkdir -p "$INSTALL_DIR"

# Build the AppImage
info "Packaging AppImage..."
npm exec --workspace @pangeavpn/desktop electron-builder -- \
  --projectDir . --linux AppImage --"$(uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')" \
  --publish never --config.electronVersion=41.5.0

APPIMAGE=$(find "$REPO_ROOT/dist/installers" -name '*.AppImage' -printf '%T@ %p\n' | sort -rn | head -1 | cut -d' ' -f2-)
if [ -z "$APPIMAGE" ]; then
  fail "AppImage not found after build."
fi

# --- Quit a running app so we can safely replace the binary ---
# If PangeaVPN is open (e.g. actively connected), the AppImage stays open for
# execution and `cp` onto it fails with "Text file busy". Close it first.
APP_MATCH="$INSTALL_DIR/PangeaVPN.AppImage|mount_.*/pangeavpn"
if pgrep -f "$APP_MATCH" >/dev/null 2>&1; then
  warn "PangeaVPN is currently running — closing it to install the update. If you're connected, the VPN will disconnect."
  pkill -TERM -f "$APP_MATCH" 2>/dev/null || true
  sleep 2
  pkill -KILL -f "$APP_MATCH" 2>/dev/null || true
fi

# Install atomically: write alongside the target, then rename into place.
# A plain `cp` onto the live path can still race with a process that reopens
# it right after the pkill above; rename(2) succeeds even while the old
# inode is busy/executing, so this can't hit "Text file busy".
APPIMAGE_TMP="$INSTALL_DIR/.PangeaVPN.AppImage.new"
sudo install -m 755 "$APPIMAGE" "$APPIMAGE_TMP"
sudo mv -f "$APPIMAGE_TMP" "$INSTALL_DIR/PangeaVPN.AppImage"

# --- Install daemon ---
info "Installing daemon..."

# Stop existing service before replacing the binary
if systemctl is-active --quiet pangea-daemon 2>/dev/null; then
  info "Stopping existing daemon..."
  sudo systemctl stop pangea-daemon
fi

# The root daemon writes its token file group-readable by "pangeavpn" so the
# desktop app (running as the ordinary user) can read it without world access.
if ! getent group pangeavpn &>/dev/null; then
  info "Creating pangeavpn group..."
  sudo groupadd --system pangeavpn
fi

RUNTIME_USER="${SUDO_USER:-}"
if [ -n "$RUNTIME_USER" ] && [ "$RUNTIME_USER" != "root" ]; then
  info "Adding $RUNTIME_USER to the pangeavpn group..."
  sudo usermod -aG pangeavpn "$RUNTIME_USER"
else
  warn "Could not detect the installing user — add them to the pangeavpn group manually, e.g.: sudo usermod -aG pangeavpn <your-username>, then log out and back in."
fi

DAEMON_SRC="$REPO_ROOT/daemon/bin/daemon"
if [ ! -f "$DAEMON_SRC" ]; then
  fail "Daemon binary not found at $DAEMON_SRC — build may have failed."
fi
sudo mkdir -p /etc/pangeavpn
DAEMON_BIN_TMP="$(dirname "$DAEMON_BIN")/.$(basename "$DAEMON_BIN").new"
sudo install -m 755 "$DAEMON_SRC" "$DAEMON_BIN_TMP"
sudo mv -f "$DAEMON_BIN_TMP" "$DAEMON_BIN"

# --- Install systemd service ---
info "Setting up systemd service..."
sudo tee "$SERVICE_FILE" > /dev/null <<EOF
[Unit]
Description=PangeaVPN Daemon
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=$DAEMON_BIN
Environment=HOME=/root
Environment=PANGEA_APP_SUPPORT_DIR=/etc/pangeavpn
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable pangea-daemon
sudo systemctl restart pangea-daemon
info "Daemon service installed and started."

# --- Install icon + desktop entry ---
info "Installing desktop entry..."
ICON_SRC="$REPO_ROOT/apps/desktop/build/PangeaVPN_linux.png"
sudo mkdir -p "$ICON_DIR"
if [ -f "$ICON_SRC" ]; then
  sudo cp "$ICON_SRC" "$ICON_DIR/pangeavpn.png"
fi

sudo tee "$DESKTOP_FILE" > /dev/null <<EOF
[Desktop Entry]
Name=PangeaVPN
Comment=Secure VPN client
Exec=$INSTALL_DIR/PangeaVPN.AppImage --no-sandbox
Icon=pangeavpn
Type=Application
Categories=Network;
StartupWMClass=PangeaVPN
EOF

sudo chmod 644 "$DESKTOP_FILE"

info "PangeaVPN installed successfully!"
info "Launch from your application menu or run: $INSTALL_DIR/PangeaVPN.AppImage"
if [ -n "$RUNTIME_USER" ] && [ "$RUNTIME_USER" != "root" ]; then
  warn "Log out and back in (or reboot) so your pangeavpn group membership takes effect — required before the app can reach the daemon."
fi
