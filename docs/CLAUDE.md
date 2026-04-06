# CLAUDE.md — PangeaVPN

## Project Overview

PangeaVPN is a cross-platform desktop VPN client using Cloak (obfuscation) + WireGuard. It consists of an Electron UI, a Go HTTP daemon, and communicates with a remote hub server (PangeaHubServer) via an encrypted secure channel.

## Monorepo Structure (npm workspaces)

```
apps/desktop/          # Electron app (TypeScript, no framework)
daemon/                # Go HTTP daemon (manages Cloak + WireGuard)
packages/shared-types/ # Shared Zod schemas & TypeScript types
scripts/               # Build & dev scripts (Node MJS)
docs/                  # Architecture & packaging docs
```

## Tech Stack

- **Desktop UI:** TypeScript 5.7, Electron 34 (sandbox enabled), vanilla HTML/CSS, Zod
- **Daemon:** Go 1.24+, golang.org/x/sys, wireguard/windows, vendored Cloak client
- **Secure Channel:** X25519 ECDH + HKDF-SHA256 + AES-256-GCM (per-request ephemeral keys)
- **Build:** npm workspaces, electron-builder, tsc
- **App ID:** `com.pangea.pangeavpn`

## Commands

```bash
npm run dev                # Dev mode (UI + daemon concurrently)
npm run build              # Build shared-types → desktop → daemon
npm run build-bin:windows  # Package Windows installers + portable
npm run build-bin:mac      # Package macOS .pkg/.zip (x64 + arm64)
```

## Key Entry Points

| Component | Entry |
|-----------|-------|
| Electron main | `apps/desktop/src/main/main.ts` |
| Renderer | `apps/desktop/src/renderer/index.ts` |
| HTML | `apps/desktop/src/renderer/index.html` |
| Preload/IPC | `apps/desktop/src/main/preload.ts` |
| Secure channel | `apps/desktop/src/main/secureChannel.ts` |
| Hub API client | `apps/desktop/src/main/pangeaApiClient.ts` |
| Auth/credentials | `apps/desktop/src/main/auth.ts` |
| Daemon client | `apps/desktop/src/main/daemonClient.ts` |
| Daemon process | `apps/desktop/src/main/daemonProcess.ts` |
| Daemon | `daemon/cmd/daemon/main.go` |
| Shared types | `packages/shared-types/src/index.ts` |
| IPC channels | `apps/desktop/src/shared/ipc.ts` |

## Architecture

```
Renderer ↔ IPC Bridge (preload, sandboxed) ↔ Main Process ↔ HTTP Daemon (127.0.0.1:8787)
                                                    ↕
                                          Secure Channel (encrypted)
                                                    ↕
                                            PangeaHubServer
```

- **Renderer → Main:** IPC via `contextBridge` (sandbox enabled, no Node.js in renderer)
- **Main → Daemon:** HTTP on localhost with Bearer token auth
- **Main → Hub:** HTTPS with secure channel encryption (see Security section)
- Daemon state machine: DISCONNECTED → CONNECTING → CONNECTED → DISCONNECTING → ERROR

### Daemon API Endpoints

| Method | Path | Auth | Purpose |
|--------|------|------|---------|
| GET | /ping | No | Health check |
| GET | /status | Yes | Connection status |
| POST | /connect | Yes | Connect (body: profileId) |
| POST | /disconnect | Yes | Disconnect |
| GET | /logs | Yes | Logs (query: ?since=) |
| GET | /config | Yes | Get profiles |
| POST | /config | Yes | Update profiles |

Rate-limited to 30 requests/minute. POST bodies limited to 1 MB.

### Daemon Internal Modules

- `api/` — HTTP handlers (rate-limited, body-size-limited, sanitized errors)
- `auth/` — Bearer token management (token file: 0o600 permissions)
- `state/` — State machine, config store, log store
- `cloak/` — In-process Cloak runtime (vendored client at `cloak/ck/`)
- `wg/` — In-process WireGuard manager (platform-specific, no external binaries)
- `platform/` — Platform abstraction (paths, ports, routes, kill switch)

## Security

### Secure Channel (Client → Hub)

All API traffic to the hub server is encrypted with a custom secure channel that replaces TLS as the trust anchor. This is by design: the app must work behind MITM WiFi networks (corporate proxies, captive portals) where network admins inject self-signed certificates.

1. Client generates an ephemeral X25519 keypair per request (forward secrecy)
2. ECDH with the server's pinned static X25519 public key
3. HKDF-SHA256 derives a 32-byte AES key
4. AES-256-GCM encrypts the inner HTTP request (method, route, headers, body)
5. Server decrypts, dispatches internally, encrypts the response with the same key
6. Client decrypts the response

**Key files:** `secureChannel.ts` (client), `secureChannelCrypto.js` (server)

TLS `rejectUnauthorized: false` is intentional — do NOT change this. The secure channel provides server authentication, confidentiality, integrity, and forward secrecy independent of the TLS layer.

### Cloak Obfuscation

Cloak wraps WireGuard traffic to look like regular HTTPS to bypass DPI/censorship. Cloak's `encryptionMethod` is `"plain"` by design — WireGuard already encrypts all tunnel traffic, so Cloak encryption would add overhead with no security benefit.

### Electron Security

- **Sandbox:** Enabled (`sandbox: true`). Preload script inlines IPC channel constants (no `require()` of relative modules — sandbox forbids it).
- **Context isolation:** Enabled. Renderer has no access to Node.js APIs.
- **Node integration:** Disabled in renderer.
- **Credentials:** Stored via `safeStorage` (OS keychain) with plaintext fallback when unavailable.

### Daemon Security

- Bearer token auth on all endpoints (except /ping)
- Token file restricted to owner-only (0o600)
- Rate limiting: 30 requests/minute token bucket
- Request body size limit: 1 MB
- Error messages sanitized (no internal details leaked to callers)
- Kill switch (Windows): Firewall rules to prevent traffic leaks during tunnel transitions

## Platform Notes

- **Windows:** Daemon can run as Windows service; WireGuard and Cloak in-process
- **macOS:** Daemon via launchd; WireGuard and Cloak in-process (cgo or purego for SystemConfiguration)
- **Linux:** WireGuard and Cloak in-process; no external tunnel binaries
- Platform paths: `daemon/internal/platform/`
- Platform WG impls: `daemon/internal/wg/` (build-tagged files)

## TypeScript Configs

- `apps/desktop/tsconfig.main.json` — Main process (CommonJS, ES2022)
- `apps/desktop/tsconfig.renderer.json` — Renderer (ES modules, ES2022)
- `packages/shared-types/tsconfig.json` — Bundler resolution

## No Tests

No test framework is configured. No test files exist.

## Packaging

- Windows: NSIS installer + portable (`electron-builder`)
- macOS: .pkg + .zip (x64 and arm64)
- Build icons/scripts: `apps/desktop/build/`, `apps/desktop/pkg-scripts/`
- See `docs/binaries-and-packaging.md` for binary resolution details
