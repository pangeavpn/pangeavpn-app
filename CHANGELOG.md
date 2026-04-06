# Changelog

## 0.2.2 (Unreleased)

### Security

- **Enable Electron sandbox** — renderer preload now runs in sandboxed mode with `contextBridge` only (no Node.js access). IPC channel constants are inlined in the preload to comply with sandbox restrictions.
- **Daemon API rate limiting** — token bucket limiter at 30 requests/minute prevents local denial-of-service.
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

- **Cross-platform VPN client** — Electron UI + Go daemon managing obfuscation + WireGuard tunnels.
- **In-process WireGuard and obfuscation** — no external tunnel binaries required on any platform.
- **Windows service support** — daemon installs as a Windows service via NSIS installer.
- **macOS launchd support** — `.pkg` installer registers a system daemon, no runtime admin prompts.
- **Portable mode** — Windows portable build with bundled daemon and elevation on demand.
- **Kill switch** — Windows firewall rules prevent traffic leaks during tunnel transitions.
- **System tray** — tray icon with connection status indicator.
- **ARM64 builds** — macOS builds for both x64 (Intel) and arm64 (Apple Silicon).
- **obfuscation obfuscation** — traffic disguised as HTTPS to `www.microsoft.com:443` to bypass censorship.
