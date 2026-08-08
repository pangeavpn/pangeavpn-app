<div align="center">

<img src="apps/desktop/build/PangeaVPN.png" alt="PangeaVPN" width="128" height="128" />

# PangeaVPN

**One internet. No borders.**

An open-source VPN client that survives deep packet inspection. WireGuard inside four pluggable censorship-resistant transports, so the tunnel looks like ordinary HTTPS.

[![Website](https://img.shields.io/badge/site-pangeavpn.org-DA7F4F?style=flat-square)](https://pangeavpn.org)
[![License](https://img.shields.io/badge/license-GPL--3.0-blue?style=flat-square)](LICENSE)
[![Platforms](https://img.shields.io/badge/platforms-Windows%20%7C%20macOS%20%7C%20Linux-555?style=flat-square)](#install)
[![Languages](https://img.shields.io/badge/languages-8-DA7F4F?style=flat-square)](#languages)
[![Electron](https://img.shields.io/badge/electron-41-47848F?style=flat-square&logo=electron&logoColor=white)](https://www.electronjs.org/)
[![Go](https://img.shields.io/badge/go-1.25+-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/)

[Install](#install) · [Transports](#transports) · [How it works](#how-it-works) · [Verify your download](#verify-your-download) · [Architecture](#architecture) · [Security](#security) · [Build from source](#build-from-source)

</div>

---

## What this is

PangeaVPN is a desktop VPN client for **Windows, macOS and Linux**, built for networks that actively detect and block VPNs — national firewalls, ISP-level DPI, corporate filters, captive Wi-Fi, and hotel networks.

It runs WireGuard entirely in-process and wraps it in an obfuscation transport, so what crosses the wire looks like a normal HTTPS session rather than a recognisable VPN handshake. If one transport gets blocked, it automatically tries the next.

> **This client is open source; the network is a paid service.**
> The app in this repository is GPL-3.0 and yours to read, build, fork and audit. Connecting to the Pangea network needs an account — there's a **5-day free trial with no card required**, then from £2.33/month. Monero accepted, no email required. See [pangeavpn.org/pricing](https://pangeavpn.org/pricing).
> The client speaks a documented HTTP API, so pointing it at your own infrastructure is possible, though not currently a supported path.

## Why it exists

Most VPNs leak a tell. A weird port. A WireGuard handshake signature. A TLS fingerprint that doesn't match a real browser. Deep packet inspection sees them coming and drops the connection — and when a protocol gets fingerprinted, every user of that protocol goes down at once.

PangeaVPN's answer is transport diversity. Rather than betting on a single obfuscation scheme holding forever, the daemon carries several and falls back between them automatically. Blocking one doesn't take you offline.

## Transports

In auto mode the daemon walks this list until one establishes. You can also pin a specific transport.

| # | Transport | What the network sees |
|---|---|---|
| 1 | **Cloak** | A TLS session to an innocuous-looking web host |
| 2 | **VLESS + REALITY** | A TLS handshake borrowing a real third-party site's certificate |
| 3 | **Hysteria2** | QUIC / HTTP-3, hard to distinguish from modern web traffic |
| 4 | **NaiveProxy** | Traffic carrying a genuine Chrome TLS fingerprint |
| 5 | **Shadowsocks** | An encrypted stream on its own port — no TLS shape, but it fails independently of the four above |

Cloak is always available. The others activate when the hub provisions configuration for them, so the exact cascade depends on your account and the node you land on.

The client also remembers what worked on each network and promotes that transport to the front of the queue next time you connect there, so a network that blocks Cloak doesn't cost you the fallback delay twice.

A **Snowflake** transport (WebRTC, as used by Tor) is implemented and wired up but disabled in current releases — see `snowflakeReleaseGated` in [`daemon/internal/api/service.go`](daemon/internal/api/service.go).

WireGuard rides inside whichever transport comes up. The tunnel itself is always WireGuard: modern crypto, low latency, no `wg`, no `wg-quick`, no shelling out to external binaries on any platform.

## Features

| | |
|---|---|
| **Four transports, automatic fallback** | Blocking one doesn't take you offline, and the client remembers what works per network |
| **WireGuard core** | Modern crypto, low latency, fully in-process |
| **Kill switch** | OS-level firewall rules block traffic if the tunnel drops (Windows WFP, Linux nftables/iptables, macOS PF) |
| **Lockdown mode** | Optionally keeps the kill switch armed even after disconnect |
| **Encrypted hub channel** | Per-request X25519 + AES-256-GCM — works behind proxies that intercept TLS |
| **DoH + direct IP** | Falls back to DNS-over-HTTPS, or bypasses DNS entirely when it's blocked |
| **8 languages** | Including Persian, Arabic, Chinese, Russian and Ukrainian |
| **Native desktop** | Compact taskbar popover, dark/light themes, system tray, auto-start at login |
| **Real installers** | NSIS (Windows), `.pkg` with launchd (macOS), AppImage + `.deb` (Linux) |

## Languages

English · Español · Français · Русский · Українська · 中文 · العربية · فارسی

Translation improvements are welcome — locale files live in [`apps/desktop/src/renderer/i18n/locales/`](apps/desktop/src/renderer/i18n/locales/).

## Install

Download for your platform from [pangeavpn.org/download](https://pangeavpn.org/download) or the [Releases](../../releases) page.

| Platform | Format | Notes |
|---|---|---|
| **Windows 10/11** | `Setup.exe` (NSIS) | Installs `PangeaDaemon` as a Windows service — no prompt on every connect |
| **macOS** (Intel + Apple Silicon) | `.pkg` | Registers a launchd daemon at install time. No runtime password prompts |
| **Linux** | `.AppImage`, `.deb` (x64 + arm64) | Or `./scripts/install-linux.sh` for a from-source install with systemd |

### macOS one-command install

```bash
curl -fsSL https://pangeavpn.org/install-mac.sh | bash
```

Convenient, but it pipes a remote script into a root shell — reasonable for developers, and we'd rather you read [`scripts/install-mac.sh`](scripts/install-mac.sh) first. The `.pkg` from the releases page does the same job with a normal guided installer.

### Linux from source

```bash
git clone https://github.com/pangeavpn/pangeavpn-app.git
cd pangeavpn-app
./scripts/install-linux.sh
```

## Verify your download

**Releases are not yet code-signed.** We'd rather say that plainly than have you discover it at a SmartScreen or Gatekeeper prompt.

Code signing certificates require either a registered legal entity or an annual fee that the project doesn't currently cover. It's on the roadmap. Until then, Windows will show *"Windows protected your PC"* (click **More info → Run anyway**) and macOS will warn that the developer can't be verified.

What you can do instead of taking our word for it:

- **Check the hash.** Every release publishes SHA-256 checksums for each artifact. Compare before installing:
  ```bash
  # macOS / Linux
  shasum -a 256 PangeaVPN-Setup-*.exe
  ```
  ```powershell
  # Windows
  Get-FileHash .\PangeaVPN-Setup-0.5.1-x64.exe -Algorithm SHA256
  ```
- **Check where it was built.** Every release artifact is produced by [GitHub Actions](.github/workflows/build-desktop.yml) from the tagged commit, in public, with public logs.
- **Build it yourself.** See [Build from source](#build-from-source). You do not have to trust our binaries at all.
- **Read the code.** That's the point of the licence.

Bundled `wintun.dll` and `wireguard.dll` are the official, Authenticode-signed builds from WireGuard LLC — we don't rebuild them.

## How it works

```
          Your network sees this:                        What's actually happening:

      ┌──────────────────────────────┐                ┌──────────────────────────────┐
      │   An ordinary HTTPS session  │                │       WireGuard tunnel       │
      │          (port 443)          │   ◄──────►     │    inside an obfuscation     │
      │  "Just someone browsing"     │                │   transport that looks like  │
      └──────────────────────────────┘                │       real-world HTTPS       │
                                                      └──────────────────────────────┘
```

1. **You authenticate.** The app encrypts the request with a fresh X25519 keypair and POSTs it to the hub.
2. **The hub provisions a peer** on the best node and returns a WireGuard config plus transport credentials over the same encrypted channel.
3. **The local daemon builds the tunnel.** It walks the transport list until one connects, brings up WireGuard over it, and routes traffic in.
4. **The kill switch arms** so a dropped tunnel can't leak your real IP.

All four steps complete in well under a second on a normal connection.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                                  Your device                                    │
│                                                                                 │
│   ┌──────────────┐    sandboxed IPC    ┌───────────────┐                        │
│   │   Renderer   │ ◄────────────────►  │ Electron Main │                        │
│   │  (no Node)   │   contextBridge     │   process     │                        │
│   └──────────────┘                     └───────┬───────┘                        │
│                                                │  Bearer-auth HTTP              │
│                                                │  127.0.0.1:8787                │
│                                                ▼                                │
│                                        ┌──────────────┐                         │
│                                        │  Go daemon   │                         │
│                                        │ (state mach.)│                         │
│                                        └───┬──────┬───┘                         │
│                                            │      │                             │
│                          ┌─────────────────▼──┐   │                             │
│                          │ Cloak / REALITY /  │   │                             │
│                          │ Hysteria2 / Naive /│   │                             │
│                          │    Shadowsocks     │   │                             │
│                          └─────────────────┬──┘   │                             │
│                                            ▼      ▼                             │
│                                       ┌──────────────┐                          │
│                                       │  WireGuard   │  in-process, no `wg`     │
│                                       │   (Go lib)   │                          │
│                                       └──────┬───────┘                          │
└──────────────────────────────────────────────┼──────────────────────────────────┘
                                               ▼
                                      ┌──────────────┐
                                      │   VPN node   │
                                      └──────────────┘
```

Three components, one repo:

- **`apps/desktop`** — Electron + TypeScript. Sandbox enabled, context isolation on, no Node access from the renderer. Vanilla HTML/CSS, no framework.
- **`daemon`** — Go HTTP daemon on `127.0.0.1:8787`. Bearer token auth, rate limited, 1 MB body cap, sanitised errors. Owns the state machine and the in-process WireGuard and transport managers.
- **`packages/shared-types`** — Zod schemas shared between Electron and the daemon-facing TypeScript.

Full deep-dive in [docs/architecture.md](docs/architecture.md).

### Platform implementations

| Platform | WireGuard | Transports | Daemon model | Kill switch |
|---|---|---|---|---|
| **Windows** | In-process (`wireguard/windows` + Wintun) | In-process | Windows service (LocalSystem) | WFP firewall filters |
| **macOS** | In-process (Go lib + utun) | In-process | launchd system daemon | PF rules |
| **Linux** | In-process (Go lib + TUN, policy routing + fwmark) | In-process | systemd service | nftables (iptables fallback) |

No external `wg`, `wg-quick`, `wireguard-go`, or `ck-client` binaries are spawned on any platform. This is enforced by a test — see [`daemon/internal/wg/no_exec_test.go`](daemon/internal/wg/no_exec_test.go).

## Security

PangeaVPN is open source so you can verify every claim below rather than believe it.

### Encrypted hub channel

The app talks to the hub over its own encrypted channel layered *inside* HTTPS, because the threat model explicitly includes captive portals and corporate proxies that intentionally intercept TLS.

Per request:

1. Fresh ephemeral X25519 keypair → forward secrecy
2. ECDH against the hub's pinned static public key
3. HKDF-SHA256 derives a 32-byte AES key (salt + domain-separation info string)
4. AES-256-GCM encrypts the inner `{method, route, headers, body}`
5. Sent to `/v1/secure`; only an allowlist of client-facing routes is reachable

> **On `rejectUnauthorized: false`.** The outer TLS layer deliberately does not perform CA validation, and this is a design decision rather than an oversight. On a network whose middlebox already terminates and re-signs TLS, CA validation either fails outright or succeeds against the middlebox's certificate — in both cases it tells you nothing. The actual trust anchor is the **pinned X25519 static key**: without the corresponding private key an interceptor cannot read or forge the inner payload, regardless of what it does to the outer TLS. Please read [docs/architecture.md](docs/architecture.md) before proposing a change here.

### Electron hardening

- `sandbox: true` and `contextIsolation: true` on the renderer
- Strict CSP (`default-src 'self'`, `object-src 'none'`, `base-uri 'none'`, `frame-src 'none'`, `form-action 'none'`)
- Navigation, window-open and webview tags blocked in the main process
- Permission requests denied by default; TLS errors fatal in production; DevTools disabled in packaged builds
- Electron Fuses: `runAsNode` off, `enableNodeOptionsEnvironmentVariable` off, `enableNodeCliInspectArguments` off, cookie encryption on, `onlyLoadAppFromAsar` on
- Credentials stored in the OS keychain via `safeStorage`
- Renderer DOM uses `createElement` + `textContent`; no `innerHTML` on user-controlled data

### Daemon hardening

- Bearer token required on every endpoint except `/ping`
- Token file at `0600`; machine-scoped (`%ProgramData%`) for the Windows service, user-scoped elsewhere
- 30 requests/minute rate limit, 1 MB request cap, sanitised error messages
- Loopback-only listener — never bound to a network interface

### Reporting a vulnerability

Please report security issues privately via [pangeavpn.org/contact](https://pangeavpn.org/contact) rather than opening a public issue.

## Build from source

### Prerequisites

- **Node.js LTS** + npm
- **Go 1.25+** on `PATH` (or drop a prebuilt daemon at `daemon/bin/PangeaDaemon.exe` / `daemon/bin/daemon`)
- Platform toolchain for installers: NSIS on Windows, Xcode Command Line Tools on macOS, `dpkg`/`fakeroot` for `.deb`
- The NaiveProxy transport needs cgo and a C toolchain; without the `naive_cgo` build tag it compiles to a stub that cleanly reports itself unavailable

### Run in dev

```bash
npm install
npm run dev
```

> Windows: the dev script requests UAC so the daemon can configure the WireGuard adapter.

### Commands

| Command | What it does |
|---|---|
| `npm run dev` | UI + daemon, hot-rebuilt and wired together |
| `npm run build` | Compile `shared-types` → desktop → daemon |
| `npm test` | TypeScript + Go test suites |
| `npm run build-bin:windows` | NSIS installer (x64 + arm64) |
| `npm run build-bin:mac` | `.pkg` for Intel and Apple Silicon |
| `npm run build-bin:linux` | AppImage + `.deb` (x64 + arm64) |
| `npm run build-bin` | All platforms |

### Project layout

```
apps/desktop/           Electron app (TypeScript, vanilla HTML/CSS)
  src/main/             Main process: IPC, daemon client, secure channel, auth, updater
  src/renderer/         UI + i18n locales
  src/shared/           IPC channel constants
daemon/                 Go daemon
  cmd/daemon/           Entry point + Windows service host
  internal/api/         HTTP handlers (rate-limited, sanitised)
  internal/auth/        Bearer token management
  internal/state/       State machine, config store, log store
  internal/cloak/       In-process Cloak runtime
  internal/naive/       NaiveProxy transport (cgo)
  internal/wg/          In-process WireGuard manager (build-tagged per OS)
  internal/platform/    Paths, kill switch, routes, WFP
packages/shared-types/  Zod schemas + TS types shared by both halves
scripts/                Dev + packaging scripts (Node MJS)
docs/                   Architecture and packaging deep-dives
```

## Roadmap

In rough priority order:

- **Mobile clients** (Android, iOS) — the single biggest gap
- **Code-signed releases** on Windows and macOS
- **Split tunnelling** — per-app and per-CIDR routing
- **Multi-hop / cascade routing** — chain two nodes for unlinkability
- **Auto-connect rules** — untrusted Wi-Fi, on-boot, on captive-portal exit
- **Snowflake transport** — implemented, pending production rollout
- **SNI rotation / domain fronting** — defeat single-fingerprint blocks
- **Reproducible builds** — so anyone can confirm a release matches this source
- **Kernel WireGuard on Windows and Linux** — toward gigabit throughput

## Contributing

PRs and issues welcome. A few notes:

- **One commit per logical change** — easier to review, easier to revert.
- **Don't change `rejectUnauthorized` on the secure channel** — it's deliberate. Read [docs/architecture.md](docs/architecture.md) first if it looks wrong.
- **Don't shell out to `wg`, `wireguard-go`, or `ck-client`** — everything runs in-process for a reason, and `no_exec_test.go` enforces it.
- **Run `npm test` before opening a PR.**

## Links

- Website: [pangeavpn.org](https://pangeavpn.org)
- Architecture: [docs/architecture.md](docs/architecture.md)
- Packaging: [docs/binaries-and-packaging.md](docs/binaries-and-packaging.md)
- Privacy policy: [pangeavpn.org/legal/privacy](https://pangeavpn.org/legal/privacy)
- Warrant canary: [pangeavpn.org/canary](https://pangeavpn.org/canary)

## License

[GPL-3.0](LICENSE) — free as in freedom. If you ship a fork, ship the source.
