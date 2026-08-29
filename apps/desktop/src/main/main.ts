import { Menu, Notification, Tray, app, BrowserWindow, ipcMain, nativeImage, session, shell, type NativeImage } from "electron";
import path from "node:path";
import type { ConfigResponse, OkResponse, Profile, StatusResponse } from "@pangeavpn/shared-types";
import { DaemonClient, HostOfflineError, TransportExhaustedError } from "./daemonClient";
import { DaemonProcessManager } from "./daemonProcess";
import { getLegacyStateFilePath, getUserStateDir, readDaemonTokens } from "./platformPaths";
import { getConnectedTrayIconPath, getTrayIconPath, getWindowsAppIconPath } from "./resourcePaths";
import {
  IPC_CHANNELS,
  toPublicServerInfo,
  type ConnectResult,
  type PublicServerInfo,
  type ServerInfo
} from "../shared/ipc";
import * as auth from "./auth";
import { readSecret, writeSecret } from "./secureStore";
import {
  PangeaApiClient,
  AuthError,
  ConnectCancelledError,
  SubscriptionExpiredError
} from "./pangeaApiClient";
import type { HubShadowsocksCreds } from "../shared/hubShadowsocksCreds";
import type { CachedSubscription } from "../shared/cachedSubscription";
import { beginAttempt, cancelAttempt, commitAttempt, endAttempt, isCancelled } from "./connectAttempt";
import { setupAutoUpdater, notifyConnectionStateChange } from "./autoUpdater";
import { isSafeExternalUrl } from "./externalUrl";
import { setLoginItemEnabled, isLoginItemEnabled, isHiddenLaunchArg } from "./loginItem";
import { startNetworkWatcher, onNetworkChange } from "./networkWatcher";
import { statusNotificationKind, type StatusSnapshot } from "./statusNotifications";
import { mt, mtState, setMainLocale, resolveMainLocale } from "./i18n";
import { sanitizeLog } from "./logSanitize";
import { shouldReleaseKillSwitch } from "./killSwitchRelease";
import { shouldShowTrayHint, trayHintBodyKey } from "./trayHint";
import { anchorPosition, canAnchorWindow, samePoint, type AnchorRect } from "./windowAnchor";
import {
  applyHubMethod,
  isHubMethod,
  normalizeHubMethods,
  persistableHubMethods
} from "../shared/hubMethods";
import {
  buildServerRetryOrder as buildMainServerRetryOrder,
  runServerFallback
} from "./serverFallback";
import { shouldRotateServers } from "./serverRotation";
import { shouldRecoverFromNetworkChange } from "./networkRecovery";
import {
  commitProfileSet,
  dropExpired,
  forgetProfile,
  isLatestProvision,
  isReusable,
  parseProfileRecords,
  profileFingerprint,
  recordProvision,
  retainOnly,
  type ProfileRecords
} from "./profileCache";

let mainWindow: BrowserWindow | null = null;
let tray: Tray | null = null;
let isQuitting = false;
let trayStatusState: StatusResponse["state"] = "DISCONNECTED";
let trayStatusDetail = "idle";
let trayStatusReconnecting = false;
let trayStatusOffline = false;
let trayStatusKillSwitch = false;
let trayActionInProgress = false;
let trayStatusRefreshPromise: Promise<void> | null = null;
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
// Hub-registered WireGuard peers still worth reusing, keyed by profile id.
let provisionedProfiles: ProfileRecords = {};
let connectionAttemptRunning = false;
let allowLanEnabled = true;
let launchAtStartupEnabled = false;
// Two independent settings: Lockdown is the kill switch that stays armed while
// disconnected; auto-connect reconnects on launch and after drops. They shipped
// as one "alwaysConnected" toggle, migrated on load below.
let lockdownEnabled = false;
let autoConnectEnabled = false;
// OS notifications on connection status changes; on by default, off in Settings.
let notificationsEnabled = true;
// "auto" (reality, then cloak, shadowsocks, hysteria2, naive), or one of
// "cloak"/"naive"/"reality"/"hysteria2"/"shadowsocks"/"snowflake" only.
let preferredTransport: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard" = "auto";
// Stored language preference: a locale code, or "system" to follow the OS.
let localePref = "system";
const hiddenLaunch = process.argv.some(isHiddenLaunchArg);

// Auto-connect needs the tray app on boot to reconnect; Lockdown needs it so the
// user has a way to connect (or lift the lock) on a machine that boots blocked.
async function applyLoginItem(): Promise<void> {
  await setLoginItemEnabled(launchAtStartupEnabled || lockdownEnabled || autoConnectEnabled);
}

function getTrayBounds(): AnchorRect | null {
  if (!tray) {
    return null;
  }
  try {
    return tray.getBounds();
  } catch {
    return null;
  }
}

function getTaskbarPosition(): { x: number; y: number } {
  const { screen } = require("electron") as typeof import("electron");
  let cursor: { x: number; y: number } | null = null;
  try {
    cursor = screen.getCursorScreenPoint();
  } catch {
    cursor = null;
  }
  return anchorPosition({
    displays: screen.getAllDisplays(),
    primary: screen.getPrimaryDisplay(),
    cursor,
    trayBounds: getTrayBounds(),
    size: { width: setWidth, height: setHeight },
    platform: process.platform
  });
}

// Wayland refuses to let a client place its own window, so there the popover
// becomes an ordinary window that closes to the tray instead of a tray anchor.
const anchoredWindow = canAnchorWindow(process.env, process.platform, process.argv);

// Linux WMs place a frameless window where they like as they map it, and some
// keep nudging it after, so the anchor has to be re-asserted rather than set once.
const anchorReassertDelaysMs = [0, 60, 180, 400];
const anchorFixupBudget = 8;
let anchorTimers: NodeJS.Timeout[] = [];
let anchorFixupsLeft = 0;

function clearAnchorTimers(): void {
  for (const timer of anchorTimers) {
    clearTimeout(timer);
  }
  anchorTimers = [];
}

function applyAnchor(): void {
  if (!mainWindow || mainWindow.isDestroyed()) {
    return;
  }
  const pos = getTaskbarPosition();
  mainWindow.setPosition(pos.x, pos.y);
}

function keepAnchored(): void {
  if (process.platform !== "linux" || !anchoredWindow) {
    return;
  }
  clearAnchorTimers();
  anchorFixupsLeft = anchorFixupBudget;
  for (const delay of anchorReassertDelaysMs) {
    anchorTimers.push(
      setTimeout(() => {
        if (mainWindow?.isVisible() && !hiding) {
          applyAnchor();
        }
      }, delay)
    );
  }
}

function watchDisplayChanges(): void {
  if (!anchoredWindow) {
    return;
  }
  const { screen } = require("electron") as typeof import("electron");
  const reanchor = (): void => {
    if (mainWindow?.isVisible() && !hiding) {
      applyAnchor();
      keepAnchored();
    }
  };
  screen.on("display-added", reanchor);
  screen.on("display-removed", reanchor);
  screen.on("display-metrics-changed", reanchor);
}

function createWindow(): void {
  // Without an anchor the window is on its own in the window list, so it needs a
  // frame to move and close by, a taskbar entry, and an icon to be known by.
  const windowIconPath = anchoredWindow ? getWindowsAppIconPath(__dirname) : getTrayIconPath(__dirname);
  const pos = anchoredWindow ? getTaskbarPosition() : null;
  mainWindow = new BrowserWindow({
    width: setWidth,
    height: setHeight,
    ...(pos ? { x: pos.x, y: pos.y } : { center: true }),
    frame: !anchoredWindow,
    // Without this the titlebar eats into the 640x440 the layout is built for.
    useContentSize: !anchoredWindow,
    resizable: false,
    skipTaskbar: anchoredWindow,
    alwaysOnTop: anchoredWindow,
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

  mainWindow.loadFile(path.join(__dirname, "../renderer/index.html")).catch((err) => {
    console.error("failed to load renderer:", err);
  });

  mainWindow.webContents.on("render-process-gone", (_event, details) => {
    console.error("renderer process gone:", details.reason);
  });

  mainWindow.on("close", (event) => {
    if (isQuitting || !tray) {
      return;
    }
    event.preventDefault();
    hideMainWindow();
  });

  mainWindow.on("blur", () => {
    if (isQuitting || daemonRecoveryInProgress || !anchoredWindow) return;
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
      // Safety: if the show animation never finishes, hide anyway.
      setTimeout(() => {
        clearInterval(poll);
        checkAndHide();
      }, 500);
    } else {
      setTimeout(checkAndHide, 30);
    }
  });

  mainWindow.webContents.on("preload-error", (_event, preloadPath, error) => {
    console.error(`preload failed (${preloadPath}):`, error);
  });

  // A WM that drops the window elsewhere gets one correction per move, capped so
  // a tiling WM can't turn this into a fight.
  mainWindow.on("move", () => {
    if (process.platform !== "linux" || !anchoredWindow || hiding || anchorFixupsLeft <= 0) {
      return;
    }
    if (!mainWindow?.isVisible()) {
      return;
    }
    const [x, y] = mainWindow.getPosition();
    if (samePoint({ x, y }, getTaskbarPosition())) {
      return;
    }
    anchorFixupsLeft--;
    applyAnchor();
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
    // createWindow leaves the window hidden (show:false); finish the job.
    showMainWindow();
    return;
  }
  if (showing) return;
  hiding = false;
  showing = true;

  const pos = anchoredWindow ? getTaskbarPosition() : null;
  const useSlide = anchoredWindow && process.platform !== "linux";
  const slideOffset = process.platform === "darwin" ? -20 : 20;
  const startY = pos && useSlide ? pos.y + slideOffset : pos?.y ?? 0;

  mainWindow.setOpacity(0);
  if (pos) {
    mainWindow.setBounds({ x: pos.x, y: startY, width: setWidth, height: setHeight });
  }

  if (mainWindow.isMinimized()) {
    mainWindow.restore();
  }
  if (!mainWindow.isVisible()) {
    mainWindow.show();
  }
  mainWindow.focus();
  keepAnchored();

  const duration = 180;
  const steps = 12;
  const interval = duration / steps;
  let step = 0;

  const timer = setInterval(() => {
    step++;
    const t = step / steps;
    const ease = 1 - Math.pow(1 - t, 3);
    mainWindow?.setOpacity(ease);
    if (useSlide && pos) {
      mainWindow?.setBounds({ x: pos.x, y: Math.round(startY + (pos.y - startY) * ease), width: setWidth, height: setHeight });
    }

    if (step >= steps) {
      clearInterval(timer);
      mainWindow?.setOpacity(1);
      if (pos) {
        mainWindow?.setBounds({ x: pos.x, y: pos.y, width: setWidth, height: setHeight });
      }
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
  const useSlide = anchoredWindow && process.platform !== "linux";
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
      clearAnchorTimers();
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

  await updateSettings((settings) => {
    settings.trayHintShown = true;
  }, "tray hint flag");
}

function toggleMainWindowVisibility(): void {
  if (!mainWindow || !mainWindow.isVisible()) {
    showMainWindow();
    return;
  }
  if (!anchoredWindow && !mainWindow.isFocused()) {
    mainWindow.show();
    mainWindow.focus();
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

  // While the kill switch holds traffic, the raw daemon detail can't tell the
  // user why their internet is down or that Disconnect is the way out.
  const killSwitchHolding = trayStatusState === "ERROR" && trayStatusKillSwitch;
  const detailLabel = killSwitchHolding ? mt("tray.blocked") : trayStatusDetail.trim() || "-";
  const canConnect = !trayActionInProgress && (trayStatusState === "DISCONNECTED" || trayStatusState === "ERROR");
  const canDisconnect =
    !trayActionInProgress && (trayStatusState === "CONNECTED" || trayStatusState === "CONNECTING" || killSwitchHolding);
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

let lastNotifiedStatus: StatusSnapshot | null = null;

// One notification per transition, decided by the pure table in
// statusNotifications.ts; silent so a status flap never dings the user.
function maybeNotifyStatusChange(): void {
  const next: StatusSnapshot = {
    state: trayStatusState,
    reconnecting: trayStatusReconnecting,
    killSwitchActive: trayStatusKillSwitch
  };
  const prev = lastNotifiedStatus;
  lastNotifiedStatus = next;
  if (!notificationsEnabled || !Notification.isSupported()) return;
  // The status is already on screen when the window is open, so don't also toast
  // it. lastNotifiedStatus is updated above, so nothing fires late on re-hide.
  if (mainWindow && mainWindow.isVisible() && !mainWindow.isMinimized()) return;
  const kind = statusNotificationKind(prev, next);
  if (!kind) return;
  const iconPath = process.platform === "darwin" ? undefined : getTrayIconPath(__dirname);
  const notification = new Notification({
    title: mt(`notify.${kind}Title`),
    body: mt(`notify.${kind}Body`),
    // Blocked internet is the one status worth a ding: silence here is how a
    // user ends up debugging their router instead of pressing Disconnect.
    silent: kind !== "blocking",
    ...(iconPath ? { icon: iconPath } : {})
  });
  notification.on("click", () => showMainWindow());
  notification.show();
}

async function refreshTrayStatus(): Promise<void> {
  // Dedupe concurrent callers onto the same in-flight refresh instead of one
  // returning immediately with a stale value while another is still fetching.
  if (trayStatusRefreshPromise) {
    return trayStatusRefreshPromise;
  }

  const run = (async () => {
    try {
      const status = await withDaemonRestartOnUnavailable(() => daemonClient.getStatus(), "tray status", { allowRestart: false });
      trayStatusState = status.state;
      trayStatusDetail = status.detail;
      trayStatusReconnecting = status.reconnecting;
      trayStatusOffline = status.offline;
      trayStatusKillSwitch = status.killSwitchActive;
      if (status.transportsExhausted) {
        void rotateAwayFromBlockedServer();
      }
    } catch {
      trayStatusState = "ERROR";
      trayStatusDetail = "daemon unavailable";
      trayStatusReconnecting = false;
    } finally {
      maybeNotifyStatusChange();
      updateTrayMenu();
      notifyConnectionStateChange(trayStatusState);
    }
  })();
  trayStatusRefreshPromise = run.finally(() => {
    trayStatusRefreshPromise = null;
  });
  return trayStatusRefreshPromise;
}

let serverRotationInFlight = false;
let lastServerRotationAtMs = 0;

/** Reconnects elsewhere once no transport gets through this server: the daemon
 *  walks transports, only the app can walk servers. */
async function rotateAwayFromBlockedServer(): Promise<void> {
  const blockedServerId = lastServerId;
  if (
    !shouldRotateServers({
      transportsExhausted: true,
      connectionAttemptRunning,
      rotationInFlight: serverRotationInFlight,
      lastRotationAtMs: lastServerRotationAtMs,
      nowMs: Date.now()
    })
  ) {
    return;
  }

  serverRotationInFlight = true;
  lastServerRotationAtMs = Date.now();
  try {
    const plan = await resolveTrayServerPlan(blockedServerId);
    if (!plan) {
      console.warn("rotation: no other server to try");
      return;
    }
    console.warn(`rotation: ${blockedServerId ?? "current server"} carries no transport; trying another server`);
    const result = await provisionAndConnect(plan);
    if (!result.ok) {
      console.warn("rotation: reconnecting on another server failed", result.error);
    }
  } catch (error) {
    console.warn("rotation: reconnecting on another server failed", sanitizeLog(error));
  } finally {
    serverRotationInFlight = false;
  }
}

let networkRecoverInProgress = false;
let lastNetworkRecoverAtMs = 0;
// Set by an explicit Disconnect, cleared by any deliberate connect: the
// renderer honors this intent, so main's own recovery must too.
let userDisconnected = false;

async function recoverFromNetworkChange(): Promise<void> {
  const profileId = lastConnectedProfileId;
  if (
    profileId === null ||
    !shouldRecoverFromNetworkChange({
      autoConnectEnabled,
      userDisconnected,
      lastConnectedProfileId: profileId,
      connectionAttemptRunning,
      recoverInProgress: networkRecoverInProgress,
      lastRecoverAtMs: lastNetworkRecoverAtMs,
      nowMs: Date.now()
    })
  ) {
    return;
  }

  networkRecoverInProgress = true;
  connectionAttemptRunning = true;
  // Tracked as a cancellable attempt so a user Disconnect (which calls
  // cancelAttempt) can interrupt this cascade instead of racing past it.
  const attempt = beginAttempt();
  try {
    // Refresh status first so we don't fire over an already-healthy tunnel.
    await refreshTrayStatus();
    if (trayStatusState === "CONNECTED" || trayStatusState === "CONNECTING") {
      return;
    }
    // The disconnect below would cancel the daemon's own cascade mid-transport.
    if (trayStatusReconnecting) return;
    // No physical link: the daemon is holding, so don't churn connect attempts
    // into a dead network — the daemon resumes on its own when a link returns.
    if (trayStatusOffline) return;
    if (isCancelled(attempt)) return;
    // Stamped only when a reconnect actually runs: the AP-loss event arrives
    // while the daemon still reports CONNECTED, and burning the cooldown on
    // that no-op would skip the AP-up event that follows seconds later.
    lastNetworkRecoverAtMs = Date.now();
    console.log("network change detected — attempting reconnect");
    // Tear down stale tunnel/firewall state, keeping the kill switch when
    // Lockdown is on, then bring the tunnel back on the new network.
    try {
      await daemonClient.disconnect({ keepKillSwitch: lockdownEnabled });
    } catch (err) {
      console.warn("network recover: disconnect failed", sanitizeLog(err));
    }
    if (isCancelled(attempt)) return;
    const result = await connectWithRecovery(profileId);
    if (!result.ok) {
      console.warn("network recover: reconnect failed", sanitizeLog((result as { error?: string }).error));
      await releaseFailClosedKillSwitch();
    }
  } catch (err) {
    console.warn("network recover: unexpected error", sanitizeLog(err));
  } finally {
    endAttempt(attempt);
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
async function connectExistingProfile(attempt: ReturnType<typeof beginAttempt>): Promise<boolean> {
  const profileId = lastConnectedProfileId ?? managedProfileId;
  if (!profileId) return false;

  const config = await withDaemonRestartOnUnavailable(
    () => daemonClient.getConfig(),
    "tray config",
    { allowRestart: false }
  );
  if (!config.profiles.some((profile) => profile.id === profileId)) return false;

  if (isCancelled(attempt)) return false;
  const result = await connectWithRecovery(profileId);
  // Stop can land while the daemon is mid-connect, and this path has no hub
  // call left to abort — so the tunnel has to be taken back down here.
  if (isCancelled(attempt)) {
    if (result.ok) {
      await daemonClient
        .disconnect({ keepKillSwitch: lockdownEnabled })
        .catch((err) => console.warn("cancel: fallback teardown failed", sanitizeLog(err)));
    }
    return false;
  }
  if (!result.ok) return false;
  lastConnectedProfileId = profileId;
  void persistLastConnection();
  return true;
}

async function reconnectExistingProfile(): Promise<boolean> {
  if (connectionAttemptRunning) return false;

  connectionAttemptRunning = true;
  // Tracked as a cancellable attempt, or Stop has nothing to interrupt here.
  const attempt = beginAttempt();
  try {
    return await connectExistingProfile(attempt);
  } finally {
    endAttempt(attempt);
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

  userDisconnected = false;
  trayActionInProgress = true;
  updateTrayMenu();
  // Tracks whether we set ERROR ourselves, so the finally below doesn't let
  // a daemon refresh silently overwrite it with a normal idle state.
  let explicitFailure = false;
  try {
    if (connectionAttemptRunning) {
      // A cascade already owns the connect; don't fall through into a
      // doomed provision attempt and report a failure that never happened.
      return;
    }
    let exhaustedServerId: string | null = null;
    try {
      if (await reconnectExistingProfile()) return;
    } catch (error) {
      // Parked until the network returns; the status refresh shows it.
      if (error instanceof HostOfflineError) return;
      if (!(error instanceof TransportExhaustedError)) throw error;
      exhaustedServerId = lastServerId;
    }

    const serverPlan = await resolveTrayServerPlan(exhaustedServerId);
    if (!serverPlan) {
      trayStatusState = "ERROR";
      trayStatusDetail = "no server available";
      explicitFailure = true;
      return;
    }

    const result = await provisionAndConnect(serverPlan);
    if (!result.ok) {
      trayStatusState = "ERROR";
      trayStatusDetail = "connect request failed";
      explicitFailure = true;
    }
  } catch (error) {
    console.warn("tray connect failed", sanitizeLog(error));
    trayStatusState = "ERROR";
    trayStatusDetail = "connect failed";
    explicitFailure = true;
  } finally {
    trayActionInProgress = false;
    if (explicitFailure) {
      updateTrayMenu();
    } else {
      await refreshTrayStatus();
    }
  }
}

async function disconnectFromTray(): Promise<void> {
  if (trayActionInProgress) {
    return;
  }

  userDisconnected = true;
  // Same as the renderer's Disconnect: an in-flight cascade would otherwise
  // bring the tunnel straight back up after the daemon disconnect.
  cancelAttempt();
  trayActionInProgress = true;
  updateTrayMenu();
  let explicitFailure = false;
  try {
    const result = await withDaemonRestartOnUnavailable(
      () => daemonClient.disconnect({ keepKillSwitch: lockdownEnabled }),
      "tray disconnect"
    );
    if (!result.ok) {
      trayStatusState = "ERROR";
      trayStatusDetail = "disconnect request failed";
      explicitFailure = true;
    }
  } catch (error) {
    console.warn("tray disconnect failed", sanitizeLog(error));
    trayStatusState = "ERROR";
    trayStatusDetail = "disconnect failed";
    explicitFailure = true;
  } finally {
    trayActionInProgress = false;
    if (explicitFailure) {
      updateTrayMenu();
    } else {
      await refreshTrayStatus();
    }
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
  if (!lockdownEnabled) return;
  const hubIp = pangeaApiClient.getHubIp();
  try {
    await daemonClient.permitHosts(hubIp ? [hubIp] : []);
  } catch (err) {
    console.warn("lockdown: hub permit failed", sanitizeLog(err));
  }
}

function fingerprintForServer(serverId: string): string {
  return profileFingerprint({
    wireguardMtu: pangeaApiClient.getWireguardMtu(),
    customDns: pangeaApiClient.getCustomDns(),
    hubInTunnel: pangeaApiClient.getHubInTunnel(),
    server: pangeaApiClient.getCachedServers().find((server) => server.id === serverId) ?? null
  });
}

/**
 * The peer the hub registered for this server on an earlier run, when it is
 * still inside its TTL and was built from the same inputs. Reusing it turns a
 * reconnect into zero hub round trips; the caller re-provisions if it fails.
 */
async function reusableProfileForServer(serverId: string): Promise<Profile | null> {
  const profileId = `auto-${serverId}`;
  if (!isReusable(provisionedProfiles[profileId], fingerprintForServer(serverId), Date.now())) {
    return null;
  }
  // Registering any other server evicted this peer hub-side; dialling it would
  // burn the daemon's whole transport cascade on handshakes no node answers.
  if (!isLatestProvision(provisionedProfiles, profileId)) {
    return null;
  }
  try {
    const config = await withDaemonRestartOnUnavailable(
      () => daemonClient.getConfig(),
      "reuse-config",
      { allowRestart: false }
    );
    return config.profiles.find((profile) => profile.id === profileId) ?? null;
  } catch (err) {
    console.warn("reuse: could not read daemon config", sanitizeLog(err));
    return null;
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
  rememberProvisionedProfile(profile.id, serverId);
  return profile;
}

function rememberProvisionedProfile(profileId: string, serverId: string): void {
  provisionedProfiles = recordProvision(provisionedProfiles, profileId, {
    serverId,
    provisionedAt: Date.now(),
    fingerprint: fingerprintForServer(serverId)
  });
  void persistProvisionedProfiles();
}

function forgetProvisionedProfile(profileId: string): void {
  provisionedProfiles = forgetProfile(provisionedProfiles, profileId);
  void persistProvisionedProfiles();
}

/** Forgets every cached peer and strips them from the daemon's config. */
async function discardProvisionedProfiles(): Promise<void> {
  const cachedIds = Object.keys(provisionedProfiles);
  provisionedProfiles = {};
  await persistProvisionedProfiles();
  if (cachedIds.length === 0) return;
  try {
    const config = await daemonClient.getConfig();
    await daemonClient.setConfig(config.profiles.filter((p) => !cachedIds.includes(p.id)));
  } catch (err) {
    console.warn("could not drop cached peers from the daemon config", sanitizeLog(err));
  }
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

interface DialOutcome {
  profile: Profile;
  result: OkResponse;
}

async function dialServer(
  profile: Profile,
  index: number,
  mode: "connect" | "switch",
  attempt: ReturnType<typeof beginAttempt>
): Promise<DialOutcome> {
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
        .disconnect({ keepKillSwitch: lockdownEnabled })
        .catch((err) => console.warn("cancel: disconnect failed", sanitizeLog(err)));
    }
    throw new ConnectCancelledError();
  }
  return { profile, result };
}

/** Null when the cached peer did not get us connected, having forgotten it. */
async function dialReusedProfile(
  profile: Profile,
  index: number,
  mode: "connect" | "switch",
  attempt: ReturnType<typeof beginAttempt>
): Promise<DialOutcome | null> {
  let outcome: DialOutcome;
  try {
    outcome = await dialServer(profile, index, mode, attempt);
  } catch (err) {
    if (err instanceof ConnectCancelledError || err instanceof HostOfflineError || isCancelled(attempt)) throw err;
    console.warn(`reuse: ${profile.id} failed, re-provisioning`, sanitizeLog(err));
    forgetProvisionedProfile(profile.id);
    return null;
  }
  if (outcome.result.ok) return outcome;
  forgetProvisionedProfile(profile.id);
  return null;
}

async function provisionAcrossServers(serverIds: readonly string[], mode: "connect" | "switch"): Promise<ConnectResult> {
  if (connectionAttemptRunning) {
    return { ok: false, error: "connect-in-progress" };
  }
  connectionAttemptRunning = true;
  // A user-driven attempt outranks the cooldown from the last failed cascade.
  pangeaApiClient.retryHubNow();
  const attempt = beginAttempt();
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
        const reused = await reusableProfileForServer(serverId);
        if (reused) {
          // The hub may have dropped the peer behind our back, so a failure
          // here re-provisions the same server rather than cascading away.
          const outcome = await dialReusedProfile(reused, index, mode, attempt);
          if (outcome) return outcome;
        }

        if (isCancelled(attempt)) throw new ConnectCancelledError();
        const profile = await provisionProfileForServer(serverId, attempt.controller.signal);
        configChanged = true;
        return await dialServer(profile, index, mode, attempt);
      },
      (error) => preferredTransport === "auto" && error instanceof TransportExhaustedError
    );

    if (outcome.value.result.ok) {
      provisionedProfiles = dropExpired(provisionedProfiles, Date.now());
      const committedProfiles = commitProfileSet(
        initialProfiles,
        outcome.value.profile,
        Object.keys(provisionedProfiles)
      );
      await daemonClient.setConfig(committedProfiles).catch((error) => {
        console.warn("connect: profile cleanup failed", sanitizeLog(error));
      });
      provisionedProfiles = retainOnly(provisionedProfiles, committedProfiles.map((p) => p.id));
      void persistProvisionedProfiles();
      if (isCancelled(attempt)) {
        await daemonClient
          .disconnect({ keepKillSwitch: lockdownEnabled })
          .catch((error) => console.warn("cancel: disconnect after cleanup failed", sanitizeLog(error)));
        throw new ConnectCancelledError();
      }
      committed = true;
      commitAttempt(attempt);
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
    // The daemon is holding the session (kill switch armed) and connects
    // itself once the host has a network; nothing here to unwind or retry.
    if (err instanceof HostOfflineError) {
      return { ok: false, error: "offline" };
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
    if (!committed) {
      await releaseFailClosedKillSwitch();
    }
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
    let reconnected: boolean;
    try {
      reconnected = await reconnectExistingProfile();
    } catch (fallbackErr) {
      if (fallbackErr instanceof HostOfflineError) return { ok: false, error: "offline" };
      throw fallbackErr;
    }
    if (reconnected) {
      return { ok: true, ...(lastServerId ? { serverId: lastServerId } : {}) };
    }
    await releaseFailClosedKillSwitch();
    throw err;
  }
}

async function provisionAndSwitch(serverIds: readonly string[]): Promise<ConnectResult> {
  return provisionAcrossServers(serverIds, "switch");
}

// The daemon is fail-closed, so a cascade that gave up leaves the switch armed
// with no tunnel to disconnect from. Lockdown means the user wanted the block.
async function releaseFailClosedKillSwitch(): Promise<void> {
  if (lockdownEnabled) return;
  try {
    const status = await daemonClient.getStatus();
    if (!shouldReleaseKillSwitch(status, lockdownEnabled)) return;
    await daemonClient.clearKillSwitch();
    console.warn("cleared the kill switch a failed connect left armed");
  } catch (err) {
    console.warn("could not clear the kill switch after a failed connect", sanitizeLog(err));
  }
}

/** Stop the in-flight connect attempt; never tears down a wanted connection. */
async function cancelConnectAttempt(): Promise<void> {
  const cancelled = cancelAttempt();
  if (!cancelled) return;

  // Straight to disconnect, with no status round-trip first: the daemon
  // interrupts its own in-flight connect, and a redundant one is a no-op there.
  try {
    await daemonClient.disconnect({ keepKillSwitch: lockdownEnabled });
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
    lockdown: lockdownEnabled,
    ...(preferredTransport !== "auto" && { preferredTransport })
  };
}

function getSettingsPath(): string {
  return path.join(getUserStateDir(), "settings.json");
}

// Reads a desktop state file, falling back to the copy the daemon's directory
// may still hold from before these moved to the user directory.
async function readStateFile(fileName: string): Promise<string> {
  const fs = (await import("node:fs/promises")).default;
  try {
    return await fs.readFile(path.join(getUserStateDir(), fileName), "utf8");
  } catch (err) {
    const legacyPath = getLegacyStateFilePath(fileName);
    if (!legacyPath) {
      throw err;
    }
    return await fs.readFile(legacyPath, "utf8");
  }
}

// Settings written before the move out of the daemon's directory. Read-only:
// the next write lands in the user directory and takes over from there.
async function readLegacySettingsFile(): Promise<Record<string, unknown>> {
  const legacyPath = getLegacyStateFilePath("settings.json");
  if (!legacyPath) {
    return {};
  }
  try {
    const fs = (await import("node:fs/promises")).default;
    return JSON.parse(await fs.readFile(legacyPath, "utf8")) as Record<string, unknown>;
  } catch {
    return {};
  }
}

async function readSettingsFile(): Promise<Record<string, unknown>> {
  const filePath = getSettingsPath();
  const fs = (await import("node:fs/promises")).default;
  let raw: string;
  try {
    raw = await fs.readFile(filePath, "utf8");
  } catch {
    // No file here yet: either a fresh install or one that predates the move
    // off the daemon's directory, which the desktop cannot write to.
    return readLegacySettingsFile();
  }
  try {
    return JSON.parse(raw) as Record<string, unknown>;
  } catch (err) {
    // Corrupt, not missing: don't let the caller's write-back treat this as
    // "no settings yet" and silently erase everything that was in it.
    console.error("settings.json is corrupt; preserving it as .corrupt and starting fresh:", sanitizeLog(err));
    await fs.rename(filePath, `${filePath}.corrupt-${Date.now()}`).catch(() => {});
    return {};
  }
}

async function writeSettingsFile(settings: Record<string, unknown>): Promise<void> {
  const dir = await (await import("./platformPaths")).ensureUserStateDir();
  const fs = (await import("node:fs/promises")).default;
  const finalPath = path.join(dir, "settings.json");
  const tmpPath = `${finalPath}.tmp-${process.pid}-${Date.now()}`;
  // Write-then-rename: a crash or power loss mid-write leaves the old file
  // intact instead of a truncated one, since rename is atomic on both OSes.
  await fs.writeFile(tmpPath, JSON.stringify(settings, null, 2));
  await fs.rename(tmpPath, finalPath);
}

let settingsWriteChain: Promise<void> = Promise.resolve();

// Serializes every settings.json read-modify-write behind one queue, so two
// unserialized writers can't interleave and resurrect a key the other deleted.
function updateSettings(mutate: (settings: Record<string, unknown>) => void, label: string): Promise<void> {
  const run = settingsWriteChain
    .catch(() => {})
    .then(async () => {
      const settings = await readSettingsFile();
      mutate(settings);
      await writeSettingsFile(settings);
    })
    .catch((err) => {
      console.warn(`Failed to persist ${label}:`, sanitizeLog(err));
    });
  settingsWriteChain = run;
  return run;
}

/** Writes both flags together and drops the pre-split `alwaysConnected` key. */
async function persistStartupSettings(): Promise<void> {
  await updateSettings((settings) => {
    settings.lockdown = lockdownEnabled;
    settings.autoConnect = autoConnectEnabled;
    settings.notifications = notificationsEnabled;
    delete settings.alwaysConnected;
  }, "lockdown/auto-connect settings");
}

async function persistHubIp(ip: string): Promise<void> {
  await updateSettings((settings) => {
    settings.hubIp = ip;
  }, "hub IP");
}

async function persistHubShadowsocks(creds: HubShadowsocksCreds[]): Promise<void> {
  await updateSettings((settings) => {
    settings.hubShadowsocks = creds;
  }, "hub Shadowsocks credentials");
}

async function persistFrontedEndpoints(endpoints: string[]): Promise<void> {
  await updateSettings((settings) => {
    settings.frontedEndpoints = endpoints;
  }, "fronted endpoints");
}

async function persistDeadDropState(state: { seq: number; lastAttemptMs: number }): Promise<void> {
  await updateSettings((settings) => {
    settings.deadDropSeq = state.seq;
    settings.deadDropLastAttempt = state.lastAttemptMs;
  }, "dead drop state");
}

/** The node list, so a client that cannot reach the hub still knows where the
 *  servers are. Cleared on logout, which passes an empty list. */
async function persistServers(servers: ServerInfo[]): Promise<void> {
  await updateSettings((settings) => {
    if (servers.length === 0) {
      delete settings.servers;
    } else {
      settings.servers = servers;
    }
  }, "server list");
}

/** Reconstructs a clean object from known-safe fields only, so stray
 *  properties (e.g. leftover credentials from an older cache format) can
 *  never survive a read or write of the renderer-facing server cache. */
const maxSetConfigProfiles = 64;

function sanitizePublicServer(candidate: unknown): PublicServerInfo | null {
  const s = candidate as Partial<PublicServerInfo> | null;
  if (!s || typeof s !== "object" || Array.isArray(s)) return null;
  if (typeof s.id !== "string" || s.id.trim().length === 0) return null;
  if (typeof s.name !== "string" || s.name.trim().length === 0) return null;
  if (typeof s.region !== "string" || typeof s.country !== "string") return null;
  return {
    id: s.id,
    name: s.name,
    region: s.region,
    country: s.country,
    load: typeof s.load === "number" ? s.load : null,
    naive: Boolean(s.naive),
    reality: Boolean(s.reality),
    hysteria2: Boolean(s.hysteria2),
    shadowsocks: Boolean(s.shadowsocks),
    snowflake: Boolean(s.snowflake)
  };
}

/** Validated, deduplicated (by id, order preserved) list for the on-disk
 *  renderer-facing server cache — never trust a user-writable file blind. */
function sanitizePublicServers(stored: unknown): PublicServerInfo[] {
  if (!Array.isArray(stored)) return [];
  const out: PublicServerInfo[] = [];
  for (const candidate of stored) {
    const safe = sanitizePublicServer(candidate);
    if (!safe) continue;
    if (out.some((s) => s.id === safe.id)) continue;
    out.push(safe);
  }
  return out;
}

/** Blanks WireGuard keys and per-transport passwords before a daemon config
 *  crosses into the renderer, which only ever displays it in diagnostics. */
// The renderer is untrusted input to a privileged daemon, so validate the
// shape here; shared-types is ESM and cannot be required from CommonJS main.
function asProfilePayload(value: unknown): Profile[] | null {
  if (!Array.isArray(value) || value.length > maxSetConfigProfiles) {
    return null;
  }
  for (const entry of value) {
    if (!entry || typeof entry !== "object" || Array.isArray(entry)) {
      return null;
    }
    const candidate = entry as { id?: unknown; name?: unknown };
    if (typeof candidate.id !== "string" || candidate.id.length === 0) {
      return null;
    }
    if (typeof candidate.name !== "string" || candidate.name.length === 0) {
      return null;
    }
  }
  return value as Profile[];
}

function redactConfigForRenderer(config: ConfigResponse): ConfigResponse {
  return {
    ...config,
    profiles: config.profiles.map((profile) => ({
      ...profile,
      cloak: { ...profile.cloak, uid: "<redacted>", password: "<redacted>" },
      naive: profile.naive ? { ...profile.naive, password: "<redacted>" } : profile.naive,
      reality: profile.reality
        ? { ...profile.reality, uuid: "<redacted>", shortId: "<redacted>" }
        : profile.reality,
      hysteria2: profile.hysteria2
        ? { ...profile.hysteria2, password: "<redacted>", obfsPassword: "<redacted>" }
        : profile.hysteria2,
      shadowsocks: profile.shadowsocks
        ? { ...profile.shadowsocks, password: "<redacted>" }
        : profile.shadowsocks,
      wireguard: { ...profile.wireguard, configText: redactWireGuardKeys(profile.wireguard.configText) }
    }))
  };
}

function redactWireGuardKeys(configText: string): string {
  return configText
    .split(/\r?\n/)
    .map((line) => {
      if (/^\s*PrivateKey\s*=/.test(line) || /^\s*PresharedKey\s*=/.test(line)) {
        const separator = line.includes("=") ? "=" : " =";
        return `${line.split("=")[0]?.trim() ?? "Key"} ${separator} <redacted>`;
      }
      return line;
    })
    .join("\n");
}

async function persistProvisionedProfiles(): Promise<void> {
  await updateSettings((settings) => {
    settings.provisionedProfiles = provisionedProfiles;
  }, "provisioned profiles");
}

/** The hub's last word on the plan, so an unreachable hub does not present a
 *  paying user with "no subscription". Cleared on logout, which passes null. */
async function persistSubscription(cached: CachedSubscription | null): Promise<void> {
  await updateSettings((settings) => {
    if (cached === null) {
      delete settings.subscription;
    } else {
      settings.subscription = cached;
    }
  }, "subscription");
}

/** Refreshes the cached subscription in the background; never throws. Called at
 *  every point the answer could have changed: launch, sign-in, connect. */
function refreshSubscriptionCache(reason: string): void {
  if (!pangeaApiClient.getLicenseKey()) return;
  void pangeaApiClient.getSubscription().catch((err: unknown) => {
    console.warn(`subscription refresh (${reason}) failed:`, sanitizeLog(err));
  });
}

async function persistLastConnection(): Promise<void> {
  await updateSettings((settings) => {
    settings.lastServerId = lastServerId;
    settings.lastProfileId = lastConnectedProfileId;
  }, "last connection");
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
  ipcMain.handle(IPC_CHANNELS.connect, async (_event, profileId: unknown) => {
    if (typeof profileId !== "string" || profileId.trim() === "") {
      return { ok: false };
    }
    if (connectionAttemptRunning) {
      return { ok: false };
    }
    userDisconnected = false;
    connectionAttemptRunning = true;
    const attempt = beginAttempt();
    try {
      // The daemon is the authority on which profiles exist; refuse to chase
      // a profileId it doesn't recognize.
      const config = await withDaemonRestartOnUnavailable(
        () => daemonClient.getConfig(),
        "connect-profile-check",
        { allowRestart: false }
      );
      if (!config.profiles.some((profile) => profile.id === profileId)) {
        return { ok: false };
      }
      if (isCancelled(attempt)) {
        return { ok: false };
      }
      const result = await connectWithRecovery(profileId);
      if (isCancelled(attempt)) {
        // Stop landed while the daemon was connecting; it can't be un-sent.
        if (result.ok) {
          await daemonClient
            .disconnect({ keepKillSwitch: lockdownEnabled })
            .catch((err) => console.warn("cancel: connect teardown failed", sanitizeLog(err)));
        }
        return { ok: false };
      }
      if (result.ok) {
        lastConnectedProfileId = profileId;
        void persistLastConnection();
        // The tunnel is up, so the hub is reachable through it even when it was
        // not before — the most reliable point to re-cache the renewal date.
        refreshSubscriptionCache("connect");
      } else {
        await releaseFailClosedKillSwitch();
      }
      return result;
    } finally {
      endAttempt(attempt);
      connectionAttemptRunning = false;
      void refreshTrayStatus();
    }
  });
  ipcMain.handle(IPC_CHANNELS.disconnect, async () => {
    userDisconnected = true;
    // Stop any main-process cascade (network recovery, launch auto-connect)
    // so it can't silently reconnect moments after the user's Disconnect.
    cancelAttempt();
    const result = await withDaemonRestartOnUnavailable(
      () => daemonClient.disconnect({ keepKillSwitch: lockdownEnabled }),
      "disconnect"
    );
    void refreshTrayStatus();
    return result;
  });
  ipcMain.handle(IPC_CHANNELS.getLogs, async (_event, since?: number) =>
    withDaemonRestartOnUnavailable(() => daemonClient.getLogs(since), "logs", { allowRestart: false })
  );
  ipcMain.handle(IPC_CHANNELS.getConfig, async () => {
    const config = await withDaemonRestartOnUnavailable(() => daemonClient.getConfig(), "config", { allowRestart: false });
    return redactConfigForRenderer(config);
  });
  ipcMain.handle(IPC_CHANNELS.setConfig, async (event, profiles: unknown) => {
    if (!event.senderFrame || !event.senderFrame.url.startsWith("file://")) {
      throw new Error("setConfig: untrusted sender");
    }
    const parsed = asProfilePayload(profiles);
    if (!parsed) {
      throw new Error("Invalid profiles payload");
    }
    return withDaemonRestartOnUnavailable(() => daemonClient.setConfig(parsed), "setConfig");
  });
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

  ipcMain.handle(IPC_CHANNELS.openExternal, async (_event, url: string) => {
    const { shell } = await import("electron");
    if (isSafeExternalUrl(url)) {
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

      // Registration succeeded — persist identity keypair and set on API client.
      // A failure past this point must not leave the hub's device slot orphaned.
      try {
        await auth.saveIdentityKeyPair({ privateKey: identityPrivateKey, publicKey: identityPublicKey });
        pangeaApiClient.identityPubkey = identityPublicKey;

        const authState = await auth.loginWithToken(data.vpnAccessToken, data.user);
        // The hub is demonstrably reachable right now — the cheapest moment this
        // device will ever get to learn its renewal date.
        refreshSubscriptionCache("sign-in");
        return { ...authState, friendlyName: effectiveFriendlyName };
      } catch (postRegErr) {
        console.warn("post-registration setup failed:", sanitizeLog(postRegErr));
        await pangeaApiClient.deregisterDevice(identityPublicKey).catch(() => {});
        await auth.clearLicenseKey();
        pangeaApiClient.clearCache();
        return { authenticated: false, user: null };
      }
    } catch (err) {
      console.warn("token login failed:", sanitizeLog(err));
      return { authenticated: false, user: null };
    }
  });

  ipcMain.handle(IPC_CHANNELS.authLogout, async () => {
    // Stop any in-flight cascade first, or it can resume after we've cleared
    // the license key and claim a managed profile for the wrong account.
    cancelAttempt();

    try {
      const status = await daemonClient.getStatus();
      if (status.state === "CONNECTED" || status.state === "CONNECTING") {
        await daemonClient.disconnect({ keepKillSwitch: lockdownEnabled });
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

    // The cached peers belong to the account that is signing out.
    await discardProvisionedProfiles();
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
    await updateSettings((settings) => {
      settings.dohEnabled = enabled;
    }, "DoH setting");
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
    await updateSettings((settings) => {
      // Stamped with the rev, so this deliberate choice is not overwritten by
      // the next default change the way a pre-rev file's would be.
      settings.hubMethods = persistableHubMethods(methods);
      // Drop the keys this replaced so a later downgrade cannot resurrect them.
      delete settings.directIpEnabled;
      delete settings.directIpOnly;
    }, "hubMethods setting");
    return { methods, applied: true };
  });

  ipcMain.handle(IPC_CHANNELS.setDeadDrop, async (_event, enabled: unknown) => {
    const on = enabled === true;
    pangeaApiClient.setDeadDropEnabled(on);
    await updateSettings((settings) => {
      settings.deadDrop = on;
    }, "dead drop setting");
  });

  ipcMain.handle(IPC_CHANNELS.getDeadDrop, async () => pangeaApiClient.getDeadDropEnabled());

  ipcMain.handle(IPC_CHANNELS.getHubMethods, async () => pangeaApiClient.getHubMethods());

  ipcMain.handle(IPC_CHANNELS.getHubStatus, async () => pangeaApiClient.getHubStatus());

  // Probes one method on demand. Rejecting an unknown name rather than
  // defaulting keeps a stray IPC call from starting the daemon's proxy.
  ipcMain.handle(IPC_CHANNELS.testHubMethod, async (_event, method: unknown) => {
    if (!isHubMethod(method)) {
      throw new Error("Unknown hub method");
    }
    return pangeaApiClient.testHubMethod(method);
  });

  ipcMain.handle(IPC_CHANNELS.setAllowLan, async (_event, enabled: boolean) => {
    allowLanEnabled = !!enabled;
    await updateSettings((settings) => {
      settings.allowLan = allowLanEnabled;
    }, "allowLan setting");
  });

  ipcMain.handle(IPC_CHANNELS.getAllowLan, async () => allowLanEnabled);

  // Returns the stored MTU, which differs from the requested one when it was
  // rejected — the renderer uses that mismatch to flag invalid input.
  ipcMain.handle(IPC_CHANNELS.setWireguardMtu, async (_event, mtu: unknown) => {
    const stored = pangeaApiClient.setWireguardMtu(mtu);
    await updateSettings((settings) => {
      settings.wireguardMtu = stored;
    }, "wireguardMtu setting");
    return stored;
  });

  ipcMain.handle(IPC_CHANNELS.getWireguardMtu, async () => pangeaApiClient.getWireguardMtu());

  ipcMain.handle(IPC_CHANNELS.setCustomDns, async (_event, value: unknown) => {
    const stored = pangeaApiClient.setCustomDns(value);
    await updateSettings((settings) => {
      settings.customDns = stored;
    }, "customDns setting");
    return stored;
  });

  ipcMain.handle(IPC_CHANNELS.getCustomDns, async () => pangeaApiClient.getCustomDns());

  ipcMain.handle(IPC_CHANNELS.setHubInTunnel, async (_event, enabled: unknown) => {
    pangeaApiClient.setHubInTunnel(enabled === true);
    await updateSettings((settings) => {
      settings.hubInTunnel = pangeaApiClient.getHubInTunnel();
    }, "hubInTunnel setting");
  });

  ipcMain.handle(IPC_CHANNELS.getHubInTunnel, async () => pangeaApiClient.getHubInTunnel());

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
    await updateSettings((settings) => {
      settings.preferredTransport = preferredTransport;
    }, "preferredTransport setting");
  });

  ipcMain.handle(IPC_CHANNELS.getPreferredTransport, async () => preferredTransport);

  ipcMain.handle(IPC_CHANNELS.setLaunchAtStartup, async (_event, enabled: boolean) => {
    launchAtStartupEnabled = !!enabled;
    await updateSettings((settings) => {
      settings.launchAtStartup = launchAtStartupEnabled;
    }, "launchAtStartup setting");
    try {
      await applyLoginItem();
    } catch (err) {
      console.warn("Failed to apply login item:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getLaunchAtStartup, async () => {
    // Lockdown and auto-connect force the login item on, so return the stored
    // preference rather than the OS state.
    if (lockdownEnabled || autoConnectEnabled) {
      return launchAtStartupEnabled;
    }
    // Self-heal: re-derive from OS in case the user toggled it elsewhere, and
    // persist the correction so a relaunch doesn't resurrect the stale value.
    try {
      const live = await isLoginItemEnabled();
      if (live !== launchAtStartupEnabled) {
        launchAtStartupEnabled = live;
        await updateSettings((settings) => {
          settings.launchAtStartup = live;
        }, "launchAtStartup setting");
      }
      return live;
    } catch {
      return launchAtStartupEnabled;
    }
  });

  ipcMain.handle(IPC_CHANNELS.setLockdown, async (_event, enabled: boolean) => {
    const previouslyEnabled = lockdownEnabled;
    lockdownEnabled = !!enabled;
    await persistStartupSettings();
    try {
      await applyLoginItem();
    } catch (err) {
      console.warn("Failed to apply login item for lockdown:", err);
    }
    if (!previouslyEnabled && lockdownEnabled) {
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
    } else if (previouslyEnabled && !lockdownEnabled) {
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

  ipcMain.handle(IPC_CHANNELS.getLockdown, async () => lockdownEnabled);

  ipcMain.handle(IPC_CHANNELS.setAutoConnect, async (_event, enabled: boolean) => {
    autoConnectEnabled = !!enabled;
    if (autoConnectEnabled) userDisconnected = false;
    await persistStartupSettings();
    try {
      await applyLoginItem();
    } catch (err) {
      console.warn("Failed to apply login item for auto-connect:", err);
    }
  });

  ipcMain.handle(IPC_CHANNELS.getAutoConnect, async () => autoConnectEnabled);

  ipcMain.handle(IPC_CHANNELS.setNotifications, async (_event, enabled: boolean) => {
    notificationsEnabled = !!enabled;
    await updateSettings((settings) => {
      settings.notifications = notificationsEnabled;
    }, "notifications setting");
  });

  ipcMain.handle(IPC_CHANNELS.getNotifications, async () => notificationsEnabled);

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
    await updateSettings((settings) => {
      settings.locale = localePref;
    }, "locale setting");
  });

  ipcMain.handle(IPC_CHANNELS.getIsPackaged, async () => app.isPackaged);

  ipcMain.handle(IPC_CHANNELS.getCachedServers, async () => {
    try {
      const raw = await readStateFile("server-cache.json");
      return sanitizePublicServers(JSON.parse(raw));
    } catch {
      return [];
    }
  });

  ipcMain.handle(IPC_CHANNELS.cacheServers, async (_event, servers: unknown) => {
    if (!Array.isArray(servers) || servers.length > 4096) {
      return;
    }
    const safe = sanitizePublicServers(servers);
    try {
      const dir = await (await import("./platformPaths")).ensureUserStateDir();
      const fs = (await import("node:fs/promises")).default;
      const finalPath = path.join(dir, "server-cache.json");
      const tmpPath = `${finalPath}.tmp-${process.pid}-${Date.now()}`;
      await fs.writeFile(tmpPath, JSON.stringify(safe), "utf8");
      await fs.rename(tmpPath, finalPath);
    } catch (err) {
      console.warn("Failed to persist server cache:", sanitizeLog(err));
    }
  });

  ipcMain.handle(IPC_CHANNELS.getServers, async () => {
    try {
      const servers = await pangeaApiClient.getServers();
      return servers.map(toPublicServerInfo);
    } catch (err) {
      if (err instanceof AuthError) {
        pangeaApiClient.clearCache();
        await auth.logout();
        mainWindow?.webContents.send(IPC_CHANNELS.authInvalidated);
        return [];
      }
      throw err;
    }
  });

  ipcMain.handle(IPC_CHANNELS.rememberAccountNumber, async (_event, accountNumber: unknown) => {
    if (typeof accountNumber !== "string" || accountNumber.trim().length === 0) return;
    const dir = path.join(app.getPath("appData"), "pangeavpn-desktop");
    await (await import("node:fs/promises")).default.mkdir(dir, { recursive: true, mode: 0o700 });
    await writeSecret(path.join(dir, "remembered-account.dat"), accountNumber.trim());
  });

  ipcMain.handle(IPC_CHANNELS.getRememberedAccountNumber, async () => {
    try {
      const dir = path.join(app.getPath("appData"), "pangeavpn-desktop");
      return await readSecret(path.join(dir, "remembered-account.dat"));
    } catch {
      return null;
    }
  });

  ipcMain.handle(IPC_CHANNELS.clearRememberedAccountNumber, async () => {
    try {
      const dir = path.join(app.getPath("appData"), "pangeavpn-desktop");
      await (await import("node:fs/promises")).default.rm(path.join(dir, "remembered-account.dat"), { force: true });
    } catch {
      // best-effort
    }
  });

  ipcMain.handle(IPC_CHANNELS.listDevices, async () => {
    const devices = await pangeaApiClient.listDevices();
    // Flag our row by pubkey (rename-proof); strip the pubkey from the renderer.
    const myPubkey = pangeaApiClient.identityPubkey;
    return devices.map(({ identityPubkey, ...rest }) => ({
      ...rest,
      isCurrentDevice: Boolean(myPubkey && identityPubkey === myPubkey)
    }));
  });

  ipcMain.handle(IPC_CHANNELS.removeDevice, async (_event, deviceId: string) => {
    await pangeaApiClient.removeDevice(deviceId);
  });

  ipcMain.handle(IPC_CHANNELS.renameDevice, async (_event, deviceId: string, friendlyName: string) => {
    await pangeaApiClient.renameDevice(deviceId, friendlyName);
  });

  ipcMain.handle(IPC_CHANNELS.getSubscription, async () => {
    return pangeaApiClient.getSubscription();
  });

  ipcMain.handle(IPC_CHANNELS.provisionAndConnect, async (_event, serverPlan: unknown) => {
    userDisconnected = false;
    try {
      const result = await provisionAndConnect(normalizeServerPlan(serverPlan));
      void refreshTrayStatus();
      return result;
    } catch (err) {
      if (err instanceof AuthError) {
        pangeaApiClient.clearCache();
        await auth.logout();
        mainWindow?.webContents.send(IPC_CHANNELS.authInvalidated);
        return { ok: false };
      }
      throw err;
    }
  });

  ipcMain.handle(IPC_CHANNELS.cancelConnect, async () => {
    await cancelConnectAttempt();
  });

  ipcMain.handle(IPC_CHANNELS.provisionAndSwitch, async (_event, serverPlan: unknown) => {
    userDisconnected = false;
    try {
      const result = await provisionAndSwitch(normalizeServerPlan(serverPlan));
      void refreshTrayStatus();
      return result;
    } catch (err) {
      if (err instanceof AuthError) {
        pangeaApiClient.clearCache();
        await auth.logout();
        mainWindow?.webContents.send(IPC_CHANNELS.authInvalidated);
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
    message.includes("daemon token not found") ||
    message.includes("daemon unauthorized")
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

  // Toasts are attributed to the AUMID's Start Menu shortcut, which only an
  // install has — an unpackaged run must claim electron.exe, not the shipped id.
  app.setAppUserModelId(app.isPackaged ? "com.pangea.pangeavpn" : process.execPath);

  // Lock down navigation, new windows, embeds, permissions, and TLS.
  app.on("web-contents-created", (_event, contents) => {
    contents.setWindowOpenHandler(({ url }) => {
      if (isSafeExternalUrl(url)) {
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
  pangeaApiClient.onHubStatusChanged((status) => {
    mainWindow?.webContents.send(IPC_CHANNELS.hubStatusChanged, status);
  });
  pangeaApiClient.onHubShadowsocksResolved((creds) => void persistHubShadowsocks(creds));
  pangeaApiClient.onFrontedEndpointsResolved((endpoints) => void persistFrontedEndpoints(endpoints));
  pangeaApiClient.onDeadDropStateChanged((state) => void persistDeadDropState(state));
  pangeaApiClient.onServersResolved((servers) => void persistServers(servers));
  pangeaApiClient.onSubscriptionResolved((cached) => void persistSubscription(cached));

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
    const settings = JSON.parse(await readStateFile("settings.json")) as Record<string, unknown>;
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
    pangeaApiClient.setHubInTunnel(settings.hubInTunnel === true);
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
    // `alwaysConnected` was both settings at once; installs that predate the
    // split inherit it for each until the user changes one.
    const legacyAlwaysConnected =
      typeof settings.alwaysConnected === "boolean" ? settings.alwaysConnected : null;
    if (typeof settings.lockdown === "boolean") {
      lockdownEnabled = settings.lockdown;
    } else if (legacyAlwaysConnected !== null) {
      lockdownEnabled = legacyAlwaysConnected;
    }
    if (typeof settings.autoConnect === "boolean") {
      autoConnectEnabled = settings.autoConnect;
    } else if (legacyAlwaysConnected !== null) {
      autoConnectEnabled = legacyAlwaysConnected;
    }
    if (typeof settings.notifications === "boolean") {
      notificationsEnabled = settings.notifications;
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
    // The replay guard travels with the switch: without the last accepted seq a
    // reinstall would accept a stale blob it has already moved past.
    pangeaApiClient.setDeadDropEnabled(settings.deadDrop !== false);
    pangeaApiClient.setDeadDropState(settings.deadDropSeq, settings.deadDropLastAttempt);
    pangeaApiClient.setCachedServers(settings.servers);
    pangeaApiClient.setCachedSubscription(settings.subscription);
    if (typeof settings.lastServerId === "string") {
      lastServerId = settings.lastServerId;
    }
    if (typeof settings.lastProfileId === "string") {
      lastConnectedProfileId = settings.lastProfileId;
    }
    provisionedProfiles = dropExpired(parseProfileRecords(settings.provisionedProfiles), Date.now());
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

  // Off the startup path: the window must not wait on a hub that may be blocked.
  refreshSubscriptionCache("launch");

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
  // A resolver, not a snapshot, so this stays correct across window recreates.
  setupAutoUpdater(() => mainWindow);
  createTray();
  watchDisplayChanges();
  if (!hiddenLaunch) {
    showMainWindow();
  }
  daemonProcess
    .ensureRunning()
    .then(async () => {
      // The daemon outlives the app, so its transport memory has to be dropped
      // here rather than on quit, which a crash or force-kill never reaches.
      try {
        await daemonClient.clearTransportMemory();
      } catch (err) {
        console.warn("failed to clear transport memory on startup", sanitizeLog(err));
      }

      // Covers the case where no lock state was persisted yet; the daemon
      // re-applies persisted locks itself. No-ops if already up or engaged.
      if (!lockdownEnabled) return;
      const status = await daemonClient.getStatus();
      if (status.state !== "CONNECTED" && status.state !== "CONNECTING") {
        await daemonClient.engageKillSwitch({
          profileId: lastConnectedProfileId ?? undefined,
          allowLAN: allowLanEnabled
        });
      }
    })
    .catch((err) => {
      console.error("failed to ensure daemon / engage lockdown on startup", sanitizeLog(err));
      // Lockdown could not be confirmed engaged — surface it rather than let
      // the UI keep reporting the kill switch as active while traffic flows.
      if (lockdownEnabled) {
        trayStatusState = "ERROR";
        trayStatusDetail = "lockdown failed to engage";
        updateTrayMenu();
        mainWindow?.webContents.send("lockdown:engage-failed");
      }
    });

  startNetworkWatcher();
  onNetworkChange(() => {
    recoverFromNetworkChange().catch((err) => {
      console.warn("network recovery failed:", sanitizeLog(err));
    });
  });
}

app.on("window-all-closed", () => {
  if (process.platform !== "darwin") {
    app.quit();
  }
});

app.on("activate", () => {
  showMainWindow();
});

let quitCleanupDone = false;

app.on("before-quit", (event) => {
  if (quitCleanupDone) return;
  event.preventDefault();
  isQuitting = true;
  stopTrayStatusPolling();
  tray?.destroy();
  tray = null;
  trayDefaultImage = null;
  trayConnectedImage = null;

  // Disconnect first (preserving the kill switch if armed) — otherwise quit
  // SIGTERMs the daemon mid-tunnel and leaves routes/DNS/Lockdown behind.
  void (async () => {
    try {
      const status = await daemonClient.getStatus();
      if (status.state === "CONNECTED" || status.state === "CONNECTING") {
        await daemonClient.disconnect({ keepKillSwitch: lockdownEnabled });
      }
    } catch (err) {
      console.warn("quit: clean disconnect failed", sanitizeLog(err));
    } finally {
      daemonProcess.stop();
      quitCleanupDone = true;
      app.quit();
    }
  })();
});

// Chromium encrypts its cookie store with a keychain key, and an ad-hoc
// signature cannot hold the ACL, so macOS asks for the password every launch.
if (process.platform === "darwin") {
  app.commandLine.appendSwitch("use-mock-keychain");
}

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
