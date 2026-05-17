#!/bin/bash
# PangeaVPN macOS one-shot online installer.
#
# Downloads the latest release DMG for this Mac's architecture, strips the
# quarantine flag, mounts it, runs the bundled installer (which installs the
# .pkg, sets up the LaunchDaemon, and ad-hoc signs the daemon), then cleans
# up.  Users can run this with a single command:
#
#   curl -fsSL https://pangeavpn.org/install-mac.sh | bash
#
# No prior downloads or App Store required.

set -euo pipefail

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log()  { printf "${GREEN}==> %s${NC}\n" "$1"; }
warn() { printf "${YELLOW}Warning: %s${NC}\n" "$1"; }
fail() { printf "${RED}Error: %s${NC}\n" "$1" >&2; exit 1; }

HUB_LATEST_URL="${PANGEA_HUB_URL:-https://api.pangeavpn.org/api/desktop/latest}"
GITHUB_LATEST_URL="https://api.github.com/repos/pangeavpn/pangeavpn-app/releases/latest"

[[ "$(uname)" == "Darwin" ]] || fail "This installer only supports macOS."

case "$(uname -m)" in
    arm64)  ARCH_TAG="arm64" ;;
    x86_64) ARCH_TAG="x64"   ;;
    *)      fail "Unsupported architecture: $(uname -m). Need Apple Silicon (arm64) or Intel (x86_64)." ;;
esac

MACOS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
if [[ "$MACOS_MAJOR" -lt 12 ]]; then
    fail "PangeaVPN requires macOS 12 (Monterey) or later. You are on $(sw_vers -productVersion)."
fi

log "Detected macOS $(sw_vers -productVersion) on $ARCH_TAG"

# ── Resolve the DMG URL for this arch ───────────────────────────────────────
# Prefer the hub (cached, censorship-resistant via api.pangeavpn.org).
# Fall back to GitHub directly if the hub is unreachable.
log "Looking up the latest release..."

RELEASE_JSON=""
if RELEASE_JSON="$(curl -fsSL --max-time 8 "$HUB_LATEST_URL" 2>/dev/null)" && [[ -n "$RELEASE_JSON" ]]; then
    : # got it from the hub
elif RELEASE_JSON="$(curl -fsSL --max-time 12 "$GITHUB_LATEST_URL" 2>/dev/null)"; then
    : # GitHub fallback
else
    fail "Could not reach the release index. Check your internet connection."
fi

# Pull out the matching arch DMG URL.  We accept both response shapes:
#   - hub:    {"assets":[{"url":"https://...arm64-installer.dmg", ...}]}
#   - github: {"assets":[{"browser_download_url":"https://...arm64-installer.dmg", ...}]}
# Avoid jq dependency — grep+sed is good enough for these well-known fields.
DMG_URL="$(
    printf "%s" "$RELEASE_JSON" \
        | tr ',' '\n' \
        | grep -Eo '"(url|browser_download_url)":"https://[^"]+'"$ARCH_TAG"'-installer\.dmg"' \
        | head -1 \
        | sed -E 's/.*"(https:[^"]+)".*/\1/'
)"

if [[ -z "$DMG_URL" ]]; then
    fail "No ${ARCH_TAG}-installer.dmg in the latest release. The release may still be uploading — try again in a few minutes."
fi

VERSION="$(printf "%s" "$RELEASE_JSON" | grep -Eo '"(version|tag_name)":"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"$/\1/' | sed 's/^v//')"
log "Latest version: ${VERSION:-unknown}"
log "Downloading: $(basename "$DMG_URL")"

# ── Download to a temp dir we own ───────────────────────────────────────────
TMPDIR_PANGEA="$(mktemp -d -t pangeavpn-install)"
trap 'rm -rf "$TMPDIR_PANGEA"' EXIT
DMG_PATH="$TMPDIR_PANGEA/PangeaVPN.dmg"

if ! curl -fL --progress-bar -o "$DMG_PATH" "$DMG_URL"; then
    fail "Download failed. Check your internet connection and try again."
fi

# Strip the quarantine xattr BEFORE mounting so Gatekeeper doesn't block the
# bundled install script.
log "Stripping quarantine attribute..."
xattr -dr com.apple.quarantine "$DMG_PATH" 2>/dev/null || true

# ── Mount the DMG read-only ─────────────────────────────────────────────────
log "Mounting installer image..."
MOUNT_OUTPUT="$(hdiutil attach "$DMG_PATH" -nobrowse -readonly -noverify -plist 2>/dev/null)" || \
    fail "Failed to mount $DMG_PATH"
MOUNT_POINT="$(printf "%s" "$MOUNT_OUTPUT" \
    | grep -E '<string>/Volumes/' \
    | head -1 \
    | sed -E 's@.*<string>(/Volumes/[^<]+)</string>.*@\1@')"
if [[ -z "$MOUNT_POINT" || ! -d "$MOUNT_POINT" ]]; then
    fail "Could not determine mount point for the DMG."
fi
trap 'hdiutil detach "$MOUNT_POINT" -quiet 2>/dev/null || true; rm -rf "$TMPDIR_PANGEA"' EXIT

# ── Quit any running app so installer can replace it ────────────────────────
if pgrep -f "/Applications/PangeaVPN.app/Contents/MacOS" >/dev/null 2>&1; then
    log "Quitting running PangeaVPN.app..."
    osascript -e 'tell application "PangeaVPN" to quit' 2>/dev/null || true
    sleep 1
fi

# ── Delegate to the install-mac.sh inside the DMG ───────────────────────────
# That script is the source of truth for installing the .pkg, setting up the
# LaunchDaemon, generating the daemon token, and ad-hoc signing the daemon
# binary on Apple Silicon. It's published with each release and matches the
# .pkg version-for-version.
BUNDLED_INSTALLER="$MOUNT_POINT/install-mac.sh"
PKG_FILE="$(find "$MOUNT_POINT" -maxdepth 1 -name "*.pkg" -print 2>/dev/null | head -1)"

if [[ -f "$BUNDLED_INSTALLER" ]]; then
    log "Running bundled installer..."
    bash "$BUNDLED_INSTALLER" "$PKG_FILE"
elif [[ -n "$PKG_FILE" ]]; then
    warn "Bundled install-mac.sh missing — running the .pkg directly. Daemon may need a manual signing pass."
    sudo installer -pkg "$PKG_FILE" -target /
    sudo xattr -dr com.apple.quarantine /Applications/PangeaVPN.app 2>/dev/null || true
    sudo codesign --force --deep --sign - /Applications/PangeaVPN.app 2>/dev/null || \
        warn "codesign on /Applications/PangeaVPN.app failed. The app may still launch but Apple Silicon may complain."
else
    fail "DMG mounted but contained no .pkg. Release artifact looks broken."
fi

log "Done. PangeaVPN is installed in /Applications."
