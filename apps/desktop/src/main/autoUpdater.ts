import { app, ipcMain, net, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";

const HUB_LATEST_URL = "https://api.pangeavpn.org/api/desktop/latest";
const GITHUB_LATEST_URL = "https://api.github.com/repos/pangeavpn/pangeavpn-app/releases/latest";
const FALLBACK_RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";
const CHECK_TIMEOUT_MS = 5000;

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

function compareVersions(a: string, b: string): number {
  const av = a.replace(/^v/, "").split(".").map((n) => parseInt(n, 10) || 0);
  const bv = b.replace(/^v/, "").split(".").map((n) => parseInt(n, 10) || 0);
  const len = Math.max(av.length, bv.length);
  for (let i = 0; i < len; i++) {
    const x = av[i] ?? 0;
    const y = bv[i] ?? 0;
    if (x !== y) return x - y;
  }
  return 0;
}

async function fetchJson<T>(url: string): Promise<T | null> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), CHECK_TIMEOUT_MS);
  try {
    const resp = await net.fetch(url, { signal: controller.signal });
    if (!resp.ok) return null;
    return (await resp.json()) as T;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

async function fetchFromHub(): Promise<LatestRelease | null> {
  const data = await fetchJson<LatestRelease>(HUB_LATEST_URL);
  if (!data?.version) return null;
  return data;
}

async function fetchFromGitHub(): Promise<LatestRelease | null> {
  interface GhRelease {
    tag_name?: string;
    name?: string;
    body?: string;
    html_url?: string;
    published_at?: string;
  }
  const data = await fetchJson<GhRelease>(GITHUB_LATEST_URL);
  const tag = data?.tag_name;
  if (!tag) return null;
  return {
    version: tag.replace(/^v/, ""),
    tagName: tag,
    releaseUrl: data?.html_url || FALLBACK_RELEASE_URL,
    releaseNotes: data?.body || "",
    publishedAt: data?.published_at || "",
  };
}

let latestRelease: LatestRelease | null = null;
let checkAttempted = false;
let pendingMainWindow: BrowserWindow | null = null;
let fallbackTimer: NodeJS.Timeout | null = null;
let currentConnectionState = "";
let pendingHubTimer: NodeJS.Timeout | null = null;

function isMacOnlyRelease(): boolean {
  return process.platform === "darwin";
}

async function performCheck(via: "hub" | "github"): Promise<void> {
  if (checkAttempted) return;
  checkAttempted = true;
  if (fallbackTimer) {
    clearTimeout(fallbackTimer);
    fallbackTimer = null;
  }
  const data = via === "hub" ? await fetchFromHub() : await fetchFromGitHub();
  if (!data) {
    // If hub failed and we're connected, don't fall back — connected hub
    // failure is a real signal. If hub failed because we never connected,
    // the timeout path will run github separately. Reset so a manual check
    // can try again.
    checkAttempted = false;
    return;
  }
  latestRelease = data;
  if (!pendingMainWindow || pendingMainWindow.isDestroyed()) return;
  if (compareVersions(data.version, app.getVersion()) > 0) {
    pendingMainWindow.webContents.send(IPC_CHANNELS.updateAvailable, {
      version: data.version,
      releaseNotes: data.releaseNotes,
      macOnly: isMacOnlyRelease(),
    });
  } else {
    pendingMainWindow.webContents.send(IPC_CHANNELS.updateNotAvailable);
  }
}

export function setupAutoUpdater(mainWindow: BrowserWindow): void {
  pendingMainWindow = mainWindow;

  ipcMain.handle(IPC_CHANNELS.checkForUpdates, async () => {
    // Manual checks always go via GitHub so the user can refresh on demand
    // without leaking pangeavpn.org traffic off-tunnel.
    checkAttempted = false;
    await performCheck("github");
    if (!latestRelease) return null;
    return { version: latestRelease.version, releaseNotes: latestRelease.releaseNotes };
  });

  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async () => {
    await shell.openExternal(latestRelease?.releaseUrl || FALLBACK_RELEASE_URL);
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    // No in-app install; users update by downloading the release.
  });

  // Fallback: if VPN never comes up within CONNECT_WAIT_MS, ask GitHub
  // instead. Different domain, no pangeavpn.org call from a clear network.
  fallbackTimer = setTimeout(() => {
    void performCheck("github");
  }, CONNECT_WAIT_MS);
  if (typeof fallbackTimer.unref === "function") fallbackTimer.unref();
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
    void performCheck("hub");
  }, 1500);
  if (typeof pendingHubTimer.unref === "function") pendingHubTimer.unref();
}
