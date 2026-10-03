import { app, ipcMain, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";
import { isSafeExternalUrl } from "./externalUrl";
import { shouldOfferUpdate } from "./updatePolicy";

export const LATEST_ROUTE = "/api/desktop/latest";
const FALLBACK_RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";
const CHECK_TIMEOUT_MS = 8000;
const MANUAL_CHECK_MIN_INTERVAL_MS = 60_000;
// The automatic check waits for ordinary app traffic to find a hub path, so
// it never starts the path cascade on its own; it looks again on this cadence.
const AUTO_CHECK_FIRST_DELAY_MS = 15_000;
const AUTO_CHECK_RETRY_MS = 60_000;

/** The hub as the updater sees it: sealed, anonymous, over the resolved path. */
export interface UpdateHub {
  ready: () => boolean;
  fetchLatest: (timeoutMs: number) => Promise<{ status: number; body: unknown }>;
}

interface LatestRelease {
  version: string;
  tagName: string;
  releaseUrl: string;
  releaseNotes: string;
  publishedAt: string;
  prerelease: boolean;
}

function isSafeReleaseUrl(url: string): boolean {
  return isSafeExternalUrl(url);
}

function isNonEmptyString(v: unknown): v is string {
  return typeof v === "string" && v.length > 0;
}

// Turns an untrusted hub reply into a LatestRelease, discarding anything
// that isn't shaped right rather than trusting the server's types.
export function toLatestRelease(data: unknown): LatestRelease | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Record<string, unknown>;
  if (!isNonEmptyString(d.version)) return null;
  const releaseUrl = isNonEmptyString(d.releaseUrl) && isSafeReleaseUrl(d.releaseUrl)
    ? d.releaseUrl
    : FALLBACK_RELEASE_URL;
  return {
    version: d.version,
    tagName: isNonEmptyString(d.tagName) ? d.tagName : d.version,
    releaseUrl,
    releaseNotes: typeof d.releaseNotes === "string" ? d.releaseNotes : "",
    publishedAt: typeof d.publishedAt === "string" ? d.publishedAt : "",
    prerelease: d.prerelease === true,
  };
}

let hub: UpdateHub | null = null;
let latestRelease: LatestRelease | null = null;
let checkDone = false;
let getMainWindow: (() => BrowserWindow | null) = () => null;
let autoCheckTimer: NodeJS.Timeout | null = null;
let manualCheckInFlight: Promise<void> | null = null;
let lastManualCheckAt = 0;

function isMacOnlyRelease(): boolean {
  return process.platform === "darwin";
}

async function fetchLatest(): Promise<LatestRelease | null> {
  if (!hub) return null;
  try {
    const { status, body } = await hub.fetchLatest(CHECK_TIMEOUT_MS);
    return status === 200 ? toLatestRelease(body) : null;
  } catch {
    return null;
  }
}

async function performCheck(): Promise<void> {
  const data = await fetchLatest();
  if (!data) return;
  checkDone = true;
  latestRelease = data;
  const win = getMainWindow();
  if (!win || win.isDestroyed()) return;
  if (shouldOfferUpdate(data, app.getVersion())) {
    win.webContents.send(IPC_CHANNELS.updateAvailable, {
      version: data.version,
      releaseNotes: data.releaseNotes,
      macOnly: isMacOnlyRelease(),
    });
  } else {
    win.webContents.send(IPC_CHANNELS.updateNotAvailable);
  }
}

function scheduleAutoCheck(delayMs: number): void {
  autoCheckTimer = setTimeout(() => {
    autoCheckTimer = null;
    void (async () => {
      if (!checkDone && hub?.ready()) await performCheck().catch(() => {});
      if (!checkDone) scheduleAutoCheck(AUTO_CHECK_RETRY_MS);
    })();
  }, delayMs);
  if (typeof autoCheckTimer.unref === "function") autoCheckTimer.unref();
}

// The renderer shows "update" or "you're on the latest" from `available`, so the release
// policy lives in one place.
function checkResult(): { version: string; releaseNotes: string; available: boolean } | null {
  if (!latestRelease) return null;
  return {
    version: latestRelease.version,
    releaseNotes: latestRelease.releaseNotes,
    available: shouldOfferUpdate(latestRelease, app.getVersion()),
  };
}

// `windowResolver` is called at send time (not captured once) so a closed
// and reopened main window still receives the update banner.
export function setupAutoUpdater(windowResolver: () => BrowserWindow | null, updateHub: UpdateHub): void {
  getMainWindow = windowResolver;
  hub = updateHub;

  ipcMain.handle(IPC_CHANNELS.checkForUpdates, async () => {
    if (manualCheckInFlight) {
      await manualCheckInFlight;
      return checkResult();
    }
    const now = Date.now();
    if (now - lastManualCheckAt < MANUAL_CHECK_MIN_INTERVAL_MS) {
      return checkResult();
    }
    lastManualCheckAt = now;
    manualCheckInFlight = performCheck().catch(() => {});
    await manualCheckInFlight;
    manualCheckInFlight = null;
    return checkResult();
  });

  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async () => {
    const url = latestRelease?.releaseUrl;
    await shell.openExternal(url && isSafeReleaseUrl(url) ? url : FALLBACK_RELEASE_URL);
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    // No in-app install; users update by downloading the release.
  });

  scheduleAutoCheck(AUTO_CHECK_FIRST_DELAY_MS);
}
