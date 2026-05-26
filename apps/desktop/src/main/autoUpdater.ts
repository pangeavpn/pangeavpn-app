import { app, ipcMain, net, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";

const HUB_LATEST_URL = "https://api.pangeavpn.org/api/desktop/latest";
const FALLBACK_RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";
const CHECK_TIMEOUT_MS = 5000;

interface HubLatest {
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

async function fetchLatest(): Promise<HubLatest | null> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), CHECK_TIMEOUT_MS);
  try {
    const resp = await net.fetch(HUB_LATEST_URL, { signal: controller.signal });
    if (!resp.ok) return null;
    const data = (await resp.json()) as HubLatest;
    if (!data?.version) return null;
    return data;
  } catch {
    return null;
  } finally {
    clearTimeout(timer);
  }
}

let latestRelease: HubLatest | null = null;

export function setupAutoUpdater(mainWindow: BrowserWindow): void {
  async function checkOnce(): Promise<HubLatest | null> {
    const data = await fetchLatest();
    if (!data) return null;
    latestRelease = data;
    if (compareVersions(data.version, app.getVersion()) > 0) {
      mainWindow.webContents.send(IPC_CHANNELS.updateAvailable, {
        version: data.version,
        releaseNotes: data.releaseNotes,
        macOnly: true,
      });
    } else {
      mainWindow.webContents.send(IPC_CHANNELS.updateNotAvailable);
    }
    return data;
  }

  ipcMain.handle(IPC_CHANNELS.checkForUpdates, async () => {
    const data = await checkOnce();
    if (!data) return null;
    return { version: data.version, releaseNotes: data.releaseNotes };
  });

  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async () => {
    await shell.openExternal(latestRelease?.releaseUrl || FALLBACK_RELEASE_URL);
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    // No in-app install; users update by downloading the release.
  });

  setTimeout(() => {
    checkOnce().catch(() => {});
  }, 3000);
}
