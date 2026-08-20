#!/bin/bash
# PangeaVPN macOS installer: installs the app and its background service.
# Usage: bash install-mac.sh [/path/to/PangeaVPN.pkg]

set -euo pipefail

# Colour only when writing to a terminal, so piped output stays readable.
# ACCENT is the nearest 256-colour match to the app's terra accent (#c3562b).
if [[ -t 1 ]]; then
    if [[ "$(tput colors 2>/dev/null || echo 0)" -ge 256 ]]; then
        ACCENT='\033[38;5;166m'
    else
        ACCENT='\033[0;33m'
    fi
    GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
else
    ACCENT=''; GREEN=''; YELLOW=''; RED=''; NC=''
fi

SUPPORT_URL="https://pangeavpn.org/contact"
DOWNLOAD_URL="https://pangeavpn.org/download"

log()  { printf "${ACCENT}==> %s${NC}\n" "$1"; }
ok()   { printf "${GREEN}==> %s${NC}\n" "$1"; }
warn() { printf "${YELLOW}Warning: %s${NC}\n" "$1"; }
fail() {
    printf "${RED}Error: %s${NC}\n" "$1" >&2
    printf "Need help? %s\n" "$SUPPORT_URL" >&2
    exit 1
}

# Block art needs a UTF-8 terminal; anything else gets a plain header.
banner() {
    if [[ -n "${PANGEA_BANNER_SHOWN:-}" ]]; then
        return 0
    fi
    export PANGEA_BANNER_SHOWN=1
    if [[ ! -t 1 || "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" != *[Uu][Tt][Ff]* ]]; then
        echo ""
        echo "  PangeaVPN - macOS installer"
        echo ""
        return 0
    fi
    echo ""
    printf "%b" "$ACCENT"
    cat <<'ART'
      ▄███████▄
    ▄████████████████▄
   ███████████████████▄
  █████████████████████▄
  ███████████████████████▄▄▄          P A N G E A   V P N
   ▀████████████████████████
     ▀▀▀▀▀▀███████████████▀           macOS installer
           ▀████████████▀
             ███████████
            ████████████
             █████████▀
              ███████▀
               ████▀
ART
    printf "%b" "$NC"
    echo ""
}

# Mach-O headers are parsed by hand because lipo and file ship with the Xcode
# command line tools, whose installer pops up when they are missing.
macho_hex4() {
    od -An -j "$2" -N 4 -t x1 "$1" 2>/dev/null | tr -d ' \n'
}

macho_cputype_name() {
    case "$1" in
        0100000c) printf "arm64" ;;
        01000007) printf "x86_64" ;;
    esac
}

# Prints the architectures a Mach-O file can run on, space separated.
macho_archs() {
    local file="$1" magic stride count index le archs=""
    magic="$(macho_hex4 "$file" 0)"

    case "$magic" in
        cafebabe|cafebabf)
            stride=20
            if [[ "$magic" == "cafebabf" ]]; then
                stride=32
            fi
            count="$(macho_hex4 "$file" 4)"
            count=$(( 16#${count:-0} ))
            if [[ "$count" -gt 32 ]]; then
                count=32
            fi
            for (( index = 0; index < count; index++ )); do
                archs="$archs $(macho_cputype_name "$(macho_hex4 "$file" $(( 8 + index * stride )))")"
            done
            ;;
        cffaedfe|cefaedfe)
            le="$(macho_hex4 "$file" 4)"
            archs=" $(macho_cputype_name "${le:6:2}${le:4:2}${le:2:2}${le:0:2}")"
            ;;
    esac

    printf "%s" "$archs" | tr -s ' ' | sed -e 's/^ //' -e 's/ $//'
}

APP_PATH="/Applications/PangeaVPN.app"
SUPPORT_DIR="/Library/Application Support/PangeaVPN"
DAEMON_PLIST="/Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist"
DAEMON_LABEL="com.pangea.pangeavpn.daemon"
DAEMON_LOG="/var/log/pangeavpn-daemon.log"
DAEMON_PING_URL="http://127.0.0.1:8787/ping"

banner

# ── Preflight checks ────────────────────────────────────────────────────────

if [[ "$(uname)" != "Darwin" ]]; then
    fail "This installer only supports macOS."
fi

HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
    arm64)
        PKG_ARCH="arm64"
        ;;
    x86_64)
        PKG_ARCH="x64"
        ;;
    *)
        fail "This Mac's processor type ($HOST_ARCH) isn't supported. PangeaVPN runs on Apple Silicon and Intel Macs."
        ;;
esac

log "Detected architecture: $HOST_ARCH ($PKG_ARCH)"

# macOS version check (require at least macOS 12 Monterey)
MACOS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
if [[ "$MACOS_MAJOR" -lt 12 ]]; then
    fail "PangeaVPN requires macOS 12 (Monterey) or later. You are on $(sw_vers -productVersion)."
fi

# Explain the password before asking, so it doesn't look like a phishing prompt.
# stderr stays open so sudo's own prompt is visible if there is no tty.
if ! sudo -n true 2>/dev/null; then
    log "PangeaVPN installs a background service, which needs your Mac login password."
    log "You'll be asked once, now. Typing won't show on screen - that's normal."
fi
if ! sudo -v; then
    fail "Could not get administrator access. If you mistyped your password, run the installer again. Otherwise use an admin account."
fi

WORK_DIR="$(mktemp -d -t pangeavpn-install)"

# Keep sudo alive for the duration of the script
while true; do sudo -n true || true; sleep 50; kill -0 "$$" 2>/dev/null || exit; done 2>/dev/null &
SUDO_KEEPALIVE_PID=$!
trap 'kill "$SUDO_KEEPALIVE_PID" 2>/dev/null || true; rm -rf "$WORK_DIR"' EXIT

# ── Locate the PKG ──────────────────────────────────────────────────────────
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PKG_FILE="${1:-}"

if [[ -z "$PKG_FILE" ]]; then
    # Find any .pkg next to this script (there should be exactly one inside the DMG)
    PKG_FILE="$(find "$SCRIPT_DIR" -maxdepth 1 -name "*.pkg" -print 2>/dev/null | head -1 || true)"

    if [[ -z "$PKG_FILE" ]]; then
        fail "Couldn't find the PangeaVPN installer package. Open the downloaded .dmg first and run this script from inside it, or install online: curl -fsSL https://pangeavpn.org/install-mac.sh | bash"
    fi
fi

if [[ ! -f "$PKG_FILE" ]]; then
    fail "File not found: $PKG_FILE. Check the path you passed to the installer."
fi

# Refuse a package built for the other kind of Mac before touching the system.
# An arm64 build cannot execute on Intel at all.
PKG_BASENAME="$(basename "$PKG_FILE")"
case "$PKG_BASENAME" in
    *arm64*)          FILE_ARCH="arm64" ;;
    *x64*|*x86_64*)   FILE_ARCH="x64"   ;;
    *)                FILE_ARCH=""      ;;
esac
if [[ -n "$FILE_ARCH" && "$FILE_ARCH" != "$PKG_ARCH" ]]; then
    fail "$PKG_BASENAME is for a different kind of Mac (${FILE_ARCH}), but this Mac needs the ${PKG_ARCH} version. Get the right one at ${DOWNLOAD_URL}"
fi

log "Installing PangeaVPN (${PKG_ARCH}) from: $PKG_BASENAME"

# ── Stage the PKG somewhere writable ────────────────────────────────────────
# A DMG mounts read-only, so quarantine can only be stripped from a local copy.
STAGED_PKG="$WORK_DIR/$PKG_BASENAME"
log "Preparing the installer..."
cp "$PKG_FILE" "$STAGED_PKG" || fail "Could not copy $PKG_BASENAME into $WORK_DIR. Check that you have enough free disk space."
xattr -dr com.apple.quarantine "$STAGED_PKG" 2>/dev/null || true

# ── Quit a running app so the installer can replace it ──────────────────────
if pgrep -f "$APP_PATH/Contents/MacOS/" >/dev/null 2>&1; then
    log "Closing the running PangeaVPN app..."
    osascript -e 'tell application "PangeaVPN" to quit' >/dev/null 2>&1 || true
    sleep 2
    pkill -f "$APP_PATH/Contents/MacOS/" 2>/dev/null || true
fi

# ── Stop any running service before the package overwrites binaries ─────────
# Overwriting under KeepAlive=true makes launchd restart a half-written binary.
log "Stopping the previous version, if any..."
sudo launchctl bootout "system/$DAEMON_LABEL" 2>/dev/null || true
sleep 1

# ── Run the PKG installer ───────────────────────────────────────────────────
log "Installing PangeaVPN (this can take a minute)..."
if ! sudo installer -pkg "$STAGED_PKG" -target / ; then
    fail "Installation failed. The installer file may be damaged - download it again from ${DOWNLOAD_URL}"
fi

if [[ ! -d "$APP_PATH" ]]; then
    fail "The installer finished but PangeaVPN is not in your Applications folder. The download may be incomplete - get it again from ${DOWNLOAD_URL}"
fi

# ── Clear quarantine from the app bundle ────────────────────────────────────
# Only the quarantine flag is removed; clearing every xattr can void a signature.
log "Letting macOS know the app is safe to open..."
sudo xattr -dr com.apple.quarantine "$APP_PATH" 2>/dev/null || true

# macOS kills apps whose signature seal is broken. Only repair a bad signature
# — re-signing a Developer ID build would void its notarization.
if ! codesign --verify --deep --strict "$APP_PATH" 2>/dev/null; then
    log "Repairing the app so macOS will allow it to open..."
    sudo codesign --force --deep --sign - "$APP_PATH" >/dev/null 2>&1 || true
    if ! codesign --verify --deep --strict "$APP_PATH" 2>/dev/null; then
        warn "PangeaVPN may refuse to open. If it does, reinstall it; if that fails too, contact ${SUPPORT_URL}"
    fi
fi

# ── Install the background service ──────────────────────────────────────────
# Transports run in-process inside the service, so nothing else needs staging.
log "Installing the PangeaVPN background service..."
DAEMON_SRC="$APP_PATH/Contents/Resources/daemon/daemon"

if [[ ! -f "$DAEMON_SRC" ]]; then
    fail "The installer is missing a required file. Please download it again from ${DOWNLOAD_URL}"
fi

# Authoritative arch check. The filename guard above is only a hint, so undo
# the part-finished install rather than leaving a Mac that cannot run it.
DAEMON_ARCHS="$(macho_archs "$DAEMON_SRC" || true)"
if [[ -n "$DAEMON_ARCHS" && " $DAEMON_ARCHS " != *" $HOST_ARCH "* ]]; then
    sudo rm -rf "$APP_PATH" "$SUPPORT_DIR/PangeaDaemon" 2>/dev/null || true
    fail "This installer is for a different kind of Mac (${DAEMON_ARCHS// /, }), but this Mac needs ${PKG_ARCH}. Nothing was left installed. Get the right version at ${DOWNLOAD_URL}"
fi

sudo mkdir -p "$SUPPORT_DIR"

sudo install -m 755 -o root -g wheel "$DAEMON_SRC" "$SUPPORT_DIR/PangeaDaemon"

sudo chown root:wheel "$SUPPORT_DIR"
sudo chmod 755 "$SUPPORT_DIR"

sudo xattr -dr com.apple.quarantine "$SUPPORT_DIR" 2>/dev/null || true

# Copying invalidates the ad-hoc signature macOS requires to run the binary.
if ! sudo codesign --force --sign - "$SUPPORT_DIR/PangeaDaemon"; then
    fail "Could not prepare the background service. Please try installing again, or contact ${SUPPORT_URL}"
fi

# ── Create shared auth token ────────────────────────────────────────────────
# The root service would otherwise create it root-only and lock the app out.
REAL_USER="${SUDO_USER:-}"
if [[ -z "$REAL_USER" || "$REAL_USER" == "root" ]]; then
    REAL_USER="$(id -un)"
fi
if [[ "$REAL_USER" == "root" ]]; then
    warn "No login user detected. Run this installer from your normal admin account rather than as root, or the app will not be able to reach its background service."
fi

TOKEN_FILE="$SUPPORT_DIR/daemon-token.txt"
log "Securing the link between the app and its service..."
openssl rand -hex 32 | sudo tee "$TOKEN_FILE" > /dev/null
sudo chown "$REAL_USER" "$TOKEN_FILE"
sudo chmod 600 "$TOKEN_FILE"

# ── Register the service with macOS ─────────────────────────────────────────
log "Setting the service to start automatically..."
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
    <string>${DAEMON_LOG}</string>
    <key>StandardErrorPath</key>
    <string>${DAEMON_LOG}</string>
  </dict>
</plist>
PLIST

sudo chown root:wheel "$DAEMON_PLIST"
sudo chmod 644 "$DAEMON_PLIST"

# ── Start the service ───────────────────────────────────────────────────────
# enable must precede bootstrap: a service left disabled refuses to bootstrap.
log "Starting the PangeaVPN service..."
sudo launchctl enable "system/$DAEMON_LABEL" >/dev/null 2>&1 || true
LAUNCHCTL_ERR="$(sudo launchctl bootstrap system "$DAEMON_PLIST" 2>&1)" || true
sudo launchctl kickstart -k "system/$DAEMON_LABEL" >/dev/null 2>&1 || true

# ── Verify ──────────────────────────────────────────────────────────────────
log "Waiting for the service to start (up to 20 seconds)..."
DAEMON_OK=false
DOTTED=false
for _ in $(seq 1 40); do
    if curl -sf --max-time 2 "$DAEMON_PING_URL" >/dev/null 2>&1; then
        DAEMON_OK=true
        break
    fi
    if [[ -t 1 ]]; then
        printf "."
        DOTTED=true
    fi
    sleep 0.5
done
if $DOTTED; then
    printf "\n"
fi

INSTALLED_VERSION="$(/usr/libexec/PlistBuddy -c "Print :CFBundleShortVersionString" "$APP_PATH/Contents/Info.plist" 2>/dev/null || true)"
VERSION_LABEL=""
if [[ -n "$INSTALLED_VERSION" ]]; then
    VERSION_LABEL=" v${INSTALLED_VERSION}"
fi

echo ""
if $DAEMON_OK; then
    ok "PangeaVPN${VERSION_LABEL} is installed and its background service is running."
    echo ""
    echo "  Open PangeaVPN from your Applications folder, or press Cmd+Space and type PangeaVPN."
    echo "  The first time you connect, macOS may ask you to allow it - choose Allow."
    echo ""
else
    warn "PangeaVPN${VERSION_LABEL} is installed, but its background service has not reported in yet."
    warn "This is usually harmless - macOS sometimes shows a network permission popup that delays it."
    echo ""
    echo "  What to do now:"
    echo "    1. Open PangeaVPN from your Applications folder - it often finishes starting on its own."
    echo "    2. If it says it cannot connect, restart your Mac and open it again."
    echo "    3. Still stuck? Go to ${SUPPORT_URL} and include the details below."
    echo ""
    echo "  Technical details for support:"
    if [[ -n "$LAUNCHCTL_ERR" ]]; then
        echo "    launchctl: $LAUNCHCTL_ERR"
    fi
    if sudo test -s "$DAEMON_LOG"; then
        sudo tail -n 15 "$DAEMON_LOG" 2>/dev/null | sed 's/^/    /' || true
    fi
    echo ""
fi
