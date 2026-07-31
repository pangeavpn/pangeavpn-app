// One-shot "we're still in the tray" hint. The window is a frameless popover
// with no taskbar entry, so a first-time user reads a hide as a quit.

export interface TrayHintConditions {
  alreadyShown: boolean;
  fromTrayClick: boolean;
  supported: boolean;
}

export type TrayHintBodyKey = "notify.trayBody" | "notify.menuBarBody";

export function shouldShowTrayHint(conditions: TrayHintConditions): boolean {
  const { alreadyShown, fromTrayClick, supported } = conditions;
  return supported && !alreadyShown && !fromTrayClick;
}

export function trayHintBodyKey(platform: NodeJS.Platform): TrayHintBodyKey {
  return platform === "darwin" ? "notify.menuBarBody" : "notify.trayBody";
}
