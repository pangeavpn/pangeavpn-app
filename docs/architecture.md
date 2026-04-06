# Architecture

`pangeavpn-desktop` is split into three parts:

- `apps/desktop`: Electron UI (renderer -> preload -> main process)
- `daemon`: Go HTTP daemon at `127.0.0.1:8787`
- `packages/shared-types`: Shared API/config schemas and TypeScript types

It communicates with the remote PangeaHubServer for authentication, server discovery, and peer provisioning.

## System overview

```
+-------------------+     IPC (sandboxed)     +-------------------+
|    Renderer       | <---------------------> |   Main Process    |
|  (HTML/CSS/JS)    |     contextBridge       |   (Electron)      |
+-------------------+                         +--------+----------+
                                                       |
                                              HTTP Bearer token
                                                       |
                                              +--------v----------+
                                              |   Go Daemon       |
                                              |  127.0.0.1:8787   |
                                              +--------+----------+
                                                       |
                                              +--------v----------+
                                              |  Cloak (obfusc.)  |
                                              |  -> WireGuard      |
                                              |  -> VPN Node       |
                                              +-------------------+

+-------------------+  Secure Channel (X25519 + AES-256-GCM)
|   Main Process    | <-------------------------------------> PangeaHubServer
|   (Electron)      |     (over HTTPS, TLS verification off
+-------------------+      for captive portal tolerance)
```

## Dev flow

- Root `npm run dev` runs:
  - `go run ./cmd/daemon` in `daemon/`
  - Electron app in `apps/desktop/`

On Windows dev, daemon launch remains UAC-elevated from the dev script so wireguard-go adapter setup can succeed.

## Windows production flow

On installed Windows builds:

1. NSIS installs `PangeaDaemon` as a Windows service (LocalSystem).
2. Service starts automatically at boot.
3. Electron does not launch daemon directly in packaged mode.
4. If daemon is down, app attempts `sc start PangeaDaemon`; if unavailable, it shows repair guidance.

On Windows portable builds:

1. App runs in packaged portable mode (`PORTABLE_EXECUTABLE_DIR` present).
2. App does not require preinstalled `PangeaDaemon` service.
3. App launches bundled `PangeaDaemon.exe` with elevation when daemon is not reachable.

On installed macOS `.pkg` builds:

1. Installer copies daemon to `/Library/Application Support/PangeaVPN/PangeaDaemon`.
2. Installer registers `com.pangea.pangeavpn.daemon` as a system `launchd` service.
3. Electron app talks to the already-installed daemon without runtime elevation prompts.

## Runtime flow

1. Renderer calls `window.daemonApi.*` or `window.pangeaApi.*`.
2. Preload (sandboxed) forwards IPC requests to main process via `ipcRenderer.invoke()`.
3. For daemon operations: main process calls daemon HTTP endpoints with Bearer token.
4. For hub operations: main process encrypts request via secure channel and sends to PangeaHubServer.
5. Daemon executes state-machine operations and returns status/log/config payloads.

Windows service-installed daemon uses machine-scoped runtime files:

- `%ProgramData%\PangeaVPN\daemon-token.txt` (0o600)
- `%ProgramData%\PangeaVPN\config.json`

Non-Windows runtime uses user-scoped files under the app config directory (for example on macOS: `~/Library/Application Support/pangeavpn-desktop/`), except installed macOS `.pkg` builds which use `/Library/Application Support/PangeaVPN/`.

## Secure channel

The Electron main process communicates with PangeaHubServer through a custom encrypted channel rather than relying on TLS for server authentication. This is necessary because:

- The app must work behind MITM WiFi (corporate proxies, captive portals)
- These networks inject self-signed certificates, breaking standard TLS verification
- The secure channel provides its own server authentication via a pinned X25519 public key

**Per-request flow:**

1. Generate ephemeral X25519 keypair (forward secrecy)
2. ECDH shared secret with server's pinned static public key
3. HKDF-SHA256 derives 32-byte AES key (salt + info string for domain separation)
4. AES-256-GCM encrypts inner HTTP request `{method, route, headers, body}`
5. POST encrypted envelope to `/v1/secure` on the hub
6. Server decrypts, dispatches to internal route, encrypts response
7. Client decrypts response using the same AES key

**Key files:**
- Client: `apps/desktop/src/main/secureChannel.ts`
- Server: `hub/src/lib/secureChannelCrypto.js`, `hub/src/routes/secureChannel.js`

The server enforces a route allowlist on the secure channel — only client-facing routes (`/api/client/bootstrap`, `/api/client/regions`, `/api/client/token-login`, `/api/register`) are reachable. Admin routes require separate HMAC authentication.

## Connection flow (end-to-end)

1. User logs in with VPN token → main process sends to hub via secure channel
2. Hub authenticates, returns session + server list
3. User selects a server → main process calls hub's `provisionAndConnect`
4. Hub provisions a WireGuard peer on the target VPN node (JWT-authenticated push)
5. Hub returns WireGuard config + Cloak credentials to client
6. Main process builds a daemon profile (WG config + Cloak config) and POSTs to daemon
7. Daemon starts Cloak (in-process, obfuscates traffic as HTTPS to `www.microsoft.com:443`)
8. Daemon starts WireGuard (in-process, connects through Cloak's local UDP listener)
9. Kill switch activates (Windows: firewall rules to prevent leaks)
10. Status transitions: DISCONNECTED → CONNECTING → CONNECTED

## WireGuard backend

All platforms use in-process wireguard-go (imported as a Go library). No external `wireguard-go` or `wg` binaries are spawned.

macOS backend creates a TUN device in-process and configures addresses via ioctl, routes via PF_ROUTE socket, and DNS via SystemConfiguration (cgo or purego, depending on build).
Linux backend creates a TUN device in-process and configures addresses/routes via netlink and DNS via systemd-resolved D-Bus or `/etc/resolv.conf` fallback.
Windows backend uses the WireGuard Windows driver libraries in-process and requires elevation.
macOS/Linux backend requires the daemon process to run as root.
In packaged macOS builds, launching from Finder keeps Electron non-root.
When Cloak mode is active with loopback WireGuard endpoints (`127.0.0.1`), the daemon also exempts `cloak.remoteHost` from tunnel routing so transport packets do not recurse into the VPN route table.

## Daemon state machine

States:

- `DISCONNECTED`
- `CONNECTING`
- `CONNECTED`
- `DISCONNECTING`
- `ERROR`

Connect flow:

1. Validate selected profile
2. Start Cloak manager and wait for local port
3. Start WireGuard manager
4. Confirm tunnel is up
5. Set `CONNECTED`

Disconnect flow:

1. Stop WireGuard
2. Stop Cloak
3. Set `DISCONNECTED`

Background loops:

- Health checks while connected (Cloak process + WireGuard status)

## Cloak in-process runtime

`daemon/internal/cloak/manager.go` runs Cloak entirely in-process using vendored client code at `daemon/internal/cloak/ck/`. No external `ck-client` or `cloak` binary is spawned.

The manager builds a `RawConfig` struct with fixed invariants:

- `ServerName` is always `www.microsoft.com`
- `RemotePort` is always `443`
- `LocalPort` is always `51820`
- `EncryptionMethod` is always `plain`
- `RemoteHost` is taken from the selected profile (`cloak.remoteHost`)

The in-process Cloak client opens a local UDP listener and routes WireGuard traffic through multiplexed TLS sessions to the remote Cloak server. Stopping is done by closing the UDP listener.

## Daemon service mode entrypoint

- CLI flag: `--service` (Windows only)
- Service name: `PangeaDaemon`
