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
`Stop`, `State`, …) plus four callbacks: `SocketProtector` (VpnService.protect),
`StatusSink` (state pushes), `SecretStore` (encrypted persistence) and
`NetworkKeyProvider` (which network the device is on). See `daemon/mobile/mobile.go`.

The iOS app is the behavioural reference: `core` ports its stores
(`data/*Repository`, `session/`, `servers/`, `connection/`, `settings/`, `tunnel/`)
with the same rules and timings, and the screens follow its views.

## Prerequisites

- Android SDK platform 36 + build-tools 35 + platform-tools (AGP installs the
  first two on demand once the licences are accepted)
- Android NDK r28 or newer (CI uses r30, `ANDROID_NDK` in
  `.github/workflows/build-android.yml`). r27 links the naive engine badly (its
  libc++abi carries PAuth relocations r27's lld rejects) and, without the linker
  flag below, aligns native code to 4 KB, which Play refuses.
- JDK 17 or newer
- Go 1.26+ and `gomobile`, installed from `daemon/` so the CLI matches the
  `golang.org/x/mobile` version `go.mod` pins:
  ```bash
  cd daemon
  go install golang.org/x/mobile/cmd/gomobile golang.org/x/mobile/cmd/gobind
  gomobile init
  ```
- `local.properties` with `sdk.dir` (and `ndk.dir` if not auto-detected)

## Build

### 1. Build the Go core AAR

From `daemon/`, the Go module root:

```bash
gomobile bind -target=android -androidapi 24 -javapkg=org.pangeavpn \
  -tags=with_utls '-ldflags=-checklinkname=0 -extldflags=-Wl,-z,max-page-size=16384' \
  -o ../apps/android/core/libs/pangeacore.aar \
  ./mobile
```

`max-page-size=16384` aligns `libgojni.so` for 16 KB page devices, which Play
requires for apps targeting API 35 and up. CI checks every `.so` with
`llvm-readelf -lW` and fails on a `LOAD` segment aligned below 16 KB.

`with_utls` is required too: without it sing-box compiles REALITY's client out,
and AnyTLS falls back to Go's own ClientHello instead of Chrome's.

`-checklinkname=0` is required: sing-box pulls in `wlynxg/anet`, which
`//go:linkname`s into `net.zoneCache` to read interfaces on Android, and Go
1.23+ rejects that push without the flag.

This produces `org.pangeavpn.mobile.Mobile` and drops the AAR where `:core`
consumes it. Re-run whenever `daemon/mobile` changes.

The filename matters: `core/libs` is a `flatDir` repository declared in
`settings.gradle.kts`, so `pangeacore.aar` resolves as the module `:pangeacore`
rather than as a file. It has to be a module — `lintVital` bundles `:core` into
an AAR on release builds, and AGP rejects that outright when an `.aar` appears
as a file dependency. A missing AAR now fails resolution instead of failing
later at compilation.

### 1b. Stage the NaiveProxy engine (optional, arm64-v8a only)

```bash
node scripts/fetch-naive-android.mjs   # from the repo root
```

This downloads the pinned `libpangea_naive.a` from the naiveproxy fork's release
into `daemon/internal/naive/android/arm64-v8a/` (gitignored). Use
`-tags=with_utls,naive_cgo` in the `gomobile bind` above to link it; without the
tag, or on the other three ABIs, the cascade skips NaiveProxy.

It is arm64-v8a only on purpose. Measured on the debug APK, linking it takes
`lib/arm64-v8a/libgojni.so` from 27.3 MB to 43.9 MB (+16.6 MB); the other three
ABIs come out byte-identical. Everything since 2019 is 64-bit ARM, so the ABIs
paying nothing are also the ones nobody ships to.

Chromium opens its own sockets, so the
engine is handed a protect callback (`naive.ProtectFD`) that reaches
`VpnService.protect()`; without it naive would dial its server through the TUN
it is establishing and hang rather than fail.

### 2. Build the apps

```bash
cd apps/android
./gradlew :app-mobile:assembleRelease   # phone/tablet APK
./gradlew :app-tv:assembleRelease       # Android TV APK
```

Debug: swap `assembleDebug`. Play App Bundles: `:app-mobile:bundleRelease` and
`:app-tv:bundleRelease`.

Release builds run R8 (`isMinifyEnabled`). The gomobile boundary is bound by
name over JNI, so its keeps live in `core/consumer-rules.pro`.

`assembleRelease` writes one APK per ABI (`arm64-v8a`, `armeabi-v7a`,
`x86_64`) plus a universal one, for direct download; bundles ignore the split.
32-bit x86 native code is left out of everything.

### Web billing links

Play builds must not send people to buy elsewhere, so the apps show no link to
the website's billing pages unless built with `-Ppangea.webBilling=true`, for
channels outside Play. The flag reaches the code as `BuildConfig.WEB_BILLING`.

## Versioning

`versionName` is read from the repo's root `package.json` by
`apps/android/build.gradle.kts`, so the apps ride the desktop's version line.
`versionCode` is derived from it: `major * 10000000 + minor * 100000 + patch * 1000
+ fourth * 100 + build`, where `build` is the rc number for `-rc.N` and 99 for a
final, so `0.8.3-rc.2` is 803002 and `0.8.3` is 803099. A release candidate
uploaded to a test track therefore never blocks its final. Any other version
shape fails the build. Bump the version in `package.json`.

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

CI signs when these repository secrets exist, and uploads the release APKs,
bundles and R8 mapping files as the `android-release` artifact:

| Secret | What it is |
|---|---|
| `ANDROID_KEYSTORE_BASE64` | the `.jks` file, base64 |
| `ANDROID_KEYSTORE_PASSWORD`, `ANDROID_KEY_ALIAS`, `ANDROID_KEY_PASSWORD` | its credentials |

With Play App Signing, this keystore is the upload key.

## Localization

`strings.xml` for all 8 locales is generated:

```bash
node scripts/gen-android-strings.mjs
node scripts/gen-ios-strings.mjs   # iOS reads the Android XML, so regenerate it too
```

Sources, in order: desktop keys mapped in `KEY_MAP`, the inline `MOBILE_ONLY`
strings, every iOS string from `scripts/ios-strings/*.mjs` (bar the App Store
ones), and the Android-only strings in `scripts/android-strings/*.mjs`, one file
per feature area. Edit those, never the generated XML. A value starting with
`@plural:` becomes a `<plurals>` resource.

## Play Console

- Declare the VpnService use and the `systemExempted` foreground service type.
- Data safety: the same answers as the App Store label (data not collected).
- Account deletion: in the app (Settings → Delete Account); for the web route,
  give Play the dashboard at `https://pangeavpn.org/app`, which has a Delete
  account button.
- Phone apps must target API 36 (since 31 August 2026); TV apps 34.

## Notes

- `minSdk 24` (maximizes Android TV coverage), `targetSdk 36`.
- The kill switch is Android's system *Always-on VPN → Block connections without
  VPN*; the app deep-links to it. On its own the service fails closed: it keeps
  its TUN up while it reconnects, waits out a lost network or switches servers,
  and hands the core a `dup()` of the fd, so a core stop never removes it.
- Only sockets that egress the real network are `protect()`ed; the WireGuard↔Cloak
  hop is loopback and isn't captured by the TUN.
- Wi-Fi↔mobile handovers are rebuilt, not roamed. WireGuard survives a changed
  address but the TCP transport under it does not, so `NetworkMonitor` watches
  the non-VPN networks (via the same fingerprint the cascade keys its memory on)
  and the service reconnects over the network that replaced it, with the iOS
  debounce, backoff and liveness rules (`core/tunnel/`). The `NOT_VPN`
  capability keeps our own tunnel from registering as a change.
