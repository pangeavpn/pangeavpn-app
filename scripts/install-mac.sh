#!/bin/bash
# PangeaVPN macOS Installer
#
# This script removes macOS quarantine flags, installs PangeaVPN, and
# ensures the background daemon starts correctly.
#
# Usage:
#   bash install-mac.sh              (auto-finds PangeaVPN*.pkg next to this script)
#   bash install-mac.sh /path/to/PangeaVPN.pkg

set -euo pipefail

# ── Configuration ────────────────────────────────────────────────────────────
VERSION="0.3.0"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log()  { printf "${GREEN}==> %s${NC}\n" "$1"; }
warn() { printf "${YELLOW}Warning: %s${NC}\n" "$1"; }
fail() { printf "${RED}Error: %s${NC}\n" "$1" >&2; exit 1; }

APP_PATH="/Applications/PangeaVPN.app"
SUPPORT_DIR="/Library/Application Support/PangeaVPN"
DAEMON_PLIST="/Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist"
DAEMON_LABEL="com.pangea.pangeavpn.daemon"

# ── Preflight checks ────────────────────────────────────────────────────────

# Must be running macOS
if [[ "$(uname)" != "Darwin" ]]; then
    fail "This installer only supports macOS."
fi

# Detect architecture
ARCH="$(uname -m)"
case "$ARCH" in
    arm64)
        PKG_ARCH="arm64"
        ;;
    x86_64)
        PKG_ARCH="x64"
        ;;
    *)
        fail "Unsupported architecture: $ARCH. PangeaVPN supports Apple Silicon (arm64) and Intel (x86_64)."
        ;;
esac

log "Detected architecture: $ARCH ($PKG_ARCH)"

# macOS version check (require at least macOS 12 Monterey)
MACOS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
if [[ "$MACOS_MAJOR" -lt 12 ]]; then
    fail "PangeaVPN requires macOS 12 (Monterey) or later. You are on $(sw_vers -productVersion)."
fi

# Verify sudo access early so we don't fail halfway through
log "Checking for administrator privileges..."
if ! sudo -v 2>/dev/null; then
    fail "This installer requires administrator privileges. Please run from an admin account."
fi

# Keep sudo alive for the duration of the script
while true; do sudo -n true; sleep 50; kill -0 "$$" || exit; done 2>/dev/null &
SUDO_KEEPALIVE_PID=$!
trap 'kill $SUDO_KEEPALIVE_PID 2>/dev/null || true' EXIT

# ── Locate the PKG ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_FILE="${1:-}"

if [[ -z "$PKG_FILE" ]]; then
    # Find any .pkg next to this script (there should be exactly one inside the DMG)
    PKG_FILE="$(find "$SCRIPT_DIR" -maxdepth 1 -name "*.pkg" -print 2>/dev/null | head -1)"

    if [[ -z "$PKG_FILE" ]]; then
        fail "No .pkg found next to this script. Place install-mac.sh alongside the PangeaVPN .pkg file."
    fi
fi

if [[ ! -f "$PKG_FILE" ]]; then
    fail "File not found: $PKG_FILE"
fi

log "Installing PangeaVPN v${VERSION} (${PKG_ARCH}) from: $(basename "$PKG_FILE")"

# ── Strip quarantine from the PKG itself ─────────────────────────────────────
log "Removing quarantine from installer package..."
xattr -dr com.apple.quarantine "$PKG_FILE" 2>/dev/null || true
xattr -cr "$PKG_FILE" 2>/dev/null || true

# ── Run the PKG installer ───────────────────────────────────────────────────
log "Running installer (you may be prompted for your password)..."
if ! sudo installer -pkg "$PKG_FILE" -target / ; then
    fail "Package installation failed. The .pkg file may be corrupted. Try re-downloading it."
fi

# ── Strip quarantine from app bundle ─────────────────────────────────────────
log "Clearing quarantine from installed files..."
if [[ -d "$APP_PATH" ]]; then
    sudo xattr -dr com.apple.quarantine "$APP_PATH"  2>/dev/null || true
    sudo xattr -cr "$APP_PATH" 2>/dev/null || true
fi

# ── Stop any running daemon before overwriting binaries ──────────────────────
# The postinstall script inside the .pkg may have signed these binaries already.
# If we overwrite them while launchd has KeepAlive=true, launchd will try to
# restart the daemon with the new unsigned binary and macOS will kill it.
log "Stopping any existing daemon..."
sudo launchctl bootout "system/$DAEMON_LABEL" 2>/dev/null || true
sleep 1

# ── Copy daemon binary to system path ───────────────────────────────────────
# WireGuard and Cloak run in-process inside the daemon, so no helper binaries
# need to be staged.
log "Setting up system daemon..."
DAEMON_SRC="$APP_PATH/Contents/Resources/daemon/daemon"

if [[ ! -f "$DAEMON_SRC" ]]; then
    fail "Daemon binary not found at $DAEMON_SRC. The .pkg may be incomplete."
fi

sudo mkdir -p "$SUPPORT_DIR"

sudo install -m 755 -o root -g wheel "$DAEMON_SRC" "$SUPPORT_DIR/PangeaDaemon"

sudo chown root:wheel "$SUPPORT_DIR"
sudo chmod 755 "$SUPPORT_DIR"

# ── Strip quarantine from copied binaries ────────────────────────────────────
sudo xattr -dr com.apple.quarantine "$SUPPORT_DIR" 2>/dev/null || true

# ── Create shared auth token ────────────────────────────────────────────────
# Both the daemon (root) and the Electron app (user) read this token file.
# We chown it to the logged-in user so that even if the daemon tightens
# permissions to 0600, the user (as owner) can still read it.
REAL_USER="${SUDO_USER:-$USER}"
TOKEN_FILE="$SUPPORT_DIR/daemon-token.txt"
log "Generating daemon auth token..."
openssl rand -hex 32 | sudo tee "$TOKEN_FILE" > /dev/null
sudo chown "$REAL_USER" "$TOKEN_FILE"
sudo chmod 600 "$TOKEN_FILE"

# ── Create LaunchDaemon plist ────────────────────────────────────────────────
echo ""
log "If you saw warnings above from the package installer, don't worry — we're fixing that now."
echo ""
log "Creating LaunchDaemon plist..."
sudo tee "$DAEMON_PLIST" > /dev/null <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
  <dict>
    <key>Label</key>
    <string>${DAEMON_LABEL}</string>
    <key>ProgramArguments</key>
    <array>
      <string>${SUPPORT_DIR}/PangeaDaemon</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
      <key>PANGEA_APP_SUPPORT_DIR</key>
      <string>${SUPPORT_DIR}</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/var/log/pangeavpn-daemon.log</string>
    <key>StandardErrorPath</key>
    <string>/var/log/pangeavpn-daemon.log</string>
  </dict>
</plist>
PLIST

sudo chown root:wheel "$DAEMON_PLIST"
sudo chmod 644 "$DAEMON_PLIST"

# ── Re-sign daemon for Apple Silicon ────────────────────────────────────────
# Copying invalidates ad-hoc signatures and Apple Silicon requires all
# executables to carry a valid code signature.
log "Signing daemon for Apple Silicon compatibility..."
sudo codesign --force --sign - "$SUPPORT_DIR/PangeaDaemon"

# ── Start the LaunchDaemon ───────────────────────────────────────────────────
log "Starting PangeaVPN daemon..."
# bootout was already done before copying binaries — no need to repeat.
# Load the plist fresh, enable, and start.
sudo launchctl bootstrap system "$DAEMON_PLIST"     2>/dev/null || true
sudo launchctl enable   "system/$DAEMON_LABEL"      2>/dev/null || true
sudo launchctl kickstart -k "system/$DAEMON_LABEL"  2>/dev/null || true

# ── Verify ───────────────────────────────────────────────────────────────────
log "Verifying daemon..."
DAEMON_OK=false
for i in $(seq 1 8); do
    if curl -sf --max-time 2 http://127.0.0.1:8787/ping >/dev/null 2>&1; then
        DAEMON_OK=true
        break
    fi
    sleep 0.5
done

echo ""
if $DAEMON_OK; then
    log "PangeaVPN v${VERSION} installed successfully and the daemon is running."
else
    warn "Daemon did not respond yet. This can happen if macOS showed a firewall prompt in the background."
    warn "Check for any system popups, then open PangeaVPN. The daemon will start automatically."
fi

echo ""
echo "       Open PangeaVPN from your Applications folder or Launchpad."
echo ""