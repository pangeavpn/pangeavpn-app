import fs from "node:fs";
import path from "node:path";
import { app } from "electron";

const windowsIconName = "PangeaVPN.ico";
const windowsConnectedIconName = "PangeaVPN_connected.ico";
const macPngIconName = "PangeaVPN.png";
const macConnectedPngIconName = "PangeaVPN_connected.png";
const macIconName = "PangeaVPN.icns";
const macConnectedIconName = "PangeaVPN_connected.icns";
const macIcoFallbackName = "PangeaVPN.ico";
const macConnectedIcoFallbackName = "PangeaVPN_connected.ico";

export function getBundledDaemonPath(): string {
  const name = process.platform === "win32" ? "PangeaDaemon.exe" : "daemon";
  return path.join(process.resourcesPath, "daemon", name);
}

export function getWindowsAppIconPath(mainModuleDir: string): string | undefined {
  if (process.platform !== "win32") {
    return undefined;
  }

  const candidates = app.isPackaged
    ? [
        path.join(process.resourcesPath, "build", windowsIconName),
        path.join(process.resourcesPath, windowsIconName)
      ]
    : [
        path.resolve(mainModuleDir, "..", "..", "build", windowsIconName),
        path.resolve(process.cwd(), "apps", "desktop", "build", windowsIconName)
      ];

  return candidates.find((candidate) => fs.existsSync(candidate));
}

export function getTrayIconPath(mainModuleDir: string): string | undefined {
  if (process.platform === "win32") {
    return getWindowsAppIconPath(mainModuleDir);
  }
  if (process.platform !== "darwin") {
    return undefined;
  }

  const candidates = app.isPackaged
    ? [
        path.join(process.resourcesPath, "build", macPngIconName),
        path.join(process.resourcesPath, macPngIconName),
        path.join(process.resourcesPath, "build", "PangeaVPNTemplate.png"),
        path.join(process.resourcesPath, "PangeaVPNTemplate.png"),
        path.join(process.resourcesPath, "build", macIconName),
        path.join(process.resourcesPath, macIconName),
        path.join(process.resourcesPath, "build", macIcoFallbackName),
        path.join(process.resourcesPath, macIcoFallbackName)
      ]
    : [
        path.resolve(mainModuleDir, "..", "..", "build", macPngIconName),
        path.resolve(process.cwd(), "apps", "desktop", "build", macPngIconName),
        path.resolve(mainModuleDir, "..", "..", "build", "PangeaVPNTemplate.png"),
        path.resolve(process.cwd(), "apps", "desktop", "build", "PangeaVPNTemplate.png"),
        path.resolve(mainModuleDir, "..", "..", "build", macIconName),
        path.resolve(mainModuleDir, "..", "..", "built", "pangeavpn.icns"),
        path.resolve(process.cwd(), "apps", "desktop", "build", macIconName),
        path.resolve(process.cwd(), "apps", "desktop", "built", "pangeavpn.icns"),
        path.resolve(mainModuleDir, "..", "..", "build", macIcoFallbackName),
        path.resolve(process.cwd(), "apps", "desktop", "build", macIcoFallbackName)
      ];

  return candidates.find((candidate) => fs.existsSync(candidate));
}

export function getConnectedTrayIconPath(mainModuleDir: string): string | undefined {
  if (process.platform === "win32") {
    const candidates = app.isPackaged
      ? [
          path.join(process.resourcesPath, "build", windowsConnectedIconName),
          path.join(process.resourcesPath, windowsConnectedIconName)
        ]
      : [
          path.resolve(mainModuleDir, "..", "..", "build", windowsConnectedIconName),
          path.resolve(process.cwd(), "apps", "desktop", "build", windowsConnectedIconName)
        ];

    return candidates.find((candidate) => fs.existsSync(candidate));
  }
  if (process.platform !== "darwin") {
    return undefined;
  }

  const candidates = app.isPackaged
    ? [
        path.join(process.resourcesPath, "build", macConnectedPngIconName),
        path.join(process.resourcesPath, macConnectedPngIconName),
        path.join(process.resourcesPath, "build", "PangeaVPN_connectedTemplate.png"),
        path.join(process.resourcesPath, "PangeaVPN_connectedTemplate.png"),
        path.join(process.resourcesPath, "build", macConnectedIconName),
        path.join(process.resourcesPath, macConnectedIconName),
        path.join(process.resourcesPath, "build", macConnectedIcoFallbackName),
        path.join(process.resourcesPath, macConnectedIcoFallbackName)
      ]
    : [
        path.resolve(mainModuleDir, "..", "..", "build", macConnectedPngIconName),
        path.resolve(process.cwd(), "apps", "desktop", "build", macConnectedPngIconName),
        path.resolve(mainModuleDir, "..", "..", "build", "PangeaVPN_connectedTemplate.png"),
        path.resolve(process.cwd(), "apps", "desktop", "build", "PangeaVPN_connectedTemplate.png"),
        path.resolve(mainModuleDir, "..", "..", "build", macConnectedIconName),
        path.resolve(mainModuleDir, "..", "..", "built", "pangeavpn_connected.icns"),
        path.resolve(process.cwd(), "apps", "desktop", "build", macConnectedIconName),
        path.resolve(process.cwd(), "apps", "desktop", "built", "pangeavpn_connected.icns"),
        path.resolve(mainModuleDir, "..", "..", "build", macConnectedIcoFallbackName),
        path.resolve(process.cwd(), "apps", "desktop", "build", macConnectedIcoFallbackName)
      ];

  return candidates.find((candidate) => fs.existsSync(candidate));
}
