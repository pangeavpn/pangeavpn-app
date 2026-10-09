// Parks the frameless popover next to the tray icon: pick the icon's display,
// read the panel edge off the work-area insets, hug that edge.

export interface AnchorRect {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface AnchorDisplay {
  bounds: AnchorRect;
  workArea: AnchorRect;
}

export interface AnchorPoint {
  x: number;
  y: number;
}

export type PanelEdge = "top" | "bottom" | "left" | "right";

export interface AnchorInput {
  displays: AnchorDisplay[];
  primary: AnchorDisplay;
  cursor: AnchorPoint | null;
  trayBounds: AnchorRect | null;
  size: { width: number; height: number };
  platform: NodeJS.Platform;
}

// Real tray bounds come from macOS/Windows; AppIndicator trays hand back zeros.
export function hasTrayBounds(rect: AnchorRect | null | undefined): rect is AnchorRect {
  return !!rect && rect.width > 0 && rect.height > 0;
}

function containsPoint(rect: AnchorRect, point: AnchorPoint): boolean {
  return (
    point.x >= rect.x &&
    point.x < rect.x + rect.width &&
    point.y >= rect.y &&
    point.y < rect.y + rect.height
  );
}

function centerOf(rect: AnchorRect): AnchorPoint {
  return { x: rect.x + rect.width / 2, y: rect.y + rect.height / 2 };
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(value, max));
}

// The icon beats the pointer: menu and keyboard paths have no pointer to go on.
export function pickDisplay(input: AnchorInput): AnchorDisplay {
  const displays = input.displays.length > 0 ? input.displays : [input.primary];
  const trayPoint = hasTrayBounds(input.trayBounds) ? centerOf(input.trayBounds) : null;

  for (const point of [trayPoint, input.cursor]) {
    if (!point) {
      continue;
    }
    const hit = displays.find((display) => containsPoint(display.bounds, point));
    if (hit) {
      return hit;
    }
  }
  return input.primary;
}

export function panelEdge(display: AnchorDisplay, platform: NodeJS.Platform): PanelEdge {
  // Insets on macOS follow the Dock, not the menu bar the status item lives in.
  if (platform === "darwin") {
    return "top";
  }

  const { bounds, workArea } = display;
  const insets: Record<PanelEdge, number> = {
    bottom: bounds.y + bounds.height - (workArea.y + workArea.height),
    top: workArea.y - bounds.y,
    left: workArea.x - bounds.x,
    right: bounds.x + bounds.width - (workArea.x + workArea.width)
  };

  // Widest strip wins; ties and nothing-reserved (auto-hidden panel, no struts)
  // fall back to the bottom.
  let edge: PanelEdge = "bottom";
  let widest = 0;
  for (const candidate of ["bottom", "top", "left", "right"] as PanelEdge[]) {
    if (insets[candidate] > widest) {
      widest = insets[candidate];
      edge = candidate;
    }
  }
  return edge;
}

export function anchorPosition(input: AnchorInput): AnchorPoint {
  const display = pickDisplay(input);
  const edge = panelEdge(display, input.platform);
  const margin = input.platform === "darwin" ? 8 : 0;
  const area = display.workArea;
  const { width, height } = input.size;
  const trayBounds = hasTrayBounds(input.trayBounds) ? input.trayBounds : null;
  // Only Linux slides along the panel to the icon; the others corner-anchor.
  const followTray = trayBounds && input.platform === "linux" ? trayBounds : null;

  let x: number;
  let y: number;

  if (edge === "top" || edge === "bottom") {
    x = followTray ? centerOf(followTray).x - width / 2 : area.x + area.width - width - margin;
    y = edge === "top" ? area.y + margin : area.y + area.height - height - margin;
  } else {
    x = edge === "left" ? area.x + margin : area.x + area.width - width - margin;
    y = followTray ? centerOf(followTray).y - height / 2 : area.y + area.height - height - margin;
  }

  return {
    x: Math.round(clamp(x, area.x + margin, area.x + area.width - width - margin)),
    y: Math.round(clamp(y, area.y + margin, area.y + area.height - height - margin))
  };
}

export function samePoint(a: AnchorPoint, b: AnchorPoint, tolerance = 1): boolean {
  return Math.abs(a.x - b.x) <= tolerance && Math.abs(a.y - b.y) <= tolerance;
}

// Reads the backend Electron was told to use, from the flag or Electron's env var.
export function requestedOzonePlatform(env: NodeJS.ProcessEnv, argv: readonly string[]): string | null {
  const flag =
    argv.find((arg) => arg.startsWith("--ozone-platform=")) ??
    argv.find((arg) => arg.startsWith("--ozone-platform-hint="));
  const value = (flag?.split("=")[1] ?? env.ELECTRON_OZONE_PLATFORM_HINT ?? "").trim().toLowerCase();
  return value === "" ? null : value;
}

// Wayland hands placement to the compositor: a client cannot put its own window
// next to the tray, so there the app has to be an ordinary window instead.
export function canAnchorWindow(
  env: NodeJS.ProcessEnv,
  platform: NodeJS.Platform,
  argv: readonly string[]
): boolean {
  if (platform !== "linux") {
    return true;
  }
  const requested = requestedOzonePlatform(env, argv);
  if (requested === "x11" || requested === "wayland") {
    return requested === "x11";
  }
  // "auto", and Electron's own default, both pick Wayland in a Wayland session.
  return !env.WAYLAND_DISPLAY && (env.XDG_SESSION_TYPE ?? "").toLowerCase() !== "wayland";
}
