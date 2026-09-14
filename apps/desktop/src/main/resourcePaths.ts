import fs from "node:fs";
import path from "node:path";
import { app } from "electron";

const windowsIconName = "PangeaVPN.ico";
const windowsConnectedIconName = "PangeaVPN_connected.ico";
const linuxPngIconName = "PangeaVPN_linux.png";
const linuxConnectedPngIconName = "PangeaVPN_connected_linux.png";
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

function firstExisting(candidates: string[]): string | undefined {
  return candidates.find((candidate) => fs.existsSync(candidate));
}

function windowsIconCandidates(mainModuleDir: string, iconName: string): string[] {
  return app.isPackaged
    ? [path.join(process.resourcesPath, "build", iconName), path.join(process.resourcesPath, iconName)]
    : [path.resolve(mainModuleDir, "..", "..", "build", iconName)];
}

function packagedNamePairs(names: string[]): string[] {
  return names.flatMap((name) => [
    path.join(process.resourcesPath, "build", name),
    path.join(process.resourcesPath, name)
  ]);
}

export function getWindowsAppIconPath(mainModuleDir: string): string | undefined {
  if (process.platform !== "win32") {
    return undefined;
  }

  return firstExisting(windowsIconCandidates(mainModuleDir, windowsIconName));
}

export function getTrayIconPath(mainModuleDir: string): string | undefined {
  if (process.platform === "win32") {
    return getWindowsAppIconPath(mainModuleDir);
  }
  if (process.platform !== "darwin" && process.platform !== "linux") {
    return undefined;
  }

  const linuxPrefix = process.platform === "linux" ? windowsIconCandidates(mainModuleDir, linuxPngIconName) : [];

  const candidates = [
    ...linuxPrefix,
    ...(app.isPackaged
      ? packagedNamePairs([macPngIconName, "PangeaVPNTemplate.png", macIconName, macIcoFallbackName])
      : [
          path.resolve(mainModuleDir, "..", "..", "build", macPngIconName),
          path.resolve(mainModuleDir, "..", "..", "build", "PangeaVPNTemplate.png"),
          path.resolve(mainModuleDir, "..", "..", "build", macIconName),
          path.resolve(mainModuleDir, "..", "..", "built", "pangeavpn.icns"),
          path.resolve(mainModuleDir, "..", "..", "build", macIcoFallbackName)
        ])
  ];

  return firstExisting(candidates);
}

export function getConnectedTrayIconPath(mainModuleDir: string): string | undefined {
  if (process.platform === "win32") {
    return firstExisting(windowsIconCandidates(mainModuleDir, windowsConnectedIconName));
  }
  if (process.platform !== "darwin" && process.platform !== "linux") {
    return undefined;
  }

  const linuxPrefix =
    process.platform === "linux" ? windowsIconCandidates(mainModuleDir, linuxConnectedPngIconName) : [];

  const candidates = [
    ...linuxPrefix,
    ...(app.isPackaged
      ? packagedNamePairs([
          macConnectedPngIconName,
          "PangeaVPN_connectedTemplate.png",
          macConnectedIconName,
          macConnectedIcoFallbackName
        ])
      : [
          path.resolve(mainModuleDir, "..", "..", "build", macConnectedPngIconName),
          path.resolve(mainModuleDir, "..", "..", "build", "PangeaVPN_connectedTemplate.png"),
          path.resolve(mainModuleDir, "..", "..", "build", macConnectedIconName),
          path.resolve(mainModuleDir, "..", "..", "built", "pangeavpn_connected.icns"),
          path.resolve(mainModuleDir, "..", "..", "build", macConnectedIcoFallbackName)
        ])
  ];

  return firstExisting(candidates);
}
