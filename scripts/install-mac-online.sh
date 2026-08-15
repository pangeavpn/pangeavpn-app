#!/bin/bash
# PangeaVPN macOS one-shot online installer, run as:
#   curl -fsSL https://pangeavpn.org/install-mac.sh | bash

set -euo pipefail

# Colour only when writing to a terminal, so piped output stays readable.
if [[ -t 1 ]]; then
    GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
else
    GREEN=''; YELLOW=''; RED=''; NC=''
fi

SUPPORT_URL="https://pangeavpn.org/contact"
DOWNLOAD_URL="https://pangeavpn.org/download"

log()  { printf "${GREEN}==> %s${NC}\n" "$1"; }
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
    printf "%b" "$GREEN"
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

HUB_LATEST_URL="${PANGEA_HUB_URL:-https://api.pangeavpn.org/api/desktop/latest}"
GITHUB_LATEST_URL="https://api.github.com/repos/pangeavpn/pangeavpn-app/releases/latest"

SUDO_KEEPALIVE_PID=""
TMPDIR_PANGEA=""
MOUNT_POINT=""

cleanup() {
    if [[ -n "$SUDO_KEEPALIVE_PID" ]]; then
        kill "$SUDO_KEEPALIVE_PID" 2>/dev/null || true
    fi
    if [[ -n "$MOUNT_POINT" ]]; then
        hdiutil detach "$MOUNT_POINT" -quiet 2>/dev/null \
            || hdiutil detach "$MOUNT_POINT" -force -quiet 2>/dev/null || true
    fi
    if [[ -n "$TMPDIR_PANGEA" ]]; then
        rm -rf "$TMPDIR_PANGEA"
    fi
    return 0
}

# Everything lives in main() so a download truncated mid-transfer cannot run
# a partial script — bash only executes it once the final line has arrived.
main() {
    trap cleanup EXIT

    banner

    [[ "$(uname)" == "Darwin" ]] || fail "This installer only supports macOS."

    case "$(uname -m)" in
        arm64)  ARCH_TAG="arm64" ;;
        x86_64) ARCH_TAG="x64"   ;;
        *)      fail "This Mac's processor type ($(uname -m)) isn't supported. PangeaVPN runs on Apple Silicon and Intel Macs." ;;
    esac

    MACOS_MAJOR="$(sw_vers -productVersion | cut -d. -f1)"
    if [[ "$MACOS_MAJOR" -lt 12 ]]; then
        fail "PangeaVPN requires macOS 12 (Monterey) or later. You are on $(sw_vers -productVersion)."
    fi

    log "Detected macOS $(sw_vers -productVersion) on $ARCH_TAG"

    # ── Ask for admin rights up front ───────────────────────────────────────
    # Prompting before the download avoids stalling on a password mid-install.
    if ! sudo -n true 2>/dev/null; then
        log "PangeaVPN installs a background service, which needs your Mac login password."
        log "You'll be asked once, now. Typing won't show on screen — that's normal."
    fi
    if ! sudo -v; then
        fail "Could not get administrator access. If you mistyped your password, run the installer again. Otherwise use an admin account."
    fi

    # Refresh the 5-minute sudo timestamp so a slow download cannot expire it.
    while true; do sudo -n true || true; sleep 50; kill -0 "$$" 2>/dev/null || exit; done 2>/dev/null &
    SUDO_KEEPALIVE_PID=$!

    # ── Resolve the DMG URL for this arch ───────────────────────────────────
    # Prefer the hub (censorship-resistant); fall back to GitHub if unreachable.
    log "Looking up the latest release..."

    RELEASE_JSON=""
    if RELEASE_JSON="$(curl -fsSL --max-time 8 "$HUB_LATEST_URL" 2>/dev/null)" && [[ -n "$RELEASE_JSON" ]]; then
        : # got it from the hub
    elif RELEASE_JSON="$(curl -fsSL --max-time 12 "$GITHUB_LATEST_URL" 2>/dev/null)"; then
        : # GitHub fallback
    else
        fail "Could not reach the download server. If the rest of your internet works, your network may be blocking it — try a different network, or get the installer from ${DOWNLOAD_URL}"
    fi

    # Accepts the hub's "url" and GitHub's "browser_download_url" asset shapes.
    # GitHub pretty-prints its JSON, so a space may follow the colon.
    DMG_URL="$(
        printf "%s" "$RELEASE_JSON" \
            | tr ',' '\n' \
            | grep -Eo '"(url|browser_download_url)":[[:space:]]*"https://[^"]+'"$ARCH_TAG"'-installer\.dmg"' \
            | head -1 \
            | sed -E 's/.*"(https:[^"]+)".*/\1/' \
            || true
    )"

    if [[ -z "$DMG_URL" ]]; then
        fail "The latest release has no download for this Mac yet. It may still be uploading — try again in a few minutes, or see ${DOWNLOAD_URL}"
    fi

    VERSION="$(printf "%s" "$RELEASE_JSON" | grep -Eo '"(version|tag_name)":[[:space:]]*"[^"]+"' | head -1 | sed -E 's/.*"([^"]+)"$/\1/' | sed 's/^v//' || true)"
    if [[ -n "$VERSION" ]]; then
        log "Latest version: $VERSION"
    fi
    log "Downloading: $(basename "$DMG_URL")"

    # ── Download to a temp dir we own ───────────────────────────────────────
    TMPDIR_PANGEA="$(mktemp -d -t pangeavpn-install)"
    DMG_PATH="$TMPDIR_PANGEA/PangeaVPN.dmg"

    if ! curl -fL --progress-bar -o "$DMG_PATH" "$DMG_URL"; then
        fail "Download failed. Check your connection and try again, or get the installer from ${DOWNLOAD_URL}"
    fi

    # ── Verify the download ─────────────────────────────────────────────────
    # Catches a truncated or altered file before it becomes a confusing failure.
    DMG_NAME="$(basename "$DMG_URL")"
    SUMS_URL="$(dirname "$DMG_URL")/SHA256SUMS.txt"
    EXPECTED_SHA="$(curl -fsSL --max-time 15 "$SUMS_URL" 2>/dev/null | awk -v f="$DMG_NAME" '$2 == f { print $1; exit }')" || EXPECTED_SHA=""

    if [[ -n "$EXPECTED_SHA" ]]; then
        ACTUAL_SHA="$(shasum -a 256 "$DMG_PATH" | awk '{ print $1 }')"
        if [[ "$ACTUAL_SHA" != "$EXPECTED_SHA" ]]; then
            fail "The download failed its integrity check — the file is damaged or was altered in transit. Do not install it. Try again; if this keeps happening, your network may be interfering."
        fi
        log "Download verified."
    else
        warn "Couldn't reach the file used to verify this download. Continuing — the download itself completed normally."
    fi

    log "Preparing the download..."
    xattr -dr com.apple.quarantine "$DMG_PATH" 2>/dev/null || true

    # ── Open the disk image ─────────────────────────────────────────────────
    log "Opening the installer..."
    MOUNT_OUTPUT="$(hdiutil attach "$DMG_PATH" -nobrowse -readonly -noverify -plist 2>/dev/null)" \
        || fail "Could not open the downloaded installer. It may be damaged — try running this again."
    MOUNT_POINT="$(printf "%s" "$MOUNT_OUTPUT" \
        | grep -E '<string>/Volumes/' \
        | head -1 \
        | sed -E 's@.*<string>(/Volumes/[^<]+)</string>.*@\1@' \
        || true)"
    if [[ -z "$MOUNT_POINT" || ! -d "$MOUNT_POINT" ]]; then
        fail "Could not open the downloaded installer. It may be damaged — try running this again."
    fi

    # ── Delegate to the install-mac.sh inside the DMG ───────────────────────
    # It ships with each release, so it is the source of truth for the setup.
    BUNDLED_INSTALLER="$MOUNT_POINT/install-mac.sh"
    PKG_FILE="$(find "$MOUNT_POINT" -maxdepth 1 -name "*.pkg" -print 2>/dev/null | head -1 || true)"

    if [[ ! -f "$BUNDLED_INSTALLER" || -z "$PKG_FILE" ]]; then
        fail "This download is incomplete or damaged. Please try again, or report it at ${SUPPORT_URL}"
    fi

    bash "$BUNDLED_INSTALLER" "$PKG_FILE"
}

main "$@"
