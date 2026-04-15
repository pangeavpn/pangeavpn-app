import { Menu, Tray, app, BrowserWindow, ipcMain, nativeImage, type NativeImage } from "electron";
import path from "node:path";
import type { OkResponse, Profile, StatusResponse } from "@pangeavpn/shared-types";
import { DaemonClient } from "./daemonClient";
import { DaemonProcessManager } from "./daemonProcess";
import { readDaemonTokens } from "./platformPaths";
import { getConnectedTrayIconPath, getTrayIconPath, getWindowsAppIconPath } from "./resourcePaths";
import { IPC_CHANNELS } from "../shared/ipc";
import * as auth from "./auth";
import { PangeaApiClient, AuthError } from "./pangeaApiClient";
import { setupAutoUpdater } from "./autoUpdater";

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let isQuitting = false;
let trayStatusState: StatusResponse["state"] = "DISCONNECTED";
let trayStatusDetail = "idle";
let trayActionInProgress = false;
let trayStatusRefreshInProgress = false;
let trayStatusTimer: NodeJS.Timeout | null = null;
let lastConnectedProfileId: string | null = null;
let trayDefaultImage: NativeImage | null = null;
let trayConnectedImage: NativeImage | null = null;
let lastDaemonRestartAttemptAtMs = 0;
let setWidth = 610;
let setHeight = 440;
const daemonRestartBackoffMs = 5000;

const daemonClient = new DaemonClient("http://127.0.0.1:8787", readDaemonTokens);
const daemonProcess = new DaemonProcessManager(daemonClient);
const pangeaApiClient = new PangeaApiClient();
let managedProfileId: string | null = null;
let lastServerId: string | null = null;

function getTaskbarPosition(): { x: number; y: number } {
  const { screen } = require("electron") as typeof import("electron");
  const display = screen.getPrimaryDisplay();
  const { width: screenW, height: screenH } = display.workAreaSize;
  const { x: workX, y: workY } = display.workArea;
  const winW = setWidth;
  const winH = setHeight;

  if (process.platform === "darwin") {
    // macOS: menu bar at top, anchor top-right
    return { x: workX + screenW - winW - 8, y: workY + 8 };
  }
  // Windows/Linux: flush to bottom-right of work area
  return { x: workX + screenW - winW, y: workY + screenH - winH };
}

function createWindow(): void {
  const windowIconPath = getWindowsAppIconPath(__dirname);
  const pos = getTaskbarPosition();
  mainWindow = new BrowserWindow({
    width: setWidth,
    height: setHeight,
    x: pos.x,
    y: pos.y,
    frame: false,
    resizable: false,
    skipTaskbar: true,
    alwaysOnTop: true,
    ...(windowIconPath ? { icon: windowIconPath } : {}),
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      preload: path.join(__dirname, "preload.js")
    }
  });

  mainWindow.loadFile(path.join(__dirname, "../renderer/index.html"));

  mainWindow.on("close", (event) => {
    if (isQuitting || !tray) {
      return;
    }
    event.preventDefault();
    hideMainWindow();
  });

  mainWindow.on("blur", () => {
    if (isQuitting) return;
    // Wait for any show animation to finish, then hide.
    const checkAndHide = () => {
      if (!isQuitting && mainWindow?.isVisible() && !hiding) {
        hideMainWindow();
      }
    };
    if (showing) {
      // Poll until show animation is done.
      const poll = setInterval(() => {
        if (!showing) {
          clearInterval(poll);
          checkAndHide();
        }
      }, 30);
      setTimeout(() => clearInterval(poll), 500); // safety
    } else {
      setTimeout(checkAndHide, 30);
    }
  });

  mainWindow.webContents.on("preload-error", (_event, preloadPath, error) => {
    console.error(`preload failed (${preloadPath}):`, error);
  });

  mainWindow.on("show", () => {
    updateTrayMenu();
  });

  mainWindow.on("hide", () => {
    updateTrayMenu();
  });

  mainWindow.on("closed", () => {
    mainWindow = null;
    updateTrayMenu();
  });
}

let showing = false;
let hiding = false;

function showMainWindow(): void {
  if (!mainWindow) {
    createWindow();
    return;
  }
  if (showing) return;
  hiding = false;
  showing = true;

  const pos = getTaskbarPosition();
  const useSlide = process.platform !== "linux";
  const slideOffset = process.platform === "darwin" ? -20 : 20;
  const startY = useSlide ? pos.y + slideOffset : pos.y;

  mainWindow.setOpacity(0);
  mainWindow.setBounds({ x: pos.x, y: startY, width: setWidth, height: setHeight });

  if (mainWindow.isMinimized()) {
    mainWindow.restore();
  }
  if (!mainWindow.isVisible()) {
    mainWindow.show();
  }
  mainWindow.focus();

  const duration = 180;
  const steps = 12;
  const interval = duration / steps;
  let step = 0;

  const timer = setInterval(() => {
    step++;
    const t = step / steps;
    const ease = 1 - Math.pow(1 - t, 3);
    mainWindow?.setOpacity(ease);
    if (useSlide) {
      mainWindow?.setPosition(pos.x, Math.round(startY + (pos.y - startY) * ease));
    }

    if (step >= steps) {
      clearInterval(timer);
      mainWindow?.setOpacity(1);
      mainWindow?.setBounds({ x: pos.x, y: pos.y, width: setWidth, height: setHeight });
      showing = false;
    }
  }, interval);

  updateTrayMenu();
}

function hideMainWindow(): void {
  if (!mainWindow || !mainWindow.isVisible() || hiding) {
    return;
  }
  hiding = true;
  showing = false;

  const [startX, startY] = mainWindow.getPosition();
  const useSlide = process.platform !== "linux";
  const slideOffset = process.platform === "darwin" ? -20 : 20;
  const endY = startY + slideOffset;
  const duration = 150;
  const steps = 10;
  const interval = duration / steps;
  let step = 0;

  const timer = setInterval(() => {
    step++;
    const t = step / steps;
    const ease = t * t;
    mainWindow?.setOpacity(1 - ease);
    if (useSlide) {
      mainWindow?.setPosition(startX, Math.round(startY + (endY - startY) * ease));
    }

    if (step >= steps) {
      clearInterval(timer);
      mainWindow?.hide();
      mainWindow?.setOpacity(1);
      hiding = false;
      updateTrayMenu();
    }
  }, interval);
}

function toggleMainWindowVisibility(): void {
  if (!mainWindow || !mainWindow.isVisible()) {
    showMainWindow();
    return;
  }
  hideMainWindow();
}

function createTray(): void {
  if (tray || (process.platform !== "win32" && process.platform !== "darwin" && process.platform !== "linux")) {
    return;
  }

  const trayIconPath = getTrayIconPath(__dirname);
  if (!trayIconPath) {
    console.warn("tray icon not found; skipping tray setup");
    return;
  }

  trayDefaultImage = loadTrayImage(trayIconPath);
  if (!trayDefaultImage) {
    console.warn(`failed loading tray icon: ${trayIconPath}`);
    return;
  }
  const connectedIconPath = getConnectedTrayIconPath(__dirname);
  trayConnectedImage = connectedIconPath ? loadTrayImage(connectedIconPath) : null;
  if (connectedIconPath && !trayConnectedImage) {
    console.warn(`failed loading connected tray icon: ${connectedIconPath}`);
  }

  tray = new Tray(trayDefaultImage);
  tray.setToolTip("PangeaVPN");

  // On macOS: left-click opens the window directly (no context menu).
  // On Windows/Linux: left-click also toggles the window.
  tray.on("click", () => {
    toggleMainWindowVisibility();
  });

  startTrayStatusPolling();
  updateTrayMenu();
}

function loadTrayImage(iconPath: string): NativeImage | null {
  let icon = nativeImage.createFromPath(iconPath);
  if (icon.isEmpty()) {
    return null;
  }
  if (process.platform === "darwin") {
    icon = icon.resize({ height: 18 });
    const lower = iconPath.toLowerCase();
    const looksLikeTemplate = lower.includes("template");
    icon.setTemplateImage(looksLikeTemplate);
  } else if (process.platform === "linux") {
    icon = icon.resize({ height: 22 });
  }
  return icon;
}

function updateTrayImage(): void {
  if (!tray || !trayDefaultImage) {
    return;
  }

  if (trayStatusState === "CONNECTED" && trayConnectedImage) {
    tray.setImage(trayConnectedImage);
    return;
  }
  tray.setImage(trayDefaultImage);
}

function updateTrayMenu(): void {
  if (!tray) {
    return;
  }

  updateTrayImage();

  const stateLabel = trayStatusState;
  tray.setToolTip(`PangeaVPN (${stateLabel})`);

  // On macOS, don't set a context menu — clicking the icon opens the window directly.
  // Setting a context menu on macOS would intercept left-clicks and show the menu instead.
  if (process.platform === "darwin") {
    return;
  }

  const detailLabel = trayStatusDetail.trim() || "-";
  const canConnect = !trayActionInProgress && (trayStatusState === "DISCONNECTED" || trayStatusState === "ERROR");
  const canDisconnect = !trayActionInProgress && (trayStatusState === "CONNECTED" || trayStatusState === "CONNECTING");
  const windowVisible = Boolean(mainWindow && mainWindow.isVisible());
  tray.setContextMenu(
    Menu.buildFromTemplate([
      {
        label: `Status: ${stateLabel}`,
        enabled: false
      },
      {
        label: `Detail: ${detailLabel}`,
        enabled: false
      },
      { type: "separator" },
      {
        label: "Connect",
        enabled: canConnect,
        click: () => {
          void connectFromTray();
        }
      },
      {
        label: "Disconnect",
        enabled: canDisconnect,
        click: () => {
          void disconnectFromTray();
        }
      },
      {
        type: "separator"
      },
      {
        label: windowVisible ? "Hide PangeaVPN" : "Show PangeaVPN",
        click: () => {
          if (windowVisible) {
            hideMainWindow();
            return;
          }
          showMainWindow();
        }
      },
      { type: "separator" },
      {
        label: "Quit",
        click: () => {
          isQuitting = true;
          app.quit();
        }
      }
    ])
  );
}

function startTrayStatusPolling(): void {
  if (trayStatusTimer) {
    return;
  }
  void refreshTrayStatus();
  trayStatusTimer = setInterval(() => {
    void refreshTrayStatus();
  }, 4000);
}

function stopTrayStatusPolling(): void {
  if (!trayStatusTimer) {
    return;
  }
  clearInterval(trayStatusTimer);
  trayStatusTimer = null;
}

async function refreshTrayStatus(): Promise<void> {
  if (!tray || trayStatusRefreshInProgress) {
    return;
  }

  trayStatusRefreshInProgress = true;
  try {
    const status = await withDaemonRestartOnUnavailable(() => daemonClient.getStatus(), "tray status", { allowRestart: false });
    trayStatusState = status.state;
    trayStatusDetail = status.detail;
  } catch {
    trayStatusState = "ERROR";
    trayStatusDetail = "daemon unavailable";
  } finally {
    trayStatusRefreshInProgress = false;
    updateTrayMenu();
  }
}

async function connectFromTray(): Promise<void> {
  if (trayActionInProgress) {
    return;
  }

  trayActionInProgress = true;
  updateTrayMenu();
  try {
    // Try to reconnect to existing profile first (no network roundtrip)
    const profileId = lastConnectedProfileId ?? managedProfileId;
    if (profileId) {
      const config = await withDaemonRestartOnUnavailable(() => daemonClient.getConfig(), "tray config", { allowRestart: false });
      if (config.profiles.some((p) => p.id === profileId)) {
        const result = await connectWithRecovery(profileId);
        if (result.ok) {
          lastConnectedProfileId = profileId;
          return;
        }
      }
    }

    // No existing profile — provision a new one
    const serverId = await resolveTrayServerId();
    if (!serverId) {
      trayStatusState = "ERROR";
      trayStatusDetail = "no server available";
      return;
    }

    const result = await provisionAndConnect(serverId);
    if (!result.ok) {
      trayStatusState = "ERROR";
      trayStatusDetail = "connect request failed";
    }
  } catch (error) {
    console.warn("tray connect failed", error);
    trayStatusState = "ERROR";
    trayStatusDetail = "connect failed";
  } finally {
    trayActionInProgress = false;
    await refreshTrayStatus();
  }
}

async function disconnectFromTray(): Promise<void> {
  if (trayActionInProgress) {
    return;
  }

  trayActionInProgress = true;
  updateTrayMenu();
  try {
    const result = await withDaemonRestartOnUnavailable(() => daemonClient.disconnect(), "tray disconnect");
    if (!result.ok) {
      trayStatusState = "ERROR";
      trayStatusDetail = "disconnect request failed";
    }
  } catch (error) {
    console.warn("tray disconnect failed", error);
    trayStatusState = "ERROR";
    trayStatusDetail = "disconnect failed";
  } finally {
    trayActionInProgress = false;
    await refreshTrayStatus();
  }
}

async function resolveTrayServerId(): Promise<string | null> {
  if (lastServerId) {
    return lastServerId;
  }

  // Fall back to first available server
  try {
    const servers = await pangeaApiClient.getServers();
    if (servers.length > 0) {
      return servers[0].id;
    }
  } catch {
    // no servers available
  }

  return null;
}

async function provisionAndConnect(serverId: string): Promise<import("@pangeavpn/shared-types").OkResponse> {
  const profile = await pangeaApiClient.provision(serverId);

  const config = await withDaemonRestartOnUnavailable(
    () => daemonClient.getConfig(),
    "provision-config",
    { allowRestart: false }
  );

  let profiles = config.profiles;
  if (managedProfileId) {
    profiles = profiles.filter((p) => p.id !== managedProfileId);
  }
  profiles = profiles.filter((p) => p.id !== profile.id);
  profiles.push(profile);
  managedProfileId = profile.id;
  lastServerId = serverId;

  await withDaemonRestartOnUnavailable(() => daemonClient.setConfig(profiles), "provision-setConfig");

  const result = await connectWithRecovery(profile.id);
  if (result.ok) {
    lastConnectedProfileId = profile.id;
  }
  return result;
}

const FRIENDLY_ADJECTIVES = [
  "Swift", "Bold", "Bright", "Calm", "Cool", "Dark", "Fast", "Free",
  "Grand", "Iron", "Kind", "Light", "Neat", "Nord", "Open", "Pale",
  "Pure", "Rare", "Rich", "Safe", "Slim", "Star", "True", "Wild"
];
const FRIENDLY_NOUNS = [
  "Bear", "Buck", "Crow", "Deer", "Dove", "Eagle", "Elk", "Falcon",
  "Fox", "Hawk", "Jay", "Kite", "Lion", "Lynx", "Moose", "Owl",
  "Puma", "Raven", "Stag", "Swan", "Tiger", "Wolf", "Wren"
];

function generateFriendlyName(): string {
  const adj = FRIENDLY_ADJECTIVES[Math.floor(Math.random() * FRIENDLY_ADJECTIVES.length)];
  const noun = FRIENDLY_NOUNS[Math.floor(Math.random() * FRIENDLY_NOUNS.length)];
  return `${adj} ${noun}`;
}

function registerIpcHandlers(): void {
  ipcMain.handle(IPC_CHANNELS.getStatus, async () =>
    withDaemonRestartOnUnavailable(() => daemonClient.getStatus(), "status", { allowRestart: false })
  );
  ipcMain.handle(IPC_CHANNELS.connect, async (_event, profileId: string) => {
    const result = await connectWithRecovery(profileId);
    if (result.ok) {
      lastConnectedProfileId = profileId;
    }
    void refreshTrayStatus();
    return result;
  });
  ipcMain.handle(IPC_CHANNELS.disconnect, async () => {
    const result = await withDaemonRestartOnUnavailable(() => daemonClient.disconnect(), "disconnect");
    void refreshTrayStatus();
    return result;
  });
  ipcMain.handle(IPC_CHANNELS.getLogs, async (_event, since?: number) =>
    withDaemonRestartOnUnavailable(() => daemonClient.getLogs(since), "logs", { allowRestart: false })
  );
  ipcMain.handle(IPC_CHANNELS.getConfig, async () =>
    withDaemonRestartOnUnavailable(() => daemonClient.getConfig(), "config", { allowRestart: false })
  );
  ipcMain.handle(IPC_CHANNELS.setConfig, async (_event, profiles: Profile[]) =>
    withDaemonRestartOnUnavailable(() => daemonClient.setConfig(profiles), "setConfig")
  );
  ipcMain.handle(IPC_CHANNELS.getAppVersion, async () => app.getVersion());

  ipcMain.handle("app:openExternal", async (_event, url: string) => {
    const { shell } = await import("electron");
    if (typeof url === "string" && (url.startsWith("https://") || url.startsWith("http://"))) {
      await shell.openExternal(url);
    }
  });

  ipcMain.handle(IPC_CHANNELS.authLogin, async (_event, vpnToken: string) => {
    if (!vpnToken || typeof vpnToken !== "string" || vpnToken.trim().length === 0) {
      return { authenticated: false, user: null };
    }

    try {
      const data = await pangeaApiClient.tokenLogin(vpnToken.trim());
      await auth.saveLicenseKey(data.vpnAccessToken);

      // Generate identity keypair for device registration
      const { generateKeyPairSync } = await import("node:crypto");
      const { publicKey: pubKeyObj, privateKey: privKeyObj } = generateKeyPairSync("x25519");
      const privDer = privKeyObj.export({ type: "pkcs8", format: "der" }) as Buffer;
      const pubDer = pubKeyObj.export({ type: "spki", format: "der" }) as Buffer;
      const identityPrivateKey = privDer.subarray(16).toString("base64");
      const identityPublicKey = pubDer.subarray(12).toString("base64");

      // Generate a friendly name for this device
      const friendlyName = generateFriendlyName();

      // Register device with the hub (reserves a device slot, max 4 per user).
      // The server returns the *effective* name (which may differ from ours if the
      // identityPubkey was already registered and has a stored name).
      let effectiveFriendlyName: string | null = friendlyName;
      try {
        const regResponse = await pangeaApiClient.registerDevice(identityPublicKey, friendlyName);
        if (regResponse.friendlyName) {
          effectiveFriendlyName = regResponse.friendlyName;
        }
      } catch (regErr) {
        console.warn("device registration failed:", regErr);
        const message = regErr instanceof Error ? regErr.message : "Device registration failed";

        // If device limit reached, keep the license key in pangeaApiClient so
        // the renderer can call listDevices / removeDevice to manage devices.
        // The license key on disk is cleared — it will be re-saved on successful retry.
        const isDeviceLimit =
          message.includes("DEVICE_LIMIT_REACHED") || message.includes("Device limit");
        if (isDeviceLimit) {
          await auth.clearLicenseKey();
          // Do NOT call pangeaApiClient.clearCache() — licenseKey must remain for device management
          return { authenticated: false, user: null, error: "DEVICE_LIMIT_REACHED" };
        }

        await auth.clearLicenseKey();
        pangeaApiClient.clearCache();
        return { authenticated: false, user: null, error: message };
      }

      // Registration succeeded — persist identity keypair and set on API client
      await auth.saveIdentityKeyPair({ privateKey: identityPrivateKey, publicKey: identityPublicKey });
      pangeaApiClient.identityPubkey = identityPublicKey;

      const authState = await auth.loginWithToken(data.vpnAccessToken, data.user);
      return { ...authState, friendlyName: effectiveFriendlyName };
    } catch (err) {
      console.warn("token login failed:", err);
      return { authenticated: false, user: null };
    }
  });

  ipcMain.handle(IPC_CHANNELS.authLogout, async () => {
    try {
      const status = await daemonClient.getStatus();
      if (status.state === "CONNECTED" || status.state === "CONNECTING") {
        await daemonClient.disconnect();
      }
    } catch {
      // daemon may be unavailable
    }

    // Best-effort deregister device from hub before clearing local state
    try {
      const identityKeys = await auth.loadIdentityKeyPair();
      if (identityKeys && pangeaApiClient.getLicenseKey()) {
        await pangeaApiClient.deregisterDevice(identityKeys.publicKey);
      }
    } catch {
      // best-effort — server may be unreachable
    }

    if (managedProfileId) {
      try {
        const config = await daemonClient.getConfig();
        const profiles = config.profiles.filter((p) => p.id !== managedProfileId);
        await daemonClient.setConfig(profiles);
      } catch {
        // best-effort cleanup
      }
      managedProfileId = null;
    }

    pangeaApiClient.clearCache();
    await auth.logout();
    void refreshTrayStatus();
  });

  ipcMain.handle(IPC_CHANNELS.authGetState, async () => {
    const state = await auth.getAuthState();
    // If authenticated and license key loaded but not yet in API client, restore it
    if (state.authenticated && !pangeaApiClient.getLicenseKey()) {
      const savedKey = await auth.loadLicenseKey().catch(() => null);
      if (savedKey) {
        pangeaApiClient.setLicenseKey(savedKey);
      }
    }
    return state;
  });

  ipcMain.handle(IPC_CHANNELS.setDoh, async (_event, enabled: boolean) => {
    pangeaApiClient.setDohEnabled(enabled);
    try {
      const filePath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "settings.json"
      );
      const fs = (await import("node:fs/promises")).default;
      let settings: Record<string, unknown> = {};
      try {
        settings = JSON.parse(await fs.readFile(filePath, "utf8"));
      } catch {
        // no existing file
      }
      settings.dohEnabled = enabled;
      await fs.writeFile(filePath, JSON.stringify(settings, null, 2));
    } catch {
      // best-effort persistence
    }
  });

  ipcMain.handle(IPC_CHANNELS.getDoh, async () => pangeaApiClient.isDohEnabled());

  ipcMain.handle(IPC_CHANNELS.setDirectIp, async (_event, enabled: boolean) => {
    pangeaApiClient.setDirectIpEnabled(enabled);
    try {
      const filePath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "settings.json"
      );
      const fs = (await import("node:fs/promises")).default;
      let settings: Record<string, unknown> = {};
      try {
        settings = JSON.parse(await fs.readFile(filePath, "utf8"));
      } catch {
        // no existing file
      }
      settings.directIpEnabled = enabled;
      await fs.writeFile(filePath, JSON.stringify(settings, null, 2));
    } catch {
      // best-effort persistence
    }
  });

  ipcMain.handle(IPC_CHANNELS.getDirectIp, async () => pangeaApiClient.isDirectIpEnabled());

  ipcMain.handle(IPC_CHANNELS.setDirectIpOnly, async (_event, enabled: boolean) => {
    pangeaApiClient.setDirectIpOnly(enabled);
    try {
      const settingsPath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "settings.json"
      );
      const raw = await (await import("node:fs/promises")).default.readFile(settingsPath, "utf8").catch(() => "{}");
      const settings = JSON.parse(raw) as Record<string, unknown>;
      settings.directIpOnly = enabled;
      await (await import("node:fs/promises")).default.writeFile(settingsPath, JSON.stringify(settings, null, 2));
    } catch (err) {
      console.warn("Failed to persist directIpOnly setting:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getDirectIpOnly, async () => pangeaApiClient.isDirectIpOnly());

  ipcMain.handle(IPC_CHANNELS.getCachedServers, async () => {
    try {
      const cachePath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "server-cache.json"
      );
      const raw = await (await import("node:fs/promises")).default.readFile(cachePath, "utf8");
      return JSON.parse(raw);
    } catch {
      return [];
    }
  });

  ipcMain.handle(IPC_CHANNELS.cacheServers, async (_event, servers: unknown[]) => {
    try {
      const cachePath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "server-cache.json"
      );
      await (await import("node:fs/promises")).default.writeFile(cachePath, JSON.stringify(servers), "utf8");
    } catch {
      // best-effort
    }
  });

  ipcMain.handle(IPC_CHANNELS.getServers, async () => {
    try {
      return await pangeaApiClient.getServers();
    } catch (err) {
      if (err instanceof AuthError) {
        pangeaApiClient.clearCache();
        await auth.logout();
        mainWindow?.webContents.send("auth:invalidated");
        return [];
      }
      throw err;
    }
  });

  ipcMain.handle(IPC_CHANNELS.listDevices, async () => {
    return pangeaApiClient.listDevices();
  });

  ipcMain.handle(IPC_CHANNELS.removeDevice, async (_event, deviceId: string) => {
    await pangeaApiClient.removeDevice(deviceId);
  });

  ipcMain.handle(IPC_CHANNELS.provisionAndConnect, async (_event, serverId: string) => {
    try {
      const result = await provisionAndConnect(serverId);
      void refreshTrayStatus();
      return result;
    } catch (err) {
      if (err instanceof AuthError) {
        pangeaApiClient.clearCache();
        await auth.logout();
        mainWindow?.webContents.send("auth:invalidated");
        return { ok: false };
      }
      throw err;
    }
  });
}

type DaemonRetryOptions = {
  allowRestart?: boolean;
};

async function withDaemonRestartOnUnavailable<T>(
  operation: () => Promise<T>,
  action: string,
  options: DaemonRetryOptions = {}
): Promise<T> {
  const allowRestart = options.allowRestart !== false;
  try {
    return await operation();
  } catch (firstError) {
    if (!isDaemonUnavailableError(firstError)) {
      throw firstError;
    }
    const shouldForceRestart = isTokenMissingError(firstError) || isUnauthorizedError(firstError);
    if (!allowRestart && !shouldForceRestart) {
      throw firstError;
    }

    const now = Date.now();
    if (now - lastDaemonRestartAttemptAtMs < daemonRestartBackoffMs) {
      throw firstError;
    }
    lastDaemonRestartAttemptAtMs = now;
    console.warn(`daemon unavailable during ${action}; attempting restart`, firstError);
    await daemonProcess.ensureRunning({
      forceRestart: shouldForceRestart
    });
    return operation();
  }
}

function isDaemonUnavailableError(error: unknown): boolean {
  if (!(error instanceof Error)) {
    return false;
  }

  const message = error.message.toLowerCase();
  return (
    message.includes("fetch failed") ||
    message.includes("failed to fetch") ||
    message.includes("econnrefused") ||
    message.includes("socket hang up") ||
    message.includes("daemon token not found")
  );
}

function isTokenMissingError(error: unknown): boolean {
  if (!(error instanceof Error)) {
    return false;
  }
  return error.message.toLowerCase().includes("daemon token not found");
}

function isUnauthorizedError(error: unknown): boolean {
  if (!(error instanceof Error)) {
    return false;
  }
  return error.message.toLowerCase().includes("daemon unauthorized");
}

async function connectWithRecovery(profileId: string): Promise<OkResponse> {
  const firstAttempt = await withDaemonRestartOnUnavailable(() => daemonClient.connect(profileId), "connect");
  if (firstAttempt.ok) {
    return firstAttempt;
  }

  if (!(process.platform === "darwin" && app.isPackaged)) {
    return firstAttempt;
  }

  try {
    await daemonProcess.ensureRunning({ forceRestart: true });
    return await daemonClient.connect(profileId);
  } catch (error) {
    console.warn("mac connect recovery failed", error);
    return firstAttempt;
  }
}

async function boot(): Promise<void> {
  await app.whenReady();

  // Restore persisted settings
  try {
    const settingsPath = (await import("node:path")).join(
      (await import("./platformPaths")).getAppSupportDir(),
      "settings.json"
    );
    const settingsRaw = await (await import("node:fs/promises")).default.readFile(settingsPath, "utf8");
    const settings = JSON.parse(settingsRaw) as Record<string, unknown>;
    if (settings.dohEnabled === true) {
      pangeaApiClient.setDohEnabled(true);
    }
    if (settings.directIpEnabled === true) {
      pangeaApiClient.setDirectIpEnabled(true);
    }
    if (settings.directIpOnly === false) {
      pangeaApiClient.setDirectIpOnly(false);
    }
  } catch {
    // no settings file yet
  }

  const savedKey = await auth.loadLicenseKey().catch(() => null);
  if (savedKey) {
    pangeaApiClient.setLicenseKey(savedKey);
  }

  // Restore persistent identity keypair (if exists from previous sign-in)
  const identityKeys = await auth.loadIdentityKeyPair().catch(() => null);
  if (identityKeys) {
    pangeaApiClient.identityPubkey = identityKeys.publicKey;
  }

  const appMenu = Menu.buildFromTemplate([
    {
      label: "PangeaVPN",
      submenu: [
        { role: "about" },
        { type: "separator" },
        {
          label: "Hide Window",
          accelerator: "CmdOrCtrl+H",
          click: () => hideMainWindow()
        },
        { type: "separator" },
        {
          label: "Quit",
          accelerator: "CmdOrCtrl+Q",
          click: () => {
            isQuitting = true;
            app.quit();
          }
        }
      ]
    },
    {
      label: "Edit",
      submenu: [
        { role: "undo" },
        { role: "redo" },
        { type: "separator" },
        { role: "cut" },
        { role: "copy" },
        { role: "paste" },
        { role: "selectAll" }
      ]
    }
  ]);
  Menu.setApplicationMenu(appMenu);

  registerIpcHandlers();
  createWindow();
  if (mainWindow) {
    setupAutoUpdater(mainWindow);
  }
  createTray();
  daemonProcess.ensureRunning().catch((err) => {
    console.error("failed to ensure daemon on startup", err);
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("activate", () => {
  if (BrowserWindow.getAllWindows().length === 0) {
    createWindow();
    return;
  }
  showMainWindow();
});

app.on("before-quit", () => {
  isQuitting = true;
  stopTrayStatusPolling();
  tray?.destroy();
  tray = null;
  trayDefaultImage = null;
  trayConnectedImage = null;
  daemonProcess.stop();
});

// Ensure only one instance of the app is running (Windows especially)
const gotTheLock = app.requestSingleInstanceLock();
if (!gotTheLock) {
  app.quit();
} else {
  app.on("second-instance", () => {
    showMainWindow();
  });

  boot().catch((err) => {
    console.error("failed to boot desktop app", err);
  });
}
