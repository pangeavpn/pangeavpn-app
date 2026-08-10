# PangeaVPN Android + Android TV

Kotlin/Compose clients for phone, tablet, and Android TV. The tunnel and hub
control plane are shared with the desktop app via a Go core compiled to an AAR
with `gomobile` — WireGuard-inside-Cloak runs in-process, identical to desktop.

## Modules

| Module | What it is |
|---|---|
| `daemon/mobile` (Go) | gomobile source: secure channel, DoH transport, provisioning, WireGuard + Cloak on the VpnService TUN fd. Builds to `pangeacore.aar`. |
| `core` | VpnService, `TunnelBridge` (AAR wrapper), `SecretStore`, repositories, ViewModels, shared models. |
| `ui-common` | Compose theme + reusable components + localized `strings.xml` (8 languages). |
| `app-mobile` | Phone/tablet app (`org.pangeavpn.app`). |
| `app-tv` | Android TV app (`org.pangeavpn.tv`), D-pad / leanback. |

The Go layer exposes a JSON API (`Login`, `ListServers`, `Prepare`, `Start(fd)`,
`Stop`, `State`, …) plus three callbacks: `SocketProtector` (VpnService.protect),
`StatusSink` (state pushes), `SecretStore` (encrypted persistence). See
`daemon/mobile/mobile.go`.

## Prerequisites

- Android SDK (compileSdk 35) + platform-tools
- Android NDK
- JDK 17
- Go 1.25+ and `gomobile`:
  ```bash
  go install golang.org/x/mobile/cmd/gomobile@latest
  gomobile init
  ```
- `local.properties` with `sdk.dir` (and `ndk.dir` if not auto-detected)

## Build

### 1. Build the Go core AAR

From `daemon/`, the Go module root:

```bash
gomobile bind -target=android -androidapi 24 -javapkg=org.pangeavpn \
  -o ../apps/android/core/libs/pangeacore.aar \
  ./mobile
```

This produces `org.pangeavpn.mobile.Mobile` and drops the AAR where `:core`
consumes it (`core/libs/*.aar`). Re-run whenever `daemon/mobile` changes.

### 2. Build the apps

```bash
cd apps/android
./gradlew :app-mobile:assembleRelease   # phone/tablet APK
./gradlew :app-tv:assembleRelease       # Android TV APK
```

Debug: swap `assembleDebug`. Play App Bundle: `:app-mobile:bundleRelease`.

Release signing is not configured here — add a `signingConfig` (keystore) before
publishing.

## Localization

`strings.xml` for all 8 locales is generated from the desktop translations:

```bash
node scripts/gen-android-strings.mjs
```

Edit the key map / mobile-only strings in that script, not the generated XML.

## Notes

- `minSdk 24` (maximizes Android TV coverage), `targetSdk 35`.
- The kill switch is Android's system *Always-on VPN → Block connections without
  VPN*; the app deep-links to it and keeps the TUN up across reconnects (fail-closed).
- Only sockets that egress the real network are `protect()`ed; the WireGuard↔Cloak
  hop is loopback and isn't captured by the TUN.
