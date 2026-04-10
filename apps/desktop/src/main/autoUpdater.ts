import { autoUpdater, type UpdateInfo } from "electron-updater";
import { app, ipcMain, shell, type BrowserWindow } from "electron";
import { IPC_CHANNELS } from "../shared/ipc";

const isMac = process.platform === "darwin";
const RELEASE_URL = "https://github.com/pangeavpn/pangeavpn-app/releases/latest";

export function setupAutoUpdater(mainWindow: BrowserWindow): void {
  autoUpdater.autoDownload = false;
  autoUpdater.autoInstallOnAppQuit = !isMac;

  autoUpdater.on("update-available", (info: UpdateInfo) => {
    mainWindow.webContents.send(IPC_CHANNELS.updateAvailable, {
      version: info.version,
      releaseNotes: info.releaseNotes,
      macOnly: isMac,
    });
  });

  autoUpdater.on("update-not-available", () => {
    mainWindow.webContents.send(IPC_CHANNELS.updateNotAvailable);
  });

  autoUpdater.on("error", (err: Error) => {
    mainWindow.webContents.send(IPC_CHANNELS.updateError, err.message);
  });

  if (!isMac) {
    autoUpdater.on("download-progress", (progress) => {
      mainWindow.webContents.send(IPC_CHANNELS.updateDownloadProgress, progress.percent);
    });

    autoUpdater.on("update-downloaded", () => {
      mainWindow.webContents.send(IPC_CHANNELS.updateDownloaded);
    });
  }

  ipcMain.handle(IPC_CHANNELS.checkForUpdates, async () => {
    try {
      const result = await autoUpdater.checkForUpdates();
      return result?.updateInfo ?? null;
    } catch {
      return null;
    }
  });

  ipcMain.handle(IPC_CHANNELS.downloadAppUpdate, async () => {
    if (isMac) {
      await shell.openExternal(RELEASE_URL);
    } else {
      await autoUpdater.downloadUpdate();
    }
  });

  ipcMain.handle(IPC_CHANNELS.installUpdate, () => {
    if (!isMac) {
      autoUpdater.quitAndInstall();
    }
  });

  // Check for updates 3 seconds after startup
  setTimeout(() => {
    autoUpdater.checkForUpdates().catch(() => {});
  }, 3000);
}
