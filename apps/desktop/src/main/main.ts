import { Menu, Notification, Tray, app, BrowserWindow, ipcMain, nativeImage, session, shell, type NativeImage } from "electron";
import path from "node:path";
import type { OkResponse, Profile, StatusResponse } from "@pangeavpn/shared-types";
import { DaemonClient, TransportExhaustedError } from "./daemonClient";
import { DaemonProcessManager } from "./daemonProcess";
import { readDaemonTokens } from "./platformPaths";
import { getConnectedTrayIconPath, getTrayIconPath, getWindowsAppIconPath } from "./resourcePaths";
import { IPC_CHANNELS, type ConnectResult, type ServerInfo } from "../shared/ipc";
import * as auth from "./auth";
import {
  PangeaApiClient,
  AuthError,
  ConnectCancelledError,
  SubscriptionExpiredError
} from "./pangeaApiClient";
import type { HubShadowsocksCreds } from "../shared/hubShadowsocksCreds";
import { beginAttempt, cancelAttempt, endAttempt, isCancelled } from "./connectAttempt";
import { setupAutoUpdater, notifyConnectionStateChange } from "./autoUpdater";
import { setLoginItemEnabled, isLoginItemEnabled, isHiddenLaunchArg } from "./loginItem";
import { startNetworkWatcher, onNetworkChange } from "./networkWatcher";
import { mt, mtState, setMainLocale, resolveMainLocale } from "./i18n";
import { sanitizeLog } from "./logSanitize";
import { shouldShowTrayHint, trayHintBodyKey } from "./trayHint";
import {
  applyHubMethod,
  isHubMethod,
  normalizeHubMethods,
  persistableHubMethods
} from "../shared/hubMethods";
import {
  buildServerRetryOrder as buildMainServerRetryOrder,
  replaceManagedProfile,
  runServerFallback
} from "./serverFallback";

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
let daemonRecoveryInProgress = false;
let trayHintShown = false;
let setWidth = 640;
let setHeight = 440;
const daemonRestartBackoffMs = 5000;

const daemonClient = new DaemonClient("http://127.0.0.1:8787", readDaemonTokens);
const daemonProcess = new DaemonProcessManager(daemonClient);
const pangeaApiClient = new PangeaApiClient();

let managedProfileId: string | null = null;
let lastServerId: string | null = null;
let connectionAttemptRunning = false;
let allowLanEnabled = true;
let launchAtStartupEnabled = false;
let alwaysConnectedEnabled = false;
// "auto" (reality, then cloak, shadowsocks, hysteria2, naive), or one of
// "cloak"/"naive"/"reality"/"hysteria2"/"shadowsocks"/"snowflake" only.
let preferredTransport: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard" = "auto";
// Stored language preference: a locale code, or "system" to follow the OS.
let localePref = "system";
const hiddenLaunch = process.argv.some(isHiddenLaunchArg);

// Login item on if launch-at-startup or Lockdown is enabled — Lockdown needs the tray app on boot to reconnect.
async function applyLoginItem(): Promise<void> {
  await setLoginItemEnabled(launchAtStartupEnabled || alwaysConnectedEnabled);
}

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
    show: false,
    ...(windowIconPath ? { icon: windowIconPath } : {}),
    webPreferences: {
      contextIsolation: true,
      nodeIntegration: false,
      sandbox: true,
      devTools: !app.isPackaged,
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
    if (isQuitting || daemonRecoveryInProgress) return;
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
      mainWindow?.setBounds({ x: pos.x, y: Math.round(startY + (pos.y - startY) * ease), width: setWidth, height: setHeight });
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

function hideMainWindow(fromTrayClick = false): void {
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
      mainWindow?.setBounds({ x: startX, y: Math.round(startY + (endY - startY) * ease), width: setWidth, height: setHeight });
    }

    if (step >= steps) {
      clearInterval(timer);
      mainWindow?.hide();
      mainWindow?.setOpacity(1);
      hiding = false;
      updateTrayMenu();
      void maybeShowTrayHint(fromTrayClick);
    }
  }, interval);
}

// The window has no taskbar entry, so a first-time user reads this first
// vanish as a quit. Tell them where it went, once per install.
async function maybeShowTrayHint(fromTrayClick: boolean): Promise<void> {
  const conditions = {
    alreadyShown: trayHintShown,
    fromTrayClick,
    supported: Notification.isSupported()
  };
  if (!shouldShowTrayHint(conditions)) {
    return;
  }
  // Set before the async write so a second hide mid-write can't double-fire.
  trayHintShown = true;

  const iconPath = process.platform === "darwin" ? undefined : getTrayIconPath(__dirname);
  const notification = new Notification({
    title: mt("notify.trayTitle"),
    body: mt(trayHintBodyKey(process.platform)),
    ...(iconPath ? { icon: iconPath } : {})
  });
  notification.on("click", () => showMainWindow());
  notification.show();

  try {
    const settings = await readSettingsFile();
    settings.trayHintShown = true;
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("failed to persist tray hint flag:", sanitizeLog(err));
  }
}

function toggleMainWindowVisibility(): void {
  if (!mainWindow || !mainWindow.isVisible()) {
    showMainWindow();
    return;
  }
  hideMainWindow(true);
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

  const stateLabel = mtState(trayStatusState);
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
        label: mt("tray.status", { state: stateLabel }),
        enabled: false
      },
      {
        label: mt("tray.detail", { detail: detailLabel }),
        enabled: false
      },
      { type: "separator" },
      {
        label: mt("tray.connect"),
        enabled: canConnect,
        click: () => {
          void connectFromTray();
        }
      },
      {
        label: mt("tray.disconnect"),
        enabled: canDisconnect,
        click: () => {
          void disconnectFromTray();
        }
      },
      {
        type: "separator"
      },
      {
        label: windowVisible ? mt("tray.hide") : mt("tray.show"),
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
        label: mt("tray.quit"),
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
    notifyConnectionStateChange(trayStatusState);
  }
}

let networkRecoverInProgress = false;
let lastNetworkRecoverAtMs = 0;
const NETWORK_RECOVER_COOLDOWN_MS = 10_000;

async function recoverFromNetworkChange(): Promise<void> {
  if (networkRecoverInProgress || connectionAttemptRunning) return;
  const now = Date.now();
  if (now - lastNetworkRecoverAtMs < NETWORK_RECOVER_COOLDOWN_MS) return;
  if (!alwaysConnectedEnabled) return;
  if (!lastConnectedProfileId) return;

  networkRecoverInProgress = true;
  connectionAttemptRunning = true;
  lastNetworkRecoverAtMs = now;
  try {
    // Refresh status first so we don't fire over an already-healthy tunnel.
    await refreshTrayStatus();
    if (trayStatusState === "CONNECTED" || trayStatusState === "CONNECTING") {
      return;
    }
    console.log("network change detected — attempting reconnect");
    // Tear down stale tunnel/firewall state without clearing the kill switch
    // (we're in lockdown mode), then bring the tunnel back on the new network.
    try {
      await daemonClient.disconnect({ keepKillSwitch: true });
    } catch (err) {
      console.warn("network recover: disconnect failed", err);
    }
    const result = await connectWithRecovery(lastConnectedProfileId);
    if (!result.ok) {
      console.warn("network recover: reconnect failed", (result as { error?: string }).error);
    }
  } finally {
    networkRecoverInProgress = false;
    connectionAttemptRunning = false;
    await refreshTrayStatus();
  }
}

/**
 * Connect the profile the daemon already holds, with no hub contact at all.
 *
 * The profile carries a WireGuard key the hub registered on a previous run, so
 * it is the one way to reach a node while the hub is unreachable — provisioning
 * a new one needs a /api/register round trip, which is exactly what is blocked.
 * Assumes the caller owns connectionAttemptRunning.
 */
async function connectExistingProfile(): Promise<boolean> {
  const profileId = lastConnectedProfileId ?? managedProfileId;
  if (!profileId) return false;

  const config = await withDaemonRestartOnUnavailable(
    () => daemonClient.getConfig(),
    "tray config",
    { allowRestart: false }
  );
  if (!config.profiles.some((profile) => profile.id === profileId)) return false;

  const result = await connectWithRecovery(profileId);
  if (!result.ok) return false;
  lastConnectedProfileId = profileId;
  void persistLastConnection();
  return true;
}

async function reconnectExistingProfile(): Promise<boolean> {
  if (connectionAttemptRunning) return false;

  connectionAttemptRunning = true;
  try {
    return await connectExistingProfile();
  } finally {
    connectionAttemptRunning = false;
  }
}

/**
 * Is this failure worth retrying against the profile we already have?
 *
 * Only reachability failures are. A hub that answered and said no — the device
 * was removed, the subscription lapsed — will have deprovisioned the peer that
 * profile names, so retrying it wastes a handshake deadline to arrive at the
 * same answer with a worse error message. Cancellation is the user's decision
 * and must not be undone by a fallback.
 */
function isHubReachabilityFailure(err: unknown): boolean {
  return !(
    err instanceof AuthError ||
    err instanceof SubscriptionExpiredError ||
    err instanceof ConnectCancelledError
  );
}

async function connectFromTray(): Promise<void> {
  if (trayActionInProgress) {
    return;
  }

  trayActionInProgress = true;
  updateTrayMenu();
  try {
    let exhaustedServerId: string | null = null;
    try {
      if (await reconnectExistingProfile()) return;
    } catch (error) {
      if (!(error instanceof TransportExhaustedError)) throw error;
      exhaustedServerId = lastServerId;
    }

    const serverPlan = await resolveTrayServerPlan(exhaustedServerId);
    if (!serverPlan) {
      trayStatusState = "ERROR";
      trayStatusDetail = "no server available";
      return;
    }

    const result = await provisionAndConnect(serverPlan);
    if (!result.ok) {
      trayStatusState = "ERROR";
      trayStatusDetail = "connect request failed";
    }
  } catch (error) {
    console.warn("tray connect failed", sanitizeLog(error));
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
    const result = await withDaemonRestartOnUnavailable(
      () => daemonClient.disconnect({ keepKillSwitch: alwaysConnectedEnabled }),
      "tray disconnect"
    );
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

async function resolveTrayServerPlan(excludedServerId: string | null = null): Promise<string[] | null> {
  try {
    const servers = await pangeaApiClient.getServers();
    if (servers.length > 0) {
      const initialServerId = lastServerId && servers.some((server) => server.id === lastServerId)
        ? lastServerId
        : servers[0].id;
      const plan = buildMainServerRetryOrder(servers, initialServerId)
        .filter((serverId) => serverId !== excludedServerId);
      return plan.length > 0 ? plan : null;
    }
  } catch {
    // no servers available
  }

  return null;
}

/** Lockdown's lock blocks the hub we must reach to provision, so open the hub
 *  alone. Best-effort: a failure surfaces as the real network error. */
async function permitHubThroughLockdown(): Promise<void> {
  if (!alwaysConnectedEnabled) return;
  const hubIp = pangeaApiClient.getHubIp();
  try {
    await daemonClient.permitHosts(hubIp ? [hubIp] : []);
  } catch (err) {
    console.warn("lockdown: hub permit failed", sanitizeLog(err));
  }
}

async function provisionProfileForServer(serverId: string, signal?: AbortSignal): Promise<Profile> {
  await permitHubThroughLockdown();
  const profile = await pangeaApiClient.provision(serverId, signal);

  const config = await withDaemonRestartOnUnavailable(
    () => daemonClient.getConfig(),
    "provision-config",
    { allowRestart: false }
  );

  let profiles = config.profiles;
  profiles = profiles.filter((p) => p.id !== profile.id);
  profiles.push(profile);

  await withDaemonRestartOnUnavailable(() => daemonClient.setConfig(profiles), "provision-setConfig");
  return profile;
}

function normalizeServerPlan(value: unknown): string[] {
  if (!Array.isArray(value) || value.length === 0 || value.length > 128) {
    throw new Error("Invalid server retry plan");
  }
  const serverIds: string[] = [];
  for (const candidate of value) {
    if (typeof candidate !== "string" || candidate.trim() === "") {
      throw new Error("Invalid server retry plan");
    }
    const serverId = candidate.trim();
    if (!serverIds.includes(serverId)) serverIds.push(serverId);
  }
  return serverIds;
}

async function provisionAcrossServers(serverIds: readonly string[], mode: "connect" | "switch"): Promise<ConnectResult> {
  if (connectionAttemptRunning) {
    return { ok: false, error: "connect-in-progress" };
  }
  connectionAttemptRunning = true;
  const attempt = beginAttempt();
  const previousManagedProfileId = managedProfileId;
  let initialProfiles: Profile[] | null = null;
  let configChanged = false;
  let committed = false;
  try {
    const initialConfig = await withDaemonRestartOnUnavailable(
      () => daemonClient.getConfig(),
      "connect-config-snapshot",
      { allowRestart: false }
    );
    initialProfiles = initialConfig.profiles;
    const candidates = preferredTransport === "auto" ? serverIds : serverIds.slice(0, 1);
    const outcome = await runServerFallback(
      candidates,
      async (serverId, index) => {
        if (isCancelled(attempt)) throw new ConnectCancelledError();
        const profile = await provisionProfileForServer(serverId, attempt.controller.signal);
        configChanged = true;

        // Provisioning is several round trips; Stop must win before the daemon
        // receives a connect or switch request.
        if (isCancelled(attempt)) throw new ConnectCancelledError();

        const result = mode === "switch" && index === 0
          ? await withDaemonRestartOnUnavailable(
              () => daemonClient.switch(profile.id, connectionOptions()),
              "switch"
            )
          : await connectWithRecovery(profile.id);

        // Daemon connect can't be un-sent. If Stop landed mid-flight, tear it
        // back down while preserving Lockdown's kill switch.
        if (isCancelled(attempt)) {
          if (result.ok) {
            await daemonClient
              .disconnect({ keepKillSwitch: alwaysConnectedEnabled })
              .catch((err) => console.warn("cancel: disconnect failed", sanitizeLog(err)));
          }
          throw new ConnectCancelledError();
        }
        return { profile, result };
      },
      (error) => preferredTransport === "auto" && error instanceof TransportExhaustedError
    );

    if (outcome.value.result.ok) {
      const committedProfiles = replaceManagedProfile(
        initialProfiles,
        previousManagedProfileId,
        outcome.value.profile
      );
      await daemonClient.setConfig(committedProfiles).catch((error) => {
        console.warn("connect: profile cleanup failed", sanitizeLog(error));
      });
      if (isCancelled(attempt)) {
        await daemonClient
          .disconnect({ keepKillSwitch: alwaysConnectedEnabled })
          .catch((error) => console.warn("cancel: disconnect after cleanup failed", sanitizeLog(error)));
        throw new ConnectCancelledError();
      }
      committed = true;
      managedProfileId = outcome.value.profile.id;
      lastServerId = outcome.serverId;
      lastConnectedProfileId = outcome.value.profile.id;
      void persistLastConnection();
    }
    return { ...outcome.value.result, serverId: outcome.serverId };
  } catch (err) {
    // A cancelled attempt aborts its in-flight request, which surfaces here.
    // Report it as a non-error so the UI goes idle instead of showing a toast.
    if (err instanceof ConnectCancelledError || isCancelled(attempt)) {
      return { ok: false, error: "cancelled" };
    }
    if (err instanceof TransportExhaustedError) {
      return { ok: false, error: "all-servers-exhausted" };
    }
    throw err;
  } finally {
    if (configChanged && !committed && initialProfiles) {
      await daemonClient.setConfig(initialProfiles).catch((error) => {
        console.warn("connect: profile restore failed", sanitizeLog(error));
      });
    }
    const cancelledDuringCleanup = !committed && isCancelled(attempt);
    endAttempt(attempt);
    connectionAttemptRunning = false;
    if (cancelledDuringCleanup) {
      return { ok: false, error: "cancelled" };
    }
  }
}

/**
 * Provision and connect, falling back to the profile the daemon already holds
 * when the hub cannot be reached.
 *
 * Runs after provisionAcrossServers has fully unwound — its finally has already
 * restored the config snapshot and released connectionAttemptRunning — so the
 * fallback connects against settled state rather than racing the cleanup.
 *
 * Switching deliberately has no equivalent: a switch that cannot reach the hub
 * unwinds to the connection the user already had, which is a better outcome
 * than replacing it with an older one.
 */
async function provisionAndConnect(serverIds: readonly string[]): Promise<ConnectResult> {
  try {
    return await provisionAcrossServers(serverIds, "connect");
  } catch (err) {
    if (!isHubReachabilityFailure(err)) throw err;
    console.warn("connect: hub unreachable, trying the last working profile", sanitizeLog(err));
    if (await reconnectExistingProfile()) {
      return { ok: true, ...(lastServerId ? { serverId: lastServerId } : {}) };
    }
    throw err;
  }
}

async function provisionAndSwitch(serverIds: readonly string[]): Promise<ConnectResult> {
  return provisionAcrossServers(serverIds, "switch");
}

/** Stop the in-flight connect attempt; never tears down a wanted connection. */
async function cancelConnectAttempt(): Promise<void> {
  const cancelled = cancelAttempt();
  if (!cancelled) return;

  // The attempt may already have handed the daemon a connect. Ask it to stand
  // down; the attempt's own guards handle the rest.
  try {
    const status = await daemonClient.getStatus();
    if (status.state !== "DISCONNECTED") {
      await daemonClient.disconnect({ keepKillSwitch: alwaysConnectedEnabled });
    }
  } catch (err) {
    console.warn("cancel: daemon teardown failed", sanitizeLog(err));
  }
  await refreshTrayStatus();
}

function connectionOptions(): {
  allowLAN: boolean;
  lockdown: boolean;
  preferredTransport?: "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard";
} {
  return {
    allowLAN: allowLanEnabled,
    lockdown: alwaysConnectedEnabled,
    ...(preferredTransport !== "auto" && { preferredTransport })
  };
}

async function readSettingsFile(): Promise<Record<string, unknown>> {
  try {
    const filePath = path.join(
      (await import("./platformPaths")).getAppSupportDir(),
      "settings.json"
    );
    const fs = (await import("node:fs/promises")).default;
    return JSON.parse(await fs.readFile(filePath, "utf8")) as Record<string, unknown>;
  } catch {
    return {};
  }
}

async function writeSettingsFile(settings: Record<string, unknown>): Promise<void> {
  const dir = (await import("./platformPaths")).getAppSupportDir();
  const fs = (await import("node:fs/promises")).default;
  // First run can reach here before the daemon has created the directory.
  await fs.mkdir(dir, { recursive: true });
  await fs.writeFile(path.join(dir, "settings.json"), JSON.stringify(settings, null, 2));
}

async function persistHubIp(ip: string): Promise<void> {
  try {
    const settings = await readSettingsFile();
    settings.hubIp = ip;
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("Failed to persist hub IP:", err);
  }
}

async function persistHubShadowsocks(creds: HubShadowsocksCreds[]): Promise<void> {
  try {
    const settings = await readSettingsFile();
    settings.hubShadowsocks = creds;
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("Failed to persist hub Shadowsocks credentials:", err);
  }
}

async function persistFrontedEndpoints(endpoints: string[]): Promise<void> {
  try {
    const settings = await readSettingsFile();
    settings.frontedEndpoints = endpoints;
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("Failed to persist fronted endpoints:", err);
  }
}

/** The node list, so a client that cannot reach the hub still knows where the
 *  servers are. Cleared on logout, which passes an empty list. */
async function persistServers(servers: ServerInfo[]): Promise<void> {
  try {
    const settings = await readSettingsFile();
    if (servers.length === 0) {
      delete settings.servers;
    } else {
      settings.servers = servers;
    }
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("Failed to persist server list:", err);
  }
}

async function persistLastConnection(): Promise<void> {
  try {
    const settings = await readSettingsFile();
    settings.lastServerId = lastServerId;
    settings.lastProfileId = lastConnectedProfileId;
    await writeSettingsFile(settings);
  } catch (err) {
    console.warn("Failed to persist last connection:", err);
  }
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
      void persistLastConnection();
    }
    void refreshTrayStatus();
    return result;
  });
  ipcMain.handle(IPC_CHANNELS.disconnect, async () => {
    const result = await withDaemonRestartOnUnavailable(
      () => daemonClient.disconnect({ keepKillSwitch: alwaysConnectedEnabled }),
      "disconnect"
    );
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
  ipcMain.handle(IPC_CHANNELS.restartDaemon, async () => {
    daemonRecoveryInProgress = true;
    try {
      await daemonProcess.restartElevated(() => {
        daemonRecoveryInProgress = false;
      });
      lastDaemonRestartAttemptAtMs = 0;
      void refreshTrayStatus();
      return { ok: true };
    } catch (error) {
      console.warn("elevated daemon recovery failed", sanitizeLog(error));
      return { ok: false, error: sanitizeLog(error) };
    } finally {
      daemonRecoveryInProgress = false;
    }
  });
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

      // Reserves a device slot (max 4 per user). The hub returns the *effective*
      // name, which differs from ours if this identityPubkey already had one.
      let effectiveFriendlyName: string | null = friendlyName;
      try {
        const regResponse = await pangeaApiClient.registerDevice(identityPublicKey, friendlyName);
        if (regResponse.friendlyName) {
          effectiveFriendlyName = regResponse.friendlyName;
        }
      } catch (regErr) {
        console.warn("device registration failed:", sanitizeLog(regErr));
        const message = regErr instanceof Error ? regErr.message : "Device registration failed";

        // Device limit: keep the key in memory so the renderer can list/remove
        // devices. The on-disk key is cleared, re-saved on a successful retry.
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
      console.warn("token login failed:", sanitizeLog(err));
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

  // The renderer disables the last remaining switch, but settings.json is
  // hand-editable and IPC is callable directly, so the invariant is enforced
  // here as well — this is the authority, the UI only reflects it.
  ipcMain.handle(IPC_CHANNELS.setHubMethod, async (_event, method: unknown, enabled: unknown) => {
    const current = pangeaApiClient.getHubMethods();
    if (!isHubMethod(method)) {
      return { methods: current, applied: false };
    }
    const { methods, applied } = applyHubMethod(current, method, enabled === true);
    if (!applied) {
      return { methods: current, applied: false };
    }
    pangeaApiClient.setHubMethods(methods);
    try {
      const settingsPath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "settings.json"
      );
      const fs = (await import("node:fs/promises")).default;
      const raw = await fs.readFile(settingsPath, "utf8").catch(() => "{}");
      const settings = JSON.parse(raw) as Record<string, unknown>;
      // Stamped with the rev, so this deliberate choice is not overwritten by
      // the next default change the way a pre-rev file's would be.
      settings.hubMethods = persistableHubMethods(methods);
      // Drop the keys this replaced so a later downgrade cannot resurrect them.
      delete settings.directIpEnabled;
      delete settings.directIpOnly;
      await fs.writeFile(settingsPath, JSON.stringify(settings, null, 2));
    } catch (err) {
      console.warn("Failed to persist hubMethods setting:", err);
    }
    return { methods, applied: true };
  });

  ipcMain.handle(IPC_CHANNELS.getHubMethods, async () => pangeaApiClient.getHubMethods());

  ipcMain.handle(IPC_CHANNELS.setAllowLan, async (_event, enabled: boolean) => {
    allowLanEnabled = !!enabled;
    try {
      const settingsPath = (await import("node:path")).join(
        (await import("./platformPaths")).getAppSupportDir(),
        "settings.json"
      );
      const raw = await (await import("node:fs/promises")).default.readFile(settingsPath, "utf8").catch(() => "{}");
      const settings = JSON.parse(raw) as Record<string, unknown>;
      settings.allowLan = allowLanEnabled;
      await (await import("node:fs/promises")).default.writeFile(settingsPath, JSON.stringify(settings, null, 2));
    } catch (err) {
      console.warn("Failed to persist allowLan setting:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getAllowLan, async () => allowLanEnabled);

  // Returns the stored MTU, which differs from the requested one when it was
  // rejected — the renderer uses that mismatch to flag invalid input.
  ipcMain.handle(IPC_CHANNELS.setWireguardMtu, async (_event, mtu: unknown) => {
    const stored = pangeaApiClient.setWireguardMtu(mtu);
    try {
      const settings = await readSettingsFile();
      settings.wireguardMtu = stored;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist wireguardMtu setting:", err);
    }
    return stored;
  });

  ipcMain.handle(IPC_CHANNELS.getWireguardMtu, async () => pangeaApiClient.getWireguardMtu());

  ipcMain.handle(IPC_CHANNELS.setCustomDns, async (_event, value: unknown) => {
    const stored = pangeaApiClient.setCustomDns(value);
    try {
      const settings = await readSettingsFile();
      settings.customDns = stored;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist customDns setting:", err);
    }
    return stored;
  });

  ipcMain.handle(IPC_CHANNELS.getCustomDns, async () => pangeaApiClient.getCustomDns());

  ipcMain.handle(IPC_CHANNELS.setPreferredTransport, async (_event, value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard") => {
    preferredTransport =
      value === "cloak" ||
      value === "naive" ||
      value === "reality" ||
      value === "hysteria2" ||
      value === "shadowsocks" ||
      value === "snowflake" ||
      value === "wireguard"
        ? value
        : "auto";
    try {
      const settings = await readSettingsFile();
      settings.preferredTransport = preferredTransport;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist preferredTransport setting:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getPreferredTransport, async () => preferredTransport);

  ipcMain.handle(IPC_CHANNELS.setLaunchAtStartup, async (_event, enabled: boolean) => {
    launchAtStartupEnabled = !!enabled;
    try {
      const settings = await readSettingsFile();
      settings.launchAtStartup = launchAtStartupEnabled;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist launchAtStartup setting:", err);
    }
    try {
      await applyLoginItem();
    } catch (err) {
      console.warn("Failed to apply login item:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getLaunchAtStartup, async () => {
    // Lockdown forces the login item on, so return the stored preference rather than the OS state.
    if (alwaysConnectedEnabled) {
      return launchAtStartupEnabled;
    }
    // Self-heal: re-derive from OS in case the user toggled it elsewhere.
    try {
      const live = await isLoginItemEnabled();
      launchAtStartupEnabled = live;
      return live;
    } catch {
      return launchAtStartupEnabled;
    }
  });

  ipcMain.handle(IPC_CHANNELS.setAlwaysConnected, async (_event, enabled: boolean) => {
    const previouslyEnabled = alwaysConnectedEnabled;
    alwaysConnectedEnabled = !!enabled;
    try {
      const settings = await readSettingsFile();
      settings.alwaysConnected = alwaysConnectedEnabled;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist alwaysConnected setting:", err);
    }
    try {
      await applyLoginItem();
    } catch (err) {
      console.warn("Failed to apply login item for lockdown:", err);
    }
    if (!previouslyEnabled && alwaysConnectedEnabled) {
      // Sent unconditionally: while connected the daemon only records it as a
      // Lockdown lock, and skipping left Locked:false on disk, cleared as stale.
      try {
        await daemonClient.engageKillSwitch({
          profileId: lastConnectedProfileId ?? undefined,
          allowLAN: allowLanEnabled
        });
      } catch (err) {
        console.warn("Failed to engage kill switch on lockdown on:", err);
      }
    } else if (previouslyEnabled && !alwaysConnectedEnabled) {
      try {
        const status = await daemonClient.getStatus();
        if (status.state !== "CONNECTED" && status.state !== "CONNECTING") {
          await daemonClient.clearKillSwitch();
        }
      } catch (err) {
        console.warn("Failed to clear kill switch on lockdown off:", err);
      }
    }
  });

  ipcMain.handle(IPC_CHANNELS.getAlwaysConnected, async () => alwaysConnectedEnabled);

  ipcMain.handle(IPC_CHANNELS.getLastServer, async () => ({
    lastServerId,
    lastProfileId: lastConnectedProfileId
  }));

  ipcMain.handle(IPC_CHANNELS.clearLastServer, async () => {
    lastServerId = null;
    lastConnectedProfileId = null;
    await persistLastConnection();
  });

  ipcMain.handle(IPC_CHANNELS.getLocale, async () => localePref);

  ipcMain.handle(IPC_CHANNELS.setLocale, async (_event, locale: string) => {
    // Persist only — the change is applied on next launch (both the renderer
    // and the tray/menu read the locale once at startup).
    localePref = typeof locale === "string" && locale.length > 0 ? locale : "system";
    try {
      const settings = await readSettingsFile();
      settings.locale = localePref;
      await writeSettingsFile(settings);
    } catch (err) {
      console.warn("Failed to persist locale setting:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getIsPackaged, async () => app.isPackaged);

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

  ipcMain.handle(IPC_CHANNELS.getSubscription, async () => {
    return pangeaApiClient.getSubscription();
  });

  ipcMain.handle(IPC_CHANNELS.provisionAndConnect, async (_event, serverPlan: unknown) => {
    try {
      const result = await provisionAndConnect(normalizeServerPlan(serverPlan));
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

  ipcMain.handle(IPC_CHANNELS.cancelConnect, async () => {
    await cancelConnectAttempt();
  });

  ipcMain.handle(IPC_CHANNELS.provisionAndSwitch, async (_event, serverPlan: unknown) => {
    try {
      const result = await provisionAndSwitch(normalizeServerPlan(serverPlan));
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
  const opts = connectionOptions();
  const firstAttempt = await withDaemonRestartOnUnavailable(() => daemonClient.connect(profileId, opts), "connect");
  if (firstAttempt.ok) {
    return firstAttempt;
  }

  if (!(process.platform === "darwin" && app.isPackaged)) {
    return firstAttempt;
  }

  try {
    await daemonProcess.ensureRunning({ forceRestart: true });
    return await daemonClient.connect(profileId, opts);
  } catch (error) {
    console.warn("mac connect recovery failed", error);
    return firstAttempt;
  }
}

async function boot(): Promise<void> {
  await app.whenReady();

  // Windows drops toasts whose AUMID doesn't match a Start Menu shortcut's;
  // NSIS writes ours with build.appId, so the process must claim the same one.
  app.setAppUserModelId("com.pangea.pangeavpn");

  // Lock down navigation, new windows, embeds, permissions, and TLS.
  app.on("web-contents-created", (_event, contents) => {
    contents.setWindowOpenHandler(({ url }) => {
      if (url.startsWith("https://") || url.startsWith("http://")) {
        void shell.openExternal(url);
      }
      return { action: "deny" };
    });
    contents.on("will-navigate", (event, url) => {
      if (!url.startsWith("file://")) {
        event.preventDefault();
      }
    });
    contents.on("will-attach-webview", (event) => {
      event.preventDefault();
    });
  });
  session.defaultSession.setPermissionRequestHandler((_wc, _perm, cb) => cb(false));
  app.on("certificate-error", (event, _wc, _url, _err, _cert, cb) => {
    event.preventDefault();
    cb(false);
  });

  // Registered before the restore below so a first run with no settings file
  // still persists the hub IP once it is learned.
  pangeaApiClient.onHubIp((ip) => void persistHubIp(ip));
  pangeaApiClient.onHubShadowsocksResolved((creds) => void persistHubShadowsocks(creds));
  pangeaApiClient.onFrontedEndpointsResolved((endpoints) => void persistFrontedEndpoints(endpoints));
  pangeaApiClient.onServersResolved((servers) => void persistServers(servers));

  // The daemon owns the proxy; the client only decides when to ask for it.
  pangeaApiClient.setShadowsocksHubProxy({
    start: async (creds) => {
      try {
        return await daemonClient.startSsProxy(creds);
      } catch (err) {
        console.warn("Failed to start the Shadowsocks hub proxy:", sanitizeLog(err));
        return null;
      }
    },
    stop: async () => {
      try {
        await daemonClient.stopSsProxy();
      } catch {
        // best-effort
      }
    }
  });

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
    // Reads the current shape, and migrates the directIpEnabled/directIpOnly
    // pair it replaced. Always yields at least one enabled method.
    pangeaApiClient.setHubMethods(normalizeHubMethods(settings.hubMethods ?? settings));
    if (settings.allowLan === false) {
      allowLanEnabled = false;
    }
    // settings.json is hand-editable, so this goes through the same normalizer
    // as IPC input — anything unusable falls back to the default.
    pangeaApiClient.setWireguardMtu(settings.wireguardMtu);
    if (settings.customDns !== undefined) {
      try {
        pangeaApiClient.setCustomDns(settings.customDns);
      } catch {
        // Ignore invalid hand-edited settings and use the VPN server default.
      }
    }
    if (
      settings.preferredTransport === "cloak" ||
      settings.preferredTransport === "naive" ||
      settings.preferredTransport === "reality" ||
      settings.preferredTransport === "hysteria2" ||
      settings.preferredTransport === "shadowsocks" ||
      settings.preferredTransport === "snowflake" ||
      settings.preferredTransport === "wireguard"
    ) {
      preferredTransport = settings.preferredTransport;
    }
    if (typeof settings.launchAtStartup === "boolean") {
      launchAtStartupEnabled = settings.launchAtStartup;
    }
    if (typeof settings.alwaysConnected === "boolean") {
      alwaysConnectedEnabled = settings.alwaysConnected;
    }
    // Last known good hub IP: the only way to reach the hub once a Lockdown
    // lock is engaged, since the lock permits that IP but blocks DNS and DoH.
    pangeaApiClient.setCachedHubIp(settings.hubIp);
    // Was a single object before every node's credentials were cached, so an
    // existing install still has one to migrate.
    pangeaApiClient.setCachedHubShadowsocks(settings.hubShadowsocks);
    // Edge relays, and the last node list the hub gave us. Both are what stands
    // between a blocked hub and a client with nowhere left to go.
    pangeaApiClient.setCachedFrontedEndpoints(settings.frontedEndpoints);
    pangeaApiClient.setCachedServers(settings.servers);
    if (typeof settings.lastServerId === "string") {
      lastServerId = settings.lastServerId;
    }
    if (typeof settings.lastProfileId === "string") {
      lastConnectedProfileId = settings.lastProfileId;
    }
    if (typeof settings.locale === "string") {
      localePref = settings.locale;
    }
    if (settings.trayHintShown === true) {
      trayHintShown = true;
    }
  } catch {
    // no settings file yet
  }

  // Resolve the display locale for the tray/menu (built below). The language
  // is fixed for this run; changing it applies on the next launch.
  setMainLocale(resolveMainLocale(localePref, app.getLocale()));

  // Reconcile OS login-item state with our persisted preference (handles reinstalls).
  try {
    await applyLoginItem();
  } catch (err) {
    console.warn("Failed to reconcile login item:", err);
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
          label: mt("menu.hideWindow"),
          accelerator: "CmdOrCtrl+H",
          click: () => hideMainWindow()
        },
        { type: "separator" },
        {
          label: mt("menu.quit"),
          accelerator: "CmdOrCtrl+Q",
          click: () => {
            isQuitting = true;
            app.quit();
          }
        }
      ]
    },
    {
      label: mt("menu.edit"),
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
  if (!hiddenLaunch) {
    showMainWindow();
  }
  daemonProcess
    .ensureRunning()
    .then(async () => {
      // Covers the case where no lock state was persisted yet; the daemon
      // re-applies persisted locks itself. No-ops if already up or engaged.
      if (!alwaysConnectedEnabled) return;
      const status = await daemonClient.getStatus();
      if (status.state !== "CONNECTED" && status.state !== "CONNECTING") {
        await daemonClient.engageKillSwitch({
          profileId: lastConnectedProfileId ?? undefined,
          allowLAN: allowLanEnabled
        });
      }
    })
    .catch((err) => {
      console.error("failed to ensure daemon / engage lockdown on startup", err);
    });

  startNetworkWatcher();
  onNetworkChange(() => {
    void recoverFromNetworkChange();
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
