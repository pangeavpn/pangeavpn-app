<div align="center">

<img src="apps/desktop/build/PangeaVPN.png" alt="PangeaVPN" width="128" height="128" />

# PangeaVPN

**One internet. No borders.**

WireGuard speed. HTTPS camouflage. Works where other VPNs don't.

[![Website](https://img.shields.io/badge/site-pangeavpn.org-DA7F4F?style=flat-square)](https://pangeavpn.org)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue?style=flat-square)](LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-555?style=flat-square)](#install)
[![Electron](https://img.shields.io/badge/electron-41-47848F?style=flat-square&logo=electron&logoColor=white)](https://www.electronjs.org/)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)
[![TypeScript](https://img.shields.io/badge/typescript-6.0-3178C6?style=flat-square&logo=typescript&logoColor=white)](https://www.typescriptlang.org/)

[Install](#install) · [How it works](#how-it-works) · [Build from source](#build-from-source) · [Architecture](#architecture) · [Security](#security) · [Roadmap](#roadmap)

</div>

---

## Why PangeaVPN

Most VPNs leak a tell. A weird port. A WireGuard handshake signature. A TLS fingerprint that doesn't match a real browser. Deep packet inspection sees them coming and drops the connection.

PangeaVPN wraps WireGuard inside Cloak, an obfuscation transport that makes your tunnel look like a perfectly ordinary HTTPS session to regular, everyday websites. To the network, you're casually browsing the web. To you, it's a full VPN.

That means it works in places other clients don't: restrictive corporate networks, captive Wi-Fi, hotels, regions with active censorship, and anywhere a firewall is sniffing for VPN traffic.

## Features

| | |
|---|---|
| **🕶️ Traffic obfuscation** | Tunnel disguised as HTTPS to evade DPI, censorship, and protocol fingerprinting |
| **⚡ WireGuard core** | Modern crypto, low latency, fully in-process — no `wg`, no `wg-quick`, no shelling out |
| **🔐 Encrypted hub channel** | Per-request X25519 + AES-256-GCM. Works behind MITM proxies that break standard TLS |
| **🛡️ Kill switch** | OS-level firewall rules block traffic if the tunnel drops (Windows WFP, Linux nftables/iptables, macOS PF) |
| **🔒 Lockdown mode** | Optional setting keeps the kill switch armed even after disconnect |
| **🌐 DoH + direct IP** | Falls back to DNS-over-HTTPS, or bypasses DNS entirely when it's blocked |
| **🖥️ Native desktop** | Compact taskbar popover, dark/light themes, system tray, auto-start at login |
| **📦 Real installers** | NSIS + portable (Windows), `.pkg` with launchd (macOS), AppImage + `.deb` (Linux) |

## Install

### Download

Grab the latest build for your OS from [pangeavpn.org](https://pangeavpn.org) or the [Releases](../../releases) page.

| Platform | Format | Notes |
|---|---|---|
| **Windows 10/11** | `Setup.exe` (NSIS) | Installs `PangeaDaemon` as a Windows service — no prompts on every connect |
| **Windows portable** | `.exe` | Bundles daemon, requests UAC on first connect, no install |
| **macOS** (Intel + Apple Silicon) | `.pkg` | Registers a launchd daemon at install time. No runtime password prompts |
| **Linux** | `.AppImage`, `.deb` (x64 + arm64) | Or run `./scripts/install-linux.sh` for a from-source install with systemd |

### One-shot install (macOS)

```bash
curl -fsSL https://pangeavpn.org/install-mac.sh | bash
```

Detects your architecture, fetches the latest DMG from the hub (GitHub fallback), strips quarantine, mounts it, installs the `.pkg`, and registers the LaunchDaemon. No Finder, no clicking through prompts.

### Quick install from source (Linux)

```bash
git clone https://github.com/PangeaVPN/PangeaVPN.git
cd PangeaVPN
./scripts/install-linux.sh
```

## How it works

```
          Your network sees this:                        What's actually happening:

      ┌──────────────────────────────┐                ┌──────────────────────────────┐
      │  HTTPS to www.microsoft.com  │                │       WireGuard tunnel       │
      │          (port 443)          │                │    inside an obfuscation     │
      │                              │   ◄──────►     │   stream encrypted again as  │
      │  "Just someone reading docs" │                │      real-looking HTTPS      │
      └──────────────────────────────┘                └──────────────────────────────┘
```

Behind the scenes:

1. **You authenticate** with a VPN token. The desktop app encrypts the request with a fresh X25519 keypair and POSTs it to the hub.
2. **The hub provisions a peer** on the best VPN node and ships back a WireGuard config + Cloak credentials over the same encrypted channel.
3. **The local daemon builds the tunnel.** Cloak opens a TLS session that looks like HTTPS. WireGuard runs over it. The OS routes traffic into the tunnel.
4. **The kill switch arms** so a dropped tunnel can't leak your real IP.

All four pieces happen in well under a second on a normal connection.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                  Your device                                    │
│                                                                                 │
│   ┌──────────────┐    sandboxed IPC    ┌───────────────┐                        │
│   │   Renderer   │ ◄────────────────►  │ Electron Main │                        │
│   │  (no Node)   │   contextBridge     │   process     │                        │
│   └──────────────┘                     └───────┬───────┘                        │
│                                                │                                │
│                                                │  Bearer-auth HTTP              │
│                                                │  127.0.0.1:8787                │
│                                                ▼                                │
│                                        ┌──────────────┐                         │
│                                        │  Go daemon   │                         │
│                                        │ (state mach.)│                         │
│                                        └───┬──────┬───┘                         │
│                                            │      │                             │
│                                  ┌─────────▼──┐   │                             │
│                                  │   Cloak    │   │                             │
│                                  │ (HTTPS-ish)│   │                             │
│                                  └─────────┬──┘   │                             │
│                                            │      │                             │
│                                            ▼      ▼                             │
│                                       ┌──────────────┐                          │
│                                       │  WireGuard   │  in-process, no `wg`     │
│                                       │   (Go lib)   │                          │
│                                       └──────┬───────┘                          │
└──────────────────────────────────────────────┼──────────────────────────────────┘
                                               │
                                               ▼
                                      ┌──────────────┐
                                      │   VPN node   │
                                      └──────────────┘
```

Three components, one repo:

- **`apps/desktop`** — Electron 41 + TypeScript 6.0. Sandbox enabled, context isolation on, no Node access from the renderer. Vanilla HTML/CSS — no framework.
- **`daemon`** — Go HTTP daemon at `127.0.0.1:8787`. Bearer token auth, rate limited, 1 MB body cap, sanitized errors. Owns the state machine and the in-process WireGuard + Cloak managers.
- **`packages/shared-types`** — Zod schemas shared between Electron and daemon-facing TypeScript.

Full deep-dive in [docs/architecture.md](docs/architecture.md).

### Platform implementations

| Platform | WireGuard | Obfuscation | Daemon model | Kill switch |
|---|---|---|---|---|
| **Windows** | In-process (`wireguard/windows`) | In-process | Windows service or UAC-elevated portable | ✅ WFP firewall rules |
| **macOS** | In-process (Go lib + TUN) | In-process | launchd system daemon | ✅ PF rules |
| **Linux** | In-process (Go lib + TUN, policy routing + fwmark) | In-process | systemd service | ✅ nftables (iptables fallback) |

No external `wg`, `wg-quick`, `wireguard-go`, or `ck-client` binaries are spawned on any platform.

## Security

PangeaVPN is open source so you can verify every claim below.

### Encrypted hub channel

The app talks to the hub server over its own encrypted channel, not TLS — because the threat model includes captive portals and corporate proxies that *intentionally* MITM TLS.

Per request:

1. Fresh ephemeral X25519 keypair → forward secrecy
2. ECDH with the hub's pinned static public key
3. HKDF-SHA256 derives a 32-byte AES key (with salt + domain-separation info string)
4. AES-256-GCM encrypts the inner `{method, route, headers, body}`
5. Sent over HTTPS to `/v1/secure`; only an allowlist of client-facing routes is reachable

`rejectUnauthorized: false` on the TLS layer is intentional and load-bearing for this threat model. The pinned X25519 key is the actual trust anchor.

### Electron hardening

- `sandbox: true` and `contextIsolation: true` on the renderer
- Strict CSP (`default-src 'self'`, `object-src 'none'`, `base-uri 'none'`, `frame-src 'none'`, `form-action 'none'`)
- Navigation, window-open, and webview tags blocked in the main process
- Permission requests denied by default; TLS errors fatal in production; DevTools disabled in packaged builds
- Electron Fuses: `runAsNode` off, `enableNodeOptionsEnvironmentVariable` off, `enableNodeCliInspectArguments` off, cookie encryption on, `onlyLoadAppFromAsar` on
- Credentials in OS keychain via `safeStorage`
- Renderer DOM uses `createElement` + `textContent`; no `innerHTML` on user-controlled data

### Daemon hardening

- Bearer token required on all endpoints except `/ping`
- Token file at `0o600`; machine-scoped (`%ProgramData%`) for the Windows service, user-scoped elsewhere
- 30 requests/minute rate limit, 1 MB request cap, error messages sanitized
- Loopback-only listener (`127.0.0.1:8787`) — never exposed on a network interface

Want a deeper dive? See [docs/architecture.md](docs/architecture.md) and the source under `apps/desktop/src/main/` and `daemon/internal/`.

## Build from source

### Prerequisites

- **Node.js LTS** + npm
- **Go 1.25+** on `PATH` (or drop a prebuilt daemon at `daemon/bin/PangeaDaemon.exe` / `daemon/bin/daemon`)
- Platform toolchain for installers (NSIS on Windows, Xcode CLT on macOS, `dpkg` / `fakeroot` for `.deb`)

### Run in dev

```bash
npm install
npm run dev
```

> Windows: the dev script requests UAC so the daemon can configure the WireGuard adapter.

### Build commands

| Command | What it does |
|---|---|
| `npm run dev` | UI + daemon, hot-rebuilt and wired together |
| `npm run build` | Compile `shared-types` → desktop → daemon |
| `npm run build-bin:windows` | NSIS installer + portable `.exe` |
| `npm run build-bin:mac` | `.pkg` and `.zip` for both Intel and Apple Silicon |
| `npm run build-bin:linux` | AppImage + `.deb` (x64 + arm64) |
| `npm run build-bin` | All platforms |

### Project layout

```
apps/desktop/           Electron app (TypeScript, vanilla HTML/CSS)
  src/main/             Main process: IPC, daemon client, secure channel, auth, updater
  src/renderer/         UI: index.html + index.ts (no framework)
  src/shared/           IPC channel constants
daemon/                 Go daemon
  cmd/daemon/           Entry point
  internal/api/         HTTP handlers (rate-limited, sanitized)
  internal/auth/        Bearer token management
  internal/state/       State machine, config store, log store
  internal/cloak/       In-process Cloak runtime + vendored client
  internal/wg/          In-process WireGuard manager (build-tagged per OS)
  internal/platform/    Paths, kill switch, routes
packages/shared-types/  Zod schemas + TS types shared by both halves
scripts/                Dev + packaging scripts (Node MJS)
docs/                   Architecture and packaging deep-dives
```

## Roadmap

What's coming, in rough priority order:

- 📱 **Mobile clients** (iOS / Android) — the single biggest gap
- ✂️ **Split tunnelling** — per-app and per-CIDR routing
- 🪂 **Multi-hop / cascade routing** — chain two nodes for unlinkability
- ⏰ **Auto-connect rules** — untrusted-Wi-Fi, on-boot, on-captive-portal-exit
- 🧬 **Pluggable transports** — obfs4, Shadowsocks/V2Ray, Hysteria alongside Cloak
- 🔀 **SNI rotation / domain fronting** — defeat single-fingerprint blocks
- 🚀 **Kernel WireGuard on Windows + Linux** — toward gigabit throughput
- 🤖 **CI release pipeline + reproducible builds**

See [features.md](features.md) and [optimisations.md](optimisations.md) for the long form.

## Contributing

PRs and issues welcome. A few notes:

- **One commit per logical change** — easier to review, easier to revert.
- **Don't change `rejectUnauthorized` on the secure channel** — it's deliberate. Read `docs/architecture.md` first if it looks wrong.
- **Don't shell out to `wg`, `wireguard-go`, or `ck-client`** — everything runs in-process for a reason.
- **No tests yet** — if you add some, that counts as a feature.

## Links

- 🌍 Website: [pangeavpn.org](https://pangeavpn.org)
- 📖 Architecture: [docs/architecture.md](docs/architecture.md)
- 📦 Packaging: [docs/binaries-and-packaging.md](docs/binaries-and-packaging.md)
- 🗒️ Changelog: [CHANGELOG.md](CHANGELOG.md)
- 📋 Wishlist: [features.md](features.md) · [optimisations.md](optimisations.md)

## License

[GPL-3.0](LICENSE) — free as in freedom. If you ship a fork, ship the source.
