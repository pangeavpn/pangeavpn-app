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
  -ldflags=-checklinkname=0 \
  -o ../apps/android/core/libs/pangeacore.aar \
  ./mobile
```

`-checklinkname=0` is required: sing-box pulls in `wlynxg/anet`, which
`//go:linkname`s into `net.zoneCache` to read interfaces on Android, and Go
1.23+ rejects that push without the flag.

This produces `org.pangeavpn.mobile.Mobile` and drops the AAR where `:core`
consumes it (`core/libs/*.aar`). Re-run whenever `daemon/mobile` changes.

### 2. Build the apps

```bash
cd apps/android
./gradlew :app-mobile:assembleRelease   # phone/tablet APK
./gradlew :app-tv:assembleRelease       # Android TV APK
```

Debug: swap `assembleDebug`. Play App Bundle: `:app-mobile:bundleRelease`.

Release builds run R8 (`isMinifyEnabled`). The gomobile boundary is bound by
name over JNI, so its keeps live in `core/consumer-rules.pro`.

## Versioning

`versionName` is read from the repo's root `package.json` by
`apps/android/build.gradle.kts`, so the apps ride the desktop's version line.
`versionCode` is derived from it (`0.5.2` → `502`); bump the version there.

## Release signing

Both app modules pick up a release `signingConfig` from a gitignored
`apps/android/keystore.properties`, falling back to environment variables:

```properties
storeFile=/absolute/path/to/pangea-release.jks
storePassword=...
keyAlias=pangea
keyPassword=...
```

Env equivalents: `PANGEA_KEYSTORE_FILE`, `PANGEA_KEYSTORE_PASSWORD`,
`PANGEA_KEY_ALIAS`, `PANGEA_KEY_PASSWORD`. With none of them set the release
build still runs and simply comes out unsigned, so CI needs no secrets.

Generate the keystore once and keep it out of the repo:

```bash
keytool -genkeypair -v -keystore pangea-release.jks -alias pangea \
  -keyalg RSA -keysize 4096 -validity 10000
```

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
