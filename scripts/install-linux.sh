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

# The account PangeaVPN is being installed for. The privileged steps below all
# call sudo themselves, so this works whether the script is run directly or
# under sudo.
RUNTIME_USER="${SUDO_USER:-$(id -un)}"
if [ "$RUNTIME_USER" = "root" ]; then
  RUNTIME_USER=""
fi

# Builds must never run as root: npm would leave root-owned node_modules/,
# dist/ and .cache/ trees behind in the checkout, and every later build the
# user runs without sudo then dies on EACCES. Drop back to them for build
# steps; only the install steps that follow need privileges.
if [ "$(id -u)" -eq 0 ] && [ -n "$RUNTIME_USER" ]; then
  # -H so npm's cache and config resolve under the user's home, not /root's.
  run_as_user() { sudo -u "$RUNTIME_USER" -H --preserve-env=PATH -- "$@"; }
else
  run_as_user() { "$@"; }
fi

# Whether the *invoking session* already carries the group, recorded before
# anything below can change it. A session keeps the group list it was created
# with, so being in /etc/group is not the same as being able to use it yet.
# Running under sudo this reads root's groups, so it is only trusted directly.
SESSION_HAS_GROUP=0
if [ "$(id -u)" -ne 0 ] && id -nG 2>/dev/null | tr ' ' '\n' | grep -qx pangeavpn; then
  SESSION_HAS_GROUP=1
fi

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

# Earlier versions of this script ran the build as root under sudo, leaving
# root-owned node_modules/, dist/, .cache/ and .git/index trees behind. The
# build below now correctly runs as the user, so those leftovers would make it
# fail on EACCES. Hand the checkout back before building.
if [ -n "$RUNTIME_USER" ] && [ -n "$(find "$REPO_ROOT" -user root -print -quit 2>/dev/null)" ]; then
  info "Repairing root-owned files left in the checkout by an earlier install..."
  sudo chown -R "$RUNTIME_USER:" "$REPO_ROOT"
fi

info "Installing npm dependencies..."
run_as_user npm install

info "Building project..."
run_as_user npm run build

# --- Install app ---
info "Installing PangeaVPN to $INSTALL_DIR..."
sudo mkdir -p "$INSTALL_DIR"

# Build the AppImage
info "Packaging AppImage..."
run_as_user npm exec --workspace @pangeavpn/desktop electron-builder -- \
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

# Tracks whether this run is what granted the group. If they already had it,
# their session already carries it and no re-login is needed.
GROUP_ADDED_NOW=0
if [ -n "$RUNTIME_USER" ]; then
  if id -nG "$RUNTIME_USER" 2>/dev/null | tr ' ' '\n' | grep -qx pangeavpn; then
    info "$RUNTIME_USER is already in the pangeavpn group."
  else
    info "Adding $RUNTIME_USER to the pangeavpn group..."
    sudo usermod -aG pangeavpn "$RUNTIME_USER"
    GROUP_ADDED_NOW=1
  fi
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

# --- Install systemd services ---
info "Setting up systemd services..."
# A lock the last session left must be back before any interface is configured;
# the daemon itself starts too late for that, so a oneshot re-applies it first.
sudo tee "$BOOT_LOCK_SERVICE_FILE" > /dev/null <<EOF
[Unit]
Description=PangeaVPN kill switch (boot re-arm)
DefaultDependencies=no
After=local-fs.target
Wants=network-pre.target
Before=network-pre.target shutdown.target
Conflicts=shutdown.target

[Service]
Type=oneshot
ExecStart=$DAEMON_BIN --arm-boot-lock
Environment=HOME=/root
Environment=PANGEA_APP_SUPPORT_DIR=/etc/pangeavpn

[Install]
WantedBy=multi-user.target
EOF

sudo tee "$SERVICE_FILE" > /dev/null <<EOF
[Unit]
Description=PangeaVPN Daemon
After=network-online.target pangea-killswitch-boot.service
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
sudo systemctl enable pangea-killswitch-boot
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
if [ "$GROUP_ADDED_NOW" -eq 1 ] || { [ "$(id -u)" -ne 0 ] && [ "$SESSION_HAS_GROUP" -eq 0 ]; }; then
  echo
  warn "=============================================================="
  warn " ONE MORE STEP: log out and back in (or reboot) before"
  warn " launching PangeaVPN."
  warn ""
  warn " $RUNTIME_USER is in the 'pangeavpn' group, but this login"
  warn " session still carries the group list it was created with,"
  warn " so the app cannot read the daemon's token yet."
  warn "=============================================================="
fi
