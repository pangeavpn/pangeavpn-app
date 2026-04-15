# Binaries And Packaging

## Repo binary locations

- Windows runtime DLLs: `apps/desktop/resources/bin/win/wintun.dll`
- Windows `wireguard.dll` sources: `apps/desktop/build/{amd64|arm|arm64|x86}/wireguard.dll`
- Windows `wintun.dll` sources: `apps/desktop/build/{amd64|arm|arm64|x86}/wintun.dll`

Put `wintun.dll` in the Windows folder.
Put architecture-matched `wireguard.dll` files in the Windows build arch folders above.
No tunnel binaries (`wireguard-go`, `wg`, `cloak`, `ck-client`, `wireguard.exe`) are required on any platform — WireGuard and Cloak run in-process inside the daemon. Only `wintun.dll` / `wireguard.dll` are loaded at runtime (via `syscall.LoadLibrary` on Windows).

## Runtime binary resolution

The daemon binary is bundled at `process.resourcesPath/daemon/{daemon|PangeaDaemon.exe}`.
Implementation is in `apps/desktop/src/main/resourcePaths.ts`.

## Daemon packaging

`electron-builder` includes:

- `apps/desktop/resources/bin/**` -> app resources `bin/`
- `daemon/bin/**` -> app resources `daemon/`

Windows daemon build (`scripts/build-daemon.mjs`) stages exactly one `wireguard.dll` and one `wintun.dll` into `daemon/bin/` from the matching `apps/desktop/build/<arch>/` folder. Those staged files are what get bundled and installed.

Build config is in `apps/desktop/package.json` under `build.extraResources`.

Windows installer also includes NSIS custom service hooks via:

- `apps/desktop/build/installer.nsh`

The installer config runs per-machine and elevated (`build.nsis.perMachine`, `build.nsis.allowElevation`).
Windows packaging also sets `build.npmRebuild=false` to avoid npm-workspace rebuild issues during electron-builder packaging.

## Windows app icon

Place the Windows icon at:

- `apps/desktop/build/PangeaVPN.ico`

It is used for:

- Windows app executable (`build.win.icon`)
- NSIS installer (`build.nsis.installerIcon`)
- NSIS uninstaller (`build.nsis.uninstallerIcon`)
- NSIS installer header (`build.nsis.installerHeaderIcon`)
- Browser window/taskbar icon in dev and packaged runtime (via `apps/desktop/src/main/resourcePaths.ts`)

## Important note

Daemon token/config for Windows service-installed daemon:

- `%ProgramData%/PangeaVPN/daemon-token.txt`
- `%ProgramData%/PangeaVPN/config.json`

Windows WireGuard operations require the daemon process to run with administrator privileges.

On macOS:

- The daemon configures WireGuard and Cloak entirely in-process (no external tunnel binaries).
- The daemon process must run as root for tunnel bring-up/teardown.
- The `.pkg` installer registers `com.pangea.pangeavpn.daemon` as a system `launchd` daemon.

On Linux:

- The daemon configures WireGuard and Cloak entirely in-process (no external tunnel binaries).
- The daemon process must run as root for tunnel bring-up/teardown.
- DNS entries are applied during connect and restored during disconnect using systemd-resolved D-Bus when available, with `/etc/resolv.conf` fallback.

On Windows:

- Dev flow (`npm run dev`) requests UAC to run the daemon elevated.
- Packaged installer app relies on the installed `PangeaDaemon` Windows service (no routine UAC prompt).
- Packaged portable app launches bundled `resources/daemon/PangeaDaemon.exe` with elevation when service install is not available.

## Build commands

Use:

- `npm run build-bin:windows`

- `npm run build-bin:mac`

All targets:

- `npm run build-bin`

`build-bin` runs target builds in order (`windows`, then `mac`) and fails non-zero if any target fails.

`build-bin:mac` builds both macOS targets in one run:

- `x64` (Intel)
- `arm64` (Apple Silicon)

## Windows artifact outputs

`npm run build-bin:windows` produces:

- `dist/bin/windows/installer/*.exe` (NSIS installer)
- `dist/bin/windows/portable/*.exe` (portable app)
- `dist/bin/windows/daemon/PangeaDaemon.exe` (standalone daemon binary)
- `dist/bin/windows/daemon/wireguard.dll` (standalone daemon runtime dependency)
- `dist/bin/windows/daemon/wintun.dll` (standalone daemon runtime dependency)
- `dist/bin/windows/manifest.json` (artifact manifest with hashes)

`npm run build-bin` also writes:

- `dist/bin/manifest-all.json` (per-target status summary)

## macOS artifact outputs

`npm run build-bin:mac` produces:

- `dist/bin/mac/installer/x64/*.pkg`
- `dist/bin/mac/portable/x64/*.zip`
- `dist/bin/mac/installer/arm64/*.pkg`
- `dist/bin/mac/portable/arm64/*.zip`
- `dist/bin/mac/daemon/daemon-x64`
- `dist/bin/mac/daemon/daemon-arm64`
- `dist/bin/mac/manifest.json`
