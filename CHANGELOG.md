# Changelog

## 0.3.0

### Desktop UI

- **Taskbar popover UI** — window transformed into a compact 610×475 popover with auto-positioning, always-on-top, auto-hide on blur, and slide/fade transitions between loading, login, and app screens.
- **Pangea visual theme** — warm terra palette, Sora/Space Grotesk fonts, glass-morphism (backdrop-filter blur) on hero card, panels and dropdowns, and a canvas-based animated background with floating line/dot pieces.
- **Native application menu bar** — About, Edit, and Quit entries.
- **Profile + panel persistence** — selected profile and collapsible panel state saved to localStorage.
- **Throughput panel** and **kill switch pill** added to the main screen.
- **Server picker polish** — server ID label, pill-shaped buttons, opaque dropdown, larger corner radius.
- **Cached servers** — last-fetched server list cached to disk for offline login.
- **Daemon startup countdown** — polling shows remaining time with timeout handling; sync errors auto-retry once before surfacing.
- **Connection errors** now surface daemon detail strings; Disconnect button is shown in the ERROR state since the kill switch may still be active.
- **Login screen** — token input auto-focuses, "Get your token" link added, and a cached-token quick-login button (tap version label 5x to toggle verbose error mode).
- **Light mode fix** — `--bg-base` now applied on `html[data-theme="light"]` so the background renders.
- **Window sizing** — `setWidth`/`setHeight` constants centralized, `setBounds` used for atomic size+position updates, and Linux skips slide animation to avoid WM-induced resize drift.
- **Unified error reporting** — new `reportError` helper in the renderer logs full errors and produces user-friendly messages across all UI actions.

### Linux support

- **Full Linux platform support** — tray icons (including connected state), `--class=PangeaVPN` flag, `/etc/pangeavpn` token candidate path with world-readable ACL, and electron-builder AppImage + deb targets (x64/arm64).
- **Build + install scripts** — `build-bin:linux` for AppImage/deb packaging, `install-linux.sh` for from-source install with systemd service, and README instructions.
- **Policy routing on Linux** — replaces simple `/1` routes with a `wg-quick`-style policy routing table (51820) and fwmark-based `ip rules`, fixing NetworkManager false-positive "no internet" reports caused by `SO_BINDTODEVICE` connectivity probes bypassing the main table. FwMark is injected into the WireGuard config so its own UDP socket bypasses the policy rule.

### Auto-updates

- **electron-updater** — replaces the custom update system with electron-updater backed by GitHub Releases, including 8 new IPC channels, download progress, and restart-to-install UX. On macOS the updater opens the release page in the browser instead of auto-downloading.

### Auth

- **DEVICE_NOT_REGISTERED auto sign-out** — detected in hub API responses, raised as `AuthError`, and wired through `getServers` to trigger automatic sign-out with a toast.
- **Cached token quick-login** — last-used VPN token is cached in localStorage and surfaced as a clickable button on the login screen.
- **Verbose error mode** — developer toggle that surfaces raw error details in place of friendly messages.

### Daemon hardening

- **IPv6 leak protection** — WFP kill switch blocks all IPv6 except loopback; blanket permit-all rules removed from kill switch `Update()` since tunnel traffic already flows through loopback and endpoint permits.
- **Health check** now monitors kill switch active state, and `StatusResponse` surfaces `killSwitchActive` plus WireGuard `bytesIn`/`bytesOut`.
- **Resilient disconnect** — `Disconnect` always transitions to DISCONNECTED even on partial cleanup failures (warnings instead of error returns), with per-step timeouts (4s Cloak, 3s kill switch). All `Connect()` error-path cleanups use 2-second timeout contexts.
- **Cloak Stop()** uses a 3-second timer with forced shutdown fallback to unwedge stuck `RouteUDP` calls (e.g. MakeSession retrying with no internet).
- **Cloak connection count** increased from 2 to 4; debug/trace logs filtered to prevent log store flooding.
- **Config persistence** — backup/rollback added on rename failure.
- **Rate limiter** raised from ~30 to ~500 req/min; `/connect` and `/disconnect` HTTP errors now use 500 status.
- **Token ACL** broadened to cover user-local app support paths (any path containing `pangeavpn-desktop`), fixing a case where elevated user-local installs left the token at 0o600 and unreadable by the renderer.

### Windows / WireGuard adapter stability

- **Deterministic tunnel GUID (v2)** — derived from name only, not name+config, preventing adapter proliferation when the WireGuard config changes between connections.
- **Native stale tunnel cleanup on startup** — enumerates Wintun/WireGuard adapters via `GetIfTable2Ex`, flushes IP/routes on stale LUIDs, closes orphaned Wintun handles, and cleans up network profile registry entries. `ActiveLUIDs()` added to the WG Manager interface for stale detection.
- **Startup reconciliation timeout** increased from 4s to 8s with timing logs.
- **Windows resources** — `pangeavpn.ico`, `resource_windows.syso`, and `versioninfo.json` wired up via `go:generate goversioninfo` (output fixed to `resource_windows.syso`, `*.syso` gitignored).

### Installers

- **macOS DMG sizing** — compute a safe `hdiutil -size` from payload bytes (minimum, padding, alignment, growth factor) to prevent undersized DMG failures; staging directory removed before creation and wrapped in try/catch/finally so partial DMGs and staging dirs are cleaned up on error.
- **macOS installer version** bumped to 0.3.0.

### Dev workflow

- `dev.mjs` kills stale daemon processes on startup and exit (`taskkill`); `killPort8787()` clears the port before elevated daemon restart.
- **Sudo elevation** for the daemon on Linux/macOS in dev mode, waiting for daemon readiness before starting the desktop app, and improved process group cleanup on exit.
- **Runtime file prep** — dev script now creates the token and config files as the current user before elevating, so elevated daemons don't leave root-owned files the renderer can't read.

### Security

- **Enable Electron sandbox** — renderer preload now runs in sandboxed mode with `contextBridge` only (no Node.js access). IPC channel constants are inlined in the preload to comply with sandbox restrictions.
- **Daemon API rate limiting** — token bucket limiter prevents local denial-of-service.
- **Daemon API body size limit** — POST endpoints reject payloads over 1 MB.
- **Sanitize daemon error messages** — API responses no longer leak internal error details; detailed errors are logged server-side only.
- **Restrict token file permissions** — daemon token file set to 0o600 (owner-only) on macOS/Linux, previously 0o644 (world-readable).
- **Upgrade HKDF salt** — secure channel key derivation now uses a proper random salt instead of all zeros.

### Docs

- Updated `docs/CLAUDE.md` with secure channel architecture, Electron security model, and daemon hardening details.
- Updated `docs/architecture.md` with system overview diagram, secure channel flow, and end-to-end connection flow.

## 0.2.1

### Features

- **Secure channel** — all hub API traffic encrypted with per-request ephemeral X25519 ECDH + AES-256-GCM, independent of TLS. Allows the app to work behind MITM WiFi and corporate proxies.
- **Electron net for API** — hub API calls use Electron's `net` module instead of Node `fetch`.

## 0.2.0

### Features

- **Token-based login** — switch from Auth0 sign-in to VPN token login flow.
- **Direct IP option** — connect by IP when DNS is blocked.
- **DoH (DNS-over-HTTPS) fallback** — resolve hub hostname via DoH when standard DNS is censored, with no-SNI mode for additional privacy.
- **Auth invalidation handling** — clear WireGuard keys and notify UI when session expires.
- **Toast notifications** — in-app toast system for user feedback.

## 0.1.x

### Features

- **Cross-platform VPN client** — Electron UI + Go daemon managing Cloak (obfuscation) + WireGuard tunnels.
- **In-process WireGuard and Cloak** — no external tunnel binaries required on any platform.
- **Windows service support** — daemon installs as a Windows service via NSIS installer.
- **macOS launchd support** — `.pkg` installer registers a system daemon, no runtime admin prompts.
- **Portable mode** — Windows portable build with bundled daemon and elevation on demand.
- **Kill switch** — Windows firewall rules prevent traffic leaks during tunnel transitions.
- **System tray** — tray icon with connection status indicator.
- **ARM64 builds** — macOS builds for both x64 (Intel) and arm64 (Apple Silicon).
- **Cloak obfuscation** — traffic disguised as HTTPS to `www.microsoft.com:443` to bypass DPI/censorship.
