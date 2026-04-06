# PangeaVPN

**Open-source, cross-platform VPN client with built-in traffic obfuscation.**

[pangeavpn.org](https://pangeavpn.org)

PangeaVPN combines WireGuard tunneling with traffic obfuscation to bypass deep packet inspection and censorship. Traffic is disguised as regular HTTPS, making VPN connections indistinguishable from normal web browsing — even behind restrictive firewalls, captive portals, and corporate proxies.

## Features

- **Traffic obfuscation** — VPN traffic masquerades as HTTPS to evade DPI and censorship
- **WireGuard tunneling** — fast, modern, and fully in-process on all platforms
- **Secure hub communication** — per-request X25519 ECDH + AES-256-GCM encryption, independent of TLS
- **Kill switch** — firewall rules prevent traffic leaks during tunnel transitions (Windows)
- **DNS-over-HTTPS fallback** — resolves servers via DoH when standard DNS is censored
- **Direct IP connect** — bypass DNS entirely when it's blocked
- **Cross-platform** — Windows (installer + portable), macOS (Intel + Apple Silicon), Linux
- **System tray** — background operation with connection status indicator

## Quick start

```bash
npm install
npm run dev
```

> On Windows, `npm run dev` requests UAC elevation to start the daemon as administrator.

## Prerequisites

- **Node.js** (LTS) + npm
- **Go 1.22+** on `PATH` for development (or a prebuilt daemon binary — see below)

Prebuilt daemon binaries can be placed at `daemon/bin/PangeaDaemon.exe` (Windows) or `daemon/bin/daemon` (macOS/Linux) to skip the Go requirement.

## Commands

| Command | Description |
|---------|-------------|
| `npm install` | Install all workspace dependencies |
| `npm run dev` | Run UI + daemon in development mode |
| `npm run build` | Build shared types, desktop app, and daemon |
| `npm run build-bin:windows` | Package Windows NSIS installer + portable |
| `npm run build-bin:mac` | Package macOS .pkg/.zip (x64 + arm64) |
| `npm run build-bin` | Build all platform targets |

## Architecture

```
┌─────────────┐    IPC (sandboxed)    ┌─────────────────┐
│  Renderer    │◄────────────────────►│  Main Process    │
│  (HTML/CSS)  │    contextBridge     │  (Electron)      │
└─────────────┘                      └────────┬─────────┘
                                              │
                                     HTTP (Bearer token)
                                              │
                                     ┌────────▼─────────┐
                                     │   Go Daemon       │
                                     │  127.0.0.1:8787   │
                                     └────────┬─────────┘
                                              │
                                     ┌────────▼─────────┐
                                     │  Obfuscation      │
                                     │  + WireGuard       │
                                     │  → VPN Node        │
                                     └──────────────────┘
```

### Project structure

```
apps/desktop/          Electron app (TypeScript, vanilla HTML/CSS)
daemon/                Go HTTP daemon (obfuscation + WireGuard)
packages/shared-types/ Shared Zod schemas and TypeScript types
scripts/               Build and dev scripts
docs/                  Architecture and packaging docs
```

### Platform details

| Platform | Obfuscation | WireGuard | Privileges |
|----------|-------------|-----------|------------|
| Windows | In-process | In-process (wireguard/windows) | UAC elevation or Windows service |
| macOS | In-process | In-process | Root via launchd daemon |
| Linux | In-process | In-process | Root |

Installed macOS `.pkg` builds register a launchd daemon at install time — no runtime admin prompts.

## Security

- **Secure channel**: all hub API traffic uses per-request ephemeral X25519 key exchange with AES-256-GCM, providing forward secrecy independent of TLS
- **Electron sandbox**: renderer is fully sandboxed with context isolation, no Node.js access
- **Daemon hardening**: Bearer token auth, rate limiting (30 req/min), 1 MB body limit, sanitized error responses
- **Credential storage**: OS keychain via Electron `safeStorage`

## License

[GPL-3.0](LICENSE)
