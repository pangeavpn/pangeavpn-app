import { app, ipcMain, net, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";
import { isSafeExternalUrl } from "./externalUrl";

const HUB_LATEST_URL = "https://api.pangeavpn.org/api/desktop/latest";
const GITHUB_LATEST_URL = "https://api.github.com/repos/pangeavpn/pangeavpn-app/releases/latest";
const FALLBACK_RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";
const CHECK_TIMEOUT_MS = 5000;
const MANUAL_CHECK_MIN_INTERVAL_MS = 60_000;

// How long we'll wait for the VPN to come up before falling back to the
// stealthier GitHub-hosted API. Keeps the hub-first preference without
// stranding users who never connect.
const CONNECT_WAIT_MS = 5 * 60 * 1000;

interface LatestRelease {
  version: string;
  tagName: string;
  releaseUrl: string;
  releaseNotes: string;
  publishedAt: string;
}

function isSafeReleaseUrl(url: string): boolean {
  return isSafeExternalUrl(url);
}

function parseVersion(v: string): { core: number[]; prerelease: string | null } {
  const [core, ...pre] = v.replace(/^v/, "").split("-");
  return {
    core: core.split(".").map((n) => parseInt(n, 10) || 0),
    prerelease: pre.length > 0 ? pre.join("-") : null,
  };
}

// A prerelease (e.g. "0.6.0-rc.1") always sorts below its final release.
function compareVersions(a: string, b: string): number {
  const av = parseVersion(a);
  const bv = parseVersion(b);
  const len = Math.max(av.core.length, bv.core.length);
  for (let i = 0; i < len; i++) {
    const x = av.core[i] ?? 0;
    const y = bv.core[i] ?? 0;
    if (x !== y) return x - y;
  }
  if (av.prerelease === bv.prerelease) return 0;
  if (av.prerelease === null) return 1;
  if (bv.prerelease === null) return -1;
  return av.prerelease < bv.prerelease ? -1 : 1;
}

async function fetchJson(url: string): Promise<unknown> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), CHECK_TIMEOUT_MS);
  try {
    const resp = await net.fetch(url, { signal: controller.signal });
    if (!resp.ok) return null;
    return await resp.json();
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

function isNonEmptyString(v: unknown): v is string {
  return typeof v === "string" && v.length > 0;
}

// Turns an untrusted hub reply into a LatestRelease, discarding anything
// that isn't shaped right rather than trusting the server's types.
function toLatestReleaseFromHub(data: unknown): LatestRelease | null {
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
  };
}

function toLatestReleaseFromGitHub(data: unknown): LatestRelease | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Record<string, unknown>;
  if (!isNonEmptyString(d.tag_name)) return null;
  const tag = d.tag_name;
  const releaseUrl = isNonEmptyString(d.html_url) && isSafeReleaseUrl(d.html_url)
    ? d.html_url
    : FALLBACK_RELEASE_URL;
  return {
    version: tag.replace(/^v/, ""),
    tagName: tag,
    releaseUrl,
    releaseNotes: typeof d.body === "string" ? d.body : "",
    publishedAt: typeof d.published_at === "string" ? d.published_at : "",
  };
}

async function fetchFromHub(): Promise<LatestRelease | null> {
  return toLatestReleaseFromHub(await fetchJson(HUB_LATEST_URL));
}

async function fetchFromGitHub(): Promise<LatestRelease | null> {
  return toLatestReleaseFromGitHub(await fetchJson(GITHUB_LATEST_URL));
}

let latestRelease: LatestRelease | null = null;
let checkAttempted = false;
let getMainWindow: (() => BrowserWindow | null) = () => null;
let fallbackTimer: NodeJS.Timeout | null = null;
let currentConnectionState = "";
let pendingHubTimer: NodeJS.Timeout | null = null;
let manualCheckInFlight: Promise<void> | null = null;
let lastManualCheckAt = 0;

function isMacOnlyRelease(): boolean {
  return process.platform === "darwin";
}

function armFallbackTimer(): void {
  fallbackTimer = setTimeout(() => {
    void performCheck("github").catch(() => {});
  }, CONNECT_WAIT_MS);
  if (typeof fallbackTimer.unref === "function") fallbackTimer.unref();
}

async function performCheck(via: "hub" | "github"): Promise<void> {
  if (checkAttempted) return;
  checkAttempted = true;
  const hadFallbackTimer = fallbackTimer !== null;
  if (fallbackTimer) {
    clearTimeout(fallbackTimer);
    fallbackTimer = null;
  }
  const data = via === "hub" ? await fetchFromHub() : await fetchFromGitHub();
  if (!data) {
    // Re-allow future attempts, and re-arm the fallback we just consumed
    // so a transient failure doesn't end update checks for the session.
    checkAttempted = false;
    if (hadFallbackTimer) armFallbackTimer();
    return;
  }
  latestRelease = data;
  const win = getMainWindow();
  if (!win || win.isDestroyed()) return;
  if (compareVersions(data.version, app.getVersion()) > 0) {
    win.webContents.send(IPC_CHANNELS.updateAvailable, {
      version: data.version,
      releaseNotes: data.releaseNotes,
      macOnly: isMacOnlyRelease(),
    });
  } else {
    win.webContents.send(IPC_CHANNELS.updateNotAvailable);
  }
}

// `windowResolver` is called at send time (not captured once) so a closed
// and reopened main window still receives the update banner.
export function setupAutoUpdater(windowResolver: () => BrowserWindow | null): void {
  getMainWindow = windowResolver;

  ipcMain.handle(IPC_CHANNELS.checkForUpdates, async () => {
    if (manualCheckInFlight) {
      await manualCheckInFlight;
      return latestRelease ? { version: latestRelease.version, releaseNotes: latestRelease.releaseNotes } : null;
    }
    const now = Date.now();
    if (now - lastManualCheckAt < MANUAL_CHECK_MIN_INTERVAL_MS) {
      return latestRelease ? { version: latestRelease.version, releaseNotes: latestRelease.releaseNotes } : null;
    }
    lastManualCheckAt = now;
    // Manual checks always go via GitHub so the user can refresh on demand
    // without leaking pangeavpn.org traffic off-tunnel.
    checkAttempted = false;
    manualCheckInFlight = performCheck("github").catch(() => {});
    await manualCheckInFlight;
    manualCheckInFlight = null;
    if (!latestRelease) return null;
    return { version: latestRelease.version, releaseNotes: latestRelease.releaseNotes };
  });

  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async () => {
    const url = latestRelease?.releaseUrl;
    await shell.openExternal(url && isSafeReleaseUrl(url) ? url : FALLBACK_RELEASE_URL);
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    // No in-app install; users update by downloading the release.
  });

  // Fallback: if VPN never comes up within CONNECT_WAIT_MS, ask GitHub
  // instead. Different domain, no pangeavpn.org call from a clear network.
  armFallbackTimer();
}

// Called from main.ts whenever the tray status refresh observes a state
// transition. We only run the hub-side check once per app session, and only
// when the tunnel is up so the request rides through it.
export function notifyConnectionStateChange(state: string): void {
  currentConnectionState = state;
  if (state !== "CONNECTED") {
    // User dropped before the delayed hub check fired — cancel it so we
    // don't leak api.pangeavpn.org traffic onto a clear network.
    if (pendingHubTimer) {
      clearTimeout(pendingHubTimer);
      pendingHubTimer = null;
    }
    return;
  }
  if (checkAttempted || pendingHubTimer) return;
  // Tiny delay so the tunnel routes settle before we send the first request.
  pendingHubTimer = setTimeout(() => {
    pendingHubTimer = null;
    if (currentConnectionState !== "CONNECTED") return;
    void performCheck("hub").catch(() => {});
  }, 1500);
  if (typeof pendingHubTimer.unref === "function") pendingHubTimer.unref();
}
