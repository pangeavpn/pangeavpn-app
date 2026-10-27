import crypto from "node:crypto";
import fsp from "node:fs/promises";
import path from "node:path";
import { app, ipcMain, net, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";
import { isSafeExternalUrl } from "./externalUrl";
import { expectedDigest, macPackageAsset } from "./macUpdatePackage";
import { shouldOfferUpdate } from "./updatePolicy";

export const LATEST_ROUTE = "/api/desktop/latest";
const FALLBACK_RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";
const CHECK_TIMEOUT_MS = 8000;
const MANUAL_CHECK_MIN_INTERVAL_MS = 60_000;
// The automatic check waits for ordinary app traffic to find a hub path, so
// it never starts the path cascade on its own; it looks again on this cadence.
const AUTO_CHECK_FIRST_DELAY_MS = 15_000;
const AUTO_CHECK_RETRY_MS = 60_000;
// A pkg is ~150 MB; anything far beyond that is not one of ours.
const MAX_PACKAGE_BYTES = 1024 * 1024 * 1024;
const MAX_CHECKSUMS_BYTES = 64 * 1024;
// Slow links are fine; a download that stops moving is not.
const DOWNLOAD_STALL_MS = 60_000;
// The install quits and reopens the app; one still running after this has
// failed somewhere, and the user may try again.
const MAC_INSTALL_GRACE_MS = 10 * 60_000;

/** The hub as the updater sees it: sealed, anonymous, over the resolved path. */
export interface UpdateHub {
  ready: () => boolean;
  fetchLatest: (timeoutMs: number) => Promise<{ status: number; body: unknown }>;
}

/** macOS: has the root daemon verify and install a downloaded release pkg. */
export type MacPackageInstaller = (pkgPath: string, sha256: string) => Promise<void>;

export type UpdateProgress = { phase: "downloading"; percent: number | null } | { phase: "installing" };

/** "installing": the pkg is going in and the app is about to be quit and reopened. */
export type UpdateDownloadResult = { mode: "installing" } | { mode: "opened"; installFailed?: boolean };

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
let installMacPackage: MacPackageInstaller | null = null;
let macInstallStartedAt = 0;

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

async function openReleasePage(): Promise<void> {
  const url = latestRelease?.releaseUrl;
  await shell.openExternal(url && isSafeReleaseUrl(url) ? url : FALLBACK_RELEASE_URL);
}

function sendProgress(progress: UpdateProgress): void {
  const win = getMainWindow();
  if (win && !win.isDestroyed()) win.webContents.send(IPC_CHANNELS.updateProgress, progress);
}

// Every read restarts the stall timer, so a slow download keeps going and a
// dead one gives up instead of hanging the update forever.
async function fetchWithStallTimeout(url: string): Promise<{ response: Response; touch: () => void; done: () => void }> {
  const controller = new AbortController();
  let timer = setTimeout(() => controller.abort(), DOWNLOAD_STALL_MS);
  const touch = () => {
    clearTimeout(timer);
    timer = setTimeout(() => controller.abort(), DOWNLOAD_STALL_MS);
  };
  const done = () => clearTimeout(timer);
  try {
    const response = await net.fetch(url, { signal: controller.signal, redirect: "follow", cache: "no-store" });
    return { response, touch, done };
  } catch (error) {
    done();
    throw error;
  }
}

async function fetchChecksums(url: string): Promise<string> {
  const { response, done } = await fetchWithStallTimeout(url);
  try {
    if (!response.ok) throw new Error(`checksums download failed (HTTP ${response.status})`);
    const text = await response.text();
    if (text.length > MAX_CHECKSUMS_BYTES) throw new Error("checksums file is too large");
    return text;
  } finally {
    done();
  }
}

async function downloadPackage(url: string, dest: string): Promise<string> {
  const file = await fsp.open(dest, "wx", 0o600);
  try {
    const { response, touch, done } = await fetchWithStallTimeout(url);
    const reader = response.body?.getReader();
    let complete = false;
    try {
      if (!response.ok || !reader) throw new Error(`download failed (HTTP ${response.status})`);
      const total = Number(response.headers.get("content-length")) || 0;
      if (total > MAX_PACKAGE_BYTES) throw new Error("download is too large");
      const hash = crypto.createHash("sha256");
      let received = 0;
      let lastPercent: number | null = -1;
      for (;;) {
        const { done: finished, value } = await reader.read();
        if (finished) break;
        touch();
        received += value.byteLength;
        if (received > MAX_PACKAGE_BYTES) throw new Error("download is too large");
        hash.update(value);
        await file.write(value);
        const percent = total > 0 ? Math.min(100, Math.floor((received * 100) / total)) : null;
        if (percent !== lastPercent) {
          lastPercent = percent;
          sendProgress({ phase: "downloading", percent });
        }
      }
      complete = true;
      return hash.digest("hex");
    } finally {
      done();
      if (!complete) await reader?.cancel().catch(() => {});
    }
  } finally {
    await file.close();
  }
}

// The daemon copies the pkg into a root-only directory before answering, so
// the download can go as soon as it does, whatever the outcome.
async function installMacUpdate(release: LatestRelease): Promise<void> {
  const asset = macPackageAsset(release.version, process.arch);
  if (!asset || !installMacPackage) throw new Error("no macOS installer for this release");
  const want = expectedDigest(await fetchChecksums(asset.checksumsUrl), asset.name);
  if (!want) throw new Error(`the release lists no checksum for ${asset.name}`);
  const dir = await fsp.mkdtemp(path.join(app.getPath("temp"), "pangeavpn-update-"));
  try {
    const pkgPath = path.join(dir, asset.name);
    sendProgress({ phase: "downloading", percent: 0 });
    const got = await downloadPackage(asset.url, pkgPath);
    if (got !== want) throw new Error("the download does not match the release checksum");
    sendProgress({ phase: "installing" });
    await installMacPackage(pkgPath, want);
  } finally {
    await fsp.rm(dir, { recursive: true, force: true }).catch(() => {});
  }
}

// `windowResolver` is called at send time (not captured once) so a closed
// and reopened main window still receives the update banner.
export function setupAutoUpdater(
  windowResolver: () => BrowserWindow | null,
  updateHub: UpdateHub,
  macInstaller?: MacPackageInstaller
): void {
  getMainWindow = windowResolver;
  hub = updateHub;
  installMacPackage = process.platform === "darwin" ? macInstaller ?? null : null;

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

  // macOS installs in place through the daemon; anything that goes wrong
  // falls back to the release page, so an update is never a dead end.
  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async (): Promise<UpdateDownloadResult> => {
    const release = latestRelease;
    if (installMacPackage && release && shouldOfferUpdate(release, app.getVersion())) {
      if (Date.now() - macInstallStartedAt < MAC_INSTALL_GRACE_MS) return { mode: "installing" };
      macInstallStartedAt = Date.now();
      try {
        await installMacUpdate(release);
        return { mode: "installing" };
      } catch (error) {
        macInstallStartedAt = 0;
        console.warn("in-app update failed:", error instanceof Error ? error.message : String(error));
        await openReleasePage();
        return { mode: "opened", installFailed: true };
      }
    }
    await openReleasePage();
    return { mode: "opened" };
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    // Nothing to do: macOS installs from downloadAppUpdate, everything else
    // updates from the release page.
  });

  scheduleAutoCheck(AUTO_CHECK_FIRST_DELAY_MS);
}
