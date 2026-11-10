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
// White logo template images (with @2x siblings) that macOS tints to suit the menu bar.
const macTemplateIconName = "PangeaVPNTemplate.png";
const macConnectedTemplateIconName = "PangeaVPN_connectedTemplate.png";
const macIcoFallbackName = "PangeaVPN.ico";
const macConnectedIcoFallbackName = "PangeaVPN_connected.ico";

export function getBundledDaemonPath(): string {
  const name = process.platform === "win32" ? "PangeaDaemon.exe" : "daemon";
  return path.join(process.resourcesPath, "daemon", name);
}

// On macOS the menu bar takes the template; Linux keeps the coloured PNG.
function macTemplateFirst(template: string, png: string): string[] {
  return process.platform === "darwin" ? [template, png] : [png, template];
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
      ? packagedNamePairs([...macTemplateFirst(macTemplateIconName, macPngIconName), macIconName, macIcoFallbackName])
      : [
          ...macTemplateFirst(macTemplateIconName, macPngIconName).map((name) =>
            path.resolve(mainModuleDir, "..", "..", "build", name)
          ),
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
          ...macTemplateFirst(macConnectedTemplateIconName, macConnectedPngIconName),
          macConnectedIconName,
          macConnectedIcoFallbackName
        ])
      : [
          ...macTemplateFirst(macConnectedTemplateIconName, macConnectedPngIconName).map((name) =>
            path.resolve(mainModuleDir, "..", "..", "build", name)
          ),
          path.resolve(mainModuleDir, "..", "..", "build", macConnectedIconName),
          path.resolve(mainModuleDir, "..", "..", "built", "pangeavpn_connected.icns"),
          path.resolve(mainModuleDir, "..", "..", "build", macConnectedIcoFallbackName)
        ])
  ];

  return firstExisting(candidates);
}

// The app bundle's own icon, which the Dock shows when nothing overrides it.
export function getMacAppIconPath(mainModuleDir: string): string | undefined {
  if (process.platform !== "darwin") return undefined;
  return firstExisting(
    app.isPackaged
      ? packagedNamePairs([macIconName])
      : [path.resolve(mainModuleDir, "..", "..", "build", macIconName)]
  );
}

// The iOS-style Dock icon for an accent, from build/dock.
export function getDockIconPath(mainModuleDir: string, accent: string): string | undefined {
  if (process.platform !== "darwin") return undefined;
  const name = `PangeaVPN-${accent}.png`;
  return firstExisting(
    app.isPackaged
      ? [path.join(process.resourcesPath, "build", "dock", name)]
      : [path.resolve(mainModuleDir, "..", "..", "build", "dock", name)]
  );
}
