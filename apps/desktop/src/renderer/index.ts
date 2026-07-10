import type { LogEntry, StatusResponse } from "@pangeavpn/shared-types";
import {
  initAutoConnect,
  notifyStatusTick,
  notifyUserConnected,
  notifyUserDisconnected,
  notifyToggleChanged,
  attemptInitialAutoConnect,
  getUserIntent
} from "./autoConnect.js";

let verboseErrors = localStorage.getItem("pangea:verboseErrors") === "1";

// Local record of this install's device name, so we can mark "this device" in the
// devices list. Written after a successful login; cleared on logout.
const MY_DEVICE_NAME_KEY = "pangea:myDeviceName";

function reportError(context: string, error: unknown, friendly?: string): string {
  // Log full error to console (captured by Electron logs / devtools)
  console.error(`[${context}]`, error);
  if (verboseErrors) return `[${context}] ${error instanceof Error ? error.message : String(error)}`;
  if (friendly) return friendly;
  const msg = String(error).toLowerCase();
  if (msg.includes("timeout") || msg.includes("etimedout") || msg.includes("network") || msg.includes("fetch") || msg.includes("econn")) {
    return "Please check your internet connection and try again.";
  }
  return "Something went wrong. Please try again later.";
}

const stateEl = document.getElementById("state") as HTMLSpanElement;
const detailEl = document.getElementById("detail") as HTMLSpanElement;
const cloakEl = document.getElementById("cloak") as HTMLSpanElement;
const wireguardEl = document.getElementById("wireguard") as HTMLSpanElement;
const ksDot = document.getElementById("ksDot") as HTMLElement;
const throughputPanel = document.getElementById("throughputPanel") as HTMLElement;
const txBytesEl = document.getElementById("txBytes") as HTMLSpanElement;
const rxBytesEl = document.getElementById("rxBytes") as HTMLSpanElement;
const themeToggleBtn = document.getElementById("themeToggleBtn") as HTMLButtonElement;
const uiMessageEl = document.getElementById("uiMessage") as HTMLParagraphElement;
const appVersionEl = document.getElementById("appVersion") as HTMLSpanElement;
const copyDiagnosticsBtn = document.getElementById("copyDiagnosticsBtn") as HTMLButtonElement;
const copyLogsBtn = document.getElementById("copyLogsBtn") as HTMLButtonElement;
const clearLogsBtn = document.getElementById("clearLogsBtn") as HTMLButtonElement;
const logsEl = document.getElementById("logs") as HTMLDivElement;
const collapsibleSections = Array.from(document.querySelectorAll<HTMLElement>("[data-collapsible]"));
const logsSection = document.getElementById("logsSection") as HTMLElement;

const daemonApi = window.daemonApi;
const pangeaApi = window.pangeaApi;

const loginBtn = document.getElementById("loginBtn") as HTMLButtonElement;
const logoutBtn = document.getElementById("logoutBtn") as HTMLButtonElement;
const serverPanel = document.getElementById("serverPanel") as HTMLElement;
const serverSelect = document.getElementById("serverSelect") as HTMLSelectElement;
const serverConnectBtn = document.getElementById("serverConnectBtn") as HTMLButtonElement;
const serverDisconnectBtn = document.getElementById("serverDisconnectBtn") as HTMLButtonElement;
const serverRefreshBtn = document.getElementById("serverRefreshBtn") as HTMLButtonElement;
const serverIndicator = document.getElementById("serverIndicator") as HTMLSpanElement;
const serverIndicatorLabel = document.getElementById("serverIndicatorLabel") as HTMLSpanElement;
const directIpToggle = document.getElementById("directIpToggle") as HTMLInputElement;
const directIpOnlyToggle = document.getElementById("directIpOnlyToggle") as HTMLInputElement;
const allowLanToggle = document.getElementById("allowLanToggle") as HTMLInputElement;
const launchAtStartupToggle = document.getElementById("launchAtStartupToggle") as HTMLInputElement;
const alwaysConnectedToggle = document.getElementById("alwaysConnectedToggle") as HTMLInputElement;
const loginScreen = document.getElementById("loginScreen") as HTMLElement;
const loginScreenBtn = document.getElementById("loginScreenBtn") as HTMLButtonElement;
const loginScreenMessage = document.getElementById("loginScreenMessage") as HTMLParagraphElement;
const heroCard = document.getElementById("heroCard") as HTMLElement;
const menuBtn = document.getElementById("menuBtn") as HTMLButtonElement;
const menuDropdown = document.getElementById("menuDropdown") as HTMLElement;
const manageSubLink = document.getElementById("manageSubLink") as HTMLAnchorElement;
const menuSettingsBtn = document.getElementById("menuSettingsBtn") as HTMLButtonElement;
const settingsOverlay = document.getElementById("settingsOverlay") as HTMLElement;
const settingsOverlayCloseBtn = document.getElementById("settingsOverlayCloseBtn") as HTMLButtonElement;
const accountUserLabel = document.getElementById("accountUserLabel") as HTMLSpanElement;
const accountSubscription = document.getElementById("accountSubscription") as HTMLSpanElement;
const accountTokenValue = document.getElementById("accountTokenValue") as HTMLElement;
const accountTokenToggleBtn = document.getElementById("accountTokenToggleBtn") as HTMLButtonElement;
const accountTokenCopyBtn = document.getElementById("accountTokenCopyBtn") as HTMLButtonElement;
const checkUpdatesBtn = document.getElementById("checkUpdatesBtn") as HTMLButtonElement;
const settingsVersionEl = document.getElementById("settingsVersion") as HTMLSpanElement;
const serverPickerBtn = document.getElementById("serverPickerBtn") as HTMLButtonElement;
const serverPickerLabel = document.getElementById("serverPickerLabel") as HTMLElement;
const serverPickerOverlay = document.getElementById("serverPickerOverlay") as HTMLElement;
const serverPickerOverlayList = document.getElementById("serverPickerOverlayList") as HTMLElement;
const serverPickerOverlayCloseBtn = document.getElementById("serverPickerOverlayCloseBtn") as HTMLButtonElement;
const cloakDot = document.getElementById("cloakDot") as HTMLElement;
const wgDot = document.getElementById("wgDot") as HTMLElement;

type ThemeMode = "light" | "dark";
const THEME_STORAGE_KEY = "pangea-vpn-theme";
const COLLAPSE_STATE_KEY = "pangea-vpn-collapse-state";

let currentDaemonState: StatusResponse["state"] = "DISCONNECTED";
let latestStatus: StatusResponse | null = null;
let uiRefreshing = false;
let uiWorking = false;
let lastServerIdLocal: string | null = null;
let alwaysConnectedLocal = false;
let logsCursor = 0;
let logEntries: LogEntry[] = [];
let authState: AuthState = { authenticated: false, user: null };
let servers: ServerInfo[] = [];
let serverWorking = false;
// True once a connect attempt has been in-flight for >= 1s, signaling the UI
// to enable the disconnect button so the user can bail out before the 10s
// cloak timeout fires.
let connectCancelAllowed = false;
let connectCancelTimer: ReturnType<typeof setTimeout> | null = null;
const CONNECT_CANCEL_DELAY_MS = 1000;

themeToggleBtn.addEventListener("click", () => {
  const nextTheme: ThemeMode = document.body.dataset.theme === "dark" ? "light" : "dark";
  applyTheme(nextTheme);
});

menuBtn.addEventListener("click", () => {
  const isOpen = menuDropdown.classList.contains("open");
  menuDropdown.classList.toggle("open", !isOpen);
  menuBtn.setAttribute("aria-expanded", String(!isOpen));
});

document.addEventListener("click", (e) => {
  if (menuDropdown.classList.contains("open") && !menuBtn.contains(e.target as Node) && !menuDropdown.contains(e.target as Node)) {
    menuDropdown.classList.remove("open");
    menuBtn.setAttribute("aria-expanded", "false");
  }
});

manageSubLink.addEventListener("click", (e) => {
  e.preventDefault();
  window.openExternal?.("https://pangeavpn.org");
});

// ── Fullscreen settings overlay ───────────────────────────────

const LAST_TOKEN_KEY = "pangea:lastToken";
let accountTokenRevealed = false;

function maskToken(token: string): string {
  return "•".repeat(Math.min(Math.max(token.length, 8), 16));
}

function refreshAccountToken(): void {
  const token = localStorage.getItem(LAST_TOKEN_KEY);
  const hasToken = Boolean(token);
  accountTokenToggleBtn.disabled = !hasToken;
  accountTokenCopyBtn.disabled = !hasToken;
  if (!token) {
    accountTokenRevealed = false;
    accountTokenValue.textContent = "—";
    accountTokenToggleBtn.textContent = "Show";
    return;
  }
  accountTokenValue.textContent = accountTokenRevealed ? token : maskToken(token);
  accountTokenToggleBtn.textContent = accountTokenRevealed ? "Hide" : "Show";
}

function formatSubscriptionDate(iso: string | null): string {
  if (!iso) return "";
  try {
    return ` ${new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" })}`;
  } catch {
    return "";
  }
}

function subscriptionText(sub: SubscriptionInfo | null): { text: string; warn: boolean } {
  if (!sub) return { text: "No active subscription", warn: false };
  const when = formatSubscriptionDate(sub.expiresAt);
  if (sub.status === "active" || sub.status === "trialing") {
    const trial = sub.status === "trialing" ? "Free trial · " : "";
    return { text: `${trial}${sub.renews ? "Renews" : "Expires"}${when}`, warn: false };
  }
  if (sub.status === "past_due") {
    return { text: `Payment past due${when ? " —" + when : ""}`, warn: true };
  }
  return { text: "No active subscription", warn: false };
}

// Fetched fresh each time Settings opens so expiry/renewal is always current.
async function refreshSubscription(): Promise<void> {
  accountSubscription.classList.remove("account-subscription-warn");
  if (!pangeaApi) {
    accountSubscription.textContent = "—";
    return;
  }
  accountSubscription.textContent = "Loading...";
  let sub: SubscriptionInfo | null = null;
  try {
    sub = await pangeaApi.getSubscription();
  } catch {
    sub = null;
  }
  const { text, warn } = subscriptionText(sub);
  accountSubscription.textContent = text;
  accountSubscription.classList.toggle("account-subscription-warn", warn);
}

// ── Overlay focus management ──────────────────────────────────
// Full-screen overlays cover the shell but leave it in the tab order, so a
// keyboard/screen-reader user could otherwise reach controls hidden behind
// them. On open we make the shell `inert` (non-interactive + hidden from AT)
// and move focus into the overlay; on close we release it and restore focus.
const overlayReturnFocus: Array<HTMLElement | null> = [];

function activateOverlay(overlay: HTMLElement): void {
  overlayReturnFocus.push(document.activeElement as HTMLElement | null);
  shell.setAttribute("inert", "");
  const focusTarget = overlay.querySelector<HTMLElement>("button:not([hidden]), [href], input, select, textarea");
  window.setTimeout(() => (focusTarget ?? overlay).focus(), 0);
}

function deactivateOverlay(): void {
  // Only re-enable the shell once no full-screen overlay remains open.
  if (!settingsOverlay.classList.contains("visible") && !serverPickerOverlay.classList.contains("visible")) {
    shell.removeAttribute("inert");
  }
  const prev = overlayReturnFocus.pop();
  prev?.focus?.();
}

// True while a modal is stacked above the full-screen overlays, so their Escape
// handlers can defer to the top layer instead of closing the layer beneath it.
function isSubModalOpen(): boolean {
  return devicesModal.classList.contains("visible") || updateOverlay.classList.contains("visible");
}

function openSettings(): void {
  settingsOverlay.classList.add("visible");
  settingsOverlay.setAttribute("aria-hidden", "false");
  settingsVersionEl.textContent = appVersionEl.textContent || "—";
  refreshAccountToken();
  void refreshSubscription();
  activateOverlay(settingsOverlay);
}

function closeSettings(): void {
  settingsOverlay.classList.remove("visible");
  settingsOverlay.setAttribute("aria-hidden", "true");
  // Re-mask the token so it isn't revealed the next time settings opens.
  accountTokenRevealed = false;
  refreshAccountToken();
  deactivateOverlay();
}

menuSettingsBtn.addEventListener("click", () => {
  menuDropdown.classList.remove("open");
  menuBtn.setAttribute("aria-expanded", "false");
  openSettings();
});

settingsOverlayCloseBtn.addEventListener("click", closeSettings);

document.addEventListener("keydown", (e) => {
  // Defer to a stacked modal (Devices / Update) so Escape backs out one layer.
  if (e.key === "Escape" && settingsOverlay.classList.contains("visible") && !isSubModalOpen()) {
    e.preventDefault();
    e.stopPropagation();
    closeSettings();
  }
});

accountTokenToggleBtn.addEventListener("click", () => {
  accountTokenRevealed = !accountTokenRevealed;
  refreshAccountToken();
});

accountTokenCopyBtn.addEventListener("click", async () => {
  const token = localStorage.getItem(LAST_TOKEN_KEY);
  if (!token) {
    showToast("No token to copy.");
    return;
  }
  try {
    await copyTextToClipboard(token);
    showToast("Token copied to clipboard.", 3000, true);
  } catch (error) {
    showToast(reportError("copyToken", error));
  }
});

function openServerPicker(): void {
  if (serverPickerBtn.disabled) return;
  serverPickerOverlay.classList.add("visible");
  serverPickerOverlay.setAttribute("aria-hidden", "false");
  serverPickerBtn.setAttribute("aria-expanded", "true");
  activateOverlay(serverPickerOverlay);
  // Refresh the list (and its load values) every time the picker opens.
  void refreshServersWithRetry();
}

function closeServerPicker(): void {
  serverPickerOverlay.classList.remove("visible");
  serverPickerOverlay.setAttribute("aria-hidden", "true");
  serverPickerBtn.setAttribute("aria-expanded", "false");
  deactivateOverlay();
}

serverPickerBtn.addEventListener("click", openServerPicker);
serverPickerOverlayCloseBtn.addEventListener("click", closeServerPicker);

// Keep the server list current whenever the app comes back into view — shown
// from the tray, restored from minimize, or otherwise unhidden.
document.addEventListener("visibilitychange", () => {
  if (document.visibilityState === "visible") void refreshServersWithRetry();
});

document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && serverPickerOverlay.classList.contains("visible")) {
    e.preventDefault();
    e.stopPropagation();
    closeServerPicker();
  }
});

// Escape closes the stacked Devices / Update modals (checked before the
// full-screen overlays above, which defer via isSubModalOpen()).
document.addEventListener("keydown", (e) => {
  if (e.key !== "Escape") return;
  if (devicesModal.classList.contains("visible")) {
    e.preventDefault();
    e.stopPropagation();
    devicesModal.classList.remove("visible");
  } else if (updateOverlay.classList.contains("visible")) {
    e.preventDefault();
    e.stopPropagation();
    hideUpdateModal();
  }
});

copyLogsBtn.addEventListener("click", async () => {
  const text = logsEl.textContent ?? "";
  if (!text.trim()) {
    setUiMessage("No logs to copy.");
    return;
  }

  try {
    await copyTextToClipboard(text);
    setUiMessage("Logs copied to clipboard.");
  } catch (error) {
    setUiMessage(reportError("copyLogs", error));
  }
});

copyDiagnosticsBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  uiWorking = true;
  updateBusyIndicator();
  try {
    const report = await buildDiagnosticsReport();
    await copyTextToClipboard(report);
    setUiMessage("Diagnostics copied to clipboard.");
  } catch (error) {
    setUiMessage(reportError("copyDiagnostics", error));
  } finally {
    uiWorking = false;
    updateBusyIndicator();
  }
});

clearLogsBtn.addEventListener("click", () => {
  const lastSeenTs = logEntries.length > 0 ? logEntries[logEntries.length - 1].ts : logsCursor;
  logsCursor = Math.max(logsCursor, lastSeenTs);
  logEntries = [];
  renderLogs(logEntries);
  setUiMessage("Logs cleared.");
});

loginBtn.addEventListener("click", () => {
  // Show the login screen so the user can enter their VPN token
  authState = { authenticated: false, user: null };
  updateAuthUI();
});

logoutBtn.addEventListener("click", async () => {
  if (!pangeaApi) return;
  logoutBtn.disabled = true;
  setUiMessage("Signing out...");
  try {
    await pangeaApi.logout();
    localStorage.removeItem(MY_DEVICE_NAME_KEY);
    authState = { authenticated: false, user: null };
    servers = [];
    updateAuthUI();
    renderServers();
    await refreshStatus();
    setUiMessage("Signed out.");
  } catch (error) {
    setUiMessage(reportError("signOut", error));
  } finally {
    logoutBtn.disabled = false;
  }
});

const loginTokenInput = document.getElementById("loginTokenInput") as HTMLInputElement;
const cachedTokenBtn = document.getElementById("cachedTokenBtn") as HTMLButtonElement;

// Show cached token button if a previous token exists. The token is masked (the
// click handler reads the real value from storage) to match the Settings viewer.
function refreshCachedTokenBtn(): void {
  const cached = localStorage.getItem("pangea:lastToken");
  if (cached) {
    const masked = cached.length > 4
      ? cached.slice(0, 4) + "•".repeat(Math.min(cached.length - 4, 12))
      : "•".repeat(cached.length);
    cachedTokenBtn.textContent = masked;
    cachedTokenBtn.setAttribute("aria-label", "Sign in with saved token");
    cachedTokenBtn.hidden = false;
  } else {
    cachedTokenBtn.hidden = true;
  }
}
refreshCachedTokenBtn();

cachedTokenBtn.addEventListener("click", () => {
  const cached = localStorage.getItem("pangea:lastToken");
  if (cached) {
    loginTokenInput.value = cached;
    loginTokenInput.dispatchEvent(new Event("input"));
    loginScreenBtn.click();
  }
});

loginTokenInput.addEventListener("keydown", (e) => {
  if (e.key === "Enter") {
    loginScreenBtn.click();
  }
});

loginTokenInput.addEventListener("input", () => {
  const val = loginTokenInput.value.trim();
  if (val.length === 0) {
    loginTokenInput.style.borderColor = "";
  } else if (val.length >= 8 && /^[a-zA-Z0-9_-]+$/.test(val)) {
    loginTokenInput.style.borderColor = "var(--success)";
  } else {
    loginTokenInput.style.borderColor = "var(--danger)";
  }
});

loginTokenInput.addEventListener("paste", () => {
  setTimeout(() => {
    loginTokenInput.value = loginTokenInput.value.trim();
    loginTokenInput.dispatchEvent(new Event("input"));
  }, 0);
});

const loginDashboardLink = document.getElementById("loginDashboardLink") as HTMLAnchorElement;
loginDashboardLink.addEventListener("click", (e) => {
  e.preventDefault();
  window.openExternal?.("https://pangeavpn.org/");
});

// ── Device management screen (login-flow: device limit reached) ──

const deviceLimitScreen = document.getElementById("deviceLimitScreen") as HTMLElement;
const deviceLimitTitle = document.getElementById("deviceLimitTitle") as HTMLElement;
const deviceLimitSubtitle = document.getElementById("deviceLimitSubtitle") as HTMLElement;
const deviceList = document.getElementById("deviceList") as HTMLElement;
const deviceLimitContinueBtn = document.getElementById("deviceLimitContinueBtn") as HTMLButtonElement;
const deviceLimitLogoutBtn = document.getElementById("deviceLimitLogoutBtn") as HTMLButtonElement;
const deviceLimitMessage = document.getElementById("deviceLimitMessage") as HTMLParagraphElement;

// ── Devices modal (menu, logged-in) ──────────────────────────

const devicesModal = document.getElementById("devicesModal") as HTMLElement;
const devicesModalList = document.getElementById("devicesModalList") as HTMLElement;
const devicesModalCloseBtn = document.getElementById("devicesModalCloseBtn") as HTMLButtonElement;
const devicesModalMessage = document.getElementById("devicesModalMessage") as HTMLParagraphElement;
const menuDevicesBtn = document.getElementById("menuDevicesBtn") as HTMLButtonElement;

let pendingLoginToken: string | null = null;
let deviceRemovedCount = 0;

function formatDeviceDate(iso: string): string {
  try {
    return new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" });
  } catch {
    return iso;
  }
}

function generateFallbackName(): string {
  const adj = ["Swift", "Bold", "Bright", "Calm", "Cool", "Fast", "Iron", "Kind", "Pale", "True", "Wild"][Math.floor(Math.random() * 11)];
  const noun = ["Bear", "Eagle", "Fox", "Hawk", "Lion", "Owl", "Stag", "Tiger", "Wolf", "Wren"][Math.floor(Math.random() * 10)];
  return `${adj} ${noun}`;
}

async function showDeviceLimitScreen(token: string): Promise<void> {
  if (!pangeaApi) return;
  pendingLoginToken = token;
  deviceRemovedCount = 0;
  deviceLimitTitle.textContent = "Device Limit Reached";
  deviceLimitSubtitle.textContent = "Your account has reached the maximum number of devices. Remove one to continue signing in.";
  deviceLimitContinueBtn.hidden = true;
  deviceLimitLogoutBtn.textContent = "Cancel & Sign Out";
  deviceLimitMessage.textContent = "";
  loginScreen.hidden = true;
  deviceLimitScreen.hidden = false;

  deviceList.innerHTML = '<div class="device-list-loading"><span class="spinner"></span></div>';
  try {
    const devices = await pangeaApi.listDevices();
    renderDeviceList(devices);
  } catch (err) {
    deviceList.innerHTML = "";
    deviceLimitMessage.textContent = reportError("listDevices", err, "Could not load devices. Please try again.");
  }
}

async function showDevicesModal(): Promise<void> {
  if (!pangeaApi) return;
  devicesModalMessage.textContent = "";
  devicesModalList.innerHTML = '<div class="device-list-loading"><span class="spinner"></span></div>';
  devicesModal.classList.add("visible");

  try {
    const devices = await pangeaApi.listDevices();
    renderDevicesModalList(devices);
  } catch (err) {
    devicesModalList.innerHTML = "";
    devicesModalMessage.textContent = reportError("listDevices", err, "Could not load devices. Please try again.");
  }
}

function renderDevicesModalList(devices: DeviceInfo[]): void {
  devicesModalList.innerHTML = "";
  if (devices.length === 0) {
    devicesModalMessage.textContent = "No devices found.";
    return;
  }
  devicesModalMessage.textContent = "";

  // Match the stored local name against the list to identify "this device".
  const myName = localStorage.getItem(MY_DEVICE_NAME_KEY);
  // Put "this device" at the top so the user sees it first.
  const sorted = [...devices].sort((a, b) => {
    const aMine = myName !== null && a.friendlyName === myName;
    const bMine = myName !== null && b.friendlyName === myName;
    if (aMine === bMine) return 0;
    return aMine ? -1 : 1;
  });

  for (const device of sorted) {
    const name = device.friendlyName || generateFallbackName();
    const isMine = myName !== null && device.friendlyName === myName;
    const dateStr = formatDeviceDate(device.createdAt);
    const item = document.createElement("div");
    item.className = "device-item";
    item.dataset.deviceId = device.id;
    const info = document.createElement("div");
    info.className = "device-info";
    const nameSpan = document.createElement("span");
    nameSpan.className = "device-name";
    nameSpan.textContent = name;
    if (isMine) {
      nameSpan.appendChild(document.createTextNode(" "));
      const badge = document.createElement("span");
      badge.className = "device-current-badge";
      badge.textContent = "This device";
      nameSpan.appendChild(badge);
    }
    const dateSpan = document.createElement("span");
    dateSpan.className = "device-date";
    dateSpan.textContent = `Added ${dateStr}`;
    info.appendChild(nameSpan);
    info.appendChild(dateSpan);
    const removeBtn = document.createElement("button");
    removeBtn.className = "device-remove-btn";
    if (isMine) {
      removeBtn.textContent = "Current";
      removeBtn.disabled = true;
      removeBtn.title = "Sign out to remove this device";
    } else {
      removeBtn.textContent = "Remove";
      removeBtn.addEventListener("click", () => void handleDevicesModalRemove(device.id, item, removeBtn));
    }
    item.appendChild(info);
    item.appendChild(removeBtn);
    devicesModalList.appendChild(item);
  }
}

async function handleDevicesModalRemove(deviceId: string, itemEl: HTMLElement, btn: HTMLButtonElement): Promise<void> {
  if (!pangeaApi) return;
  btn.disabled = true;
  btn.textContent = "Removing...";
  devicesModalMessage.textContent = "";
  try {
    await pangeaApi.removeDevice(deviceId);
    itemEl.remove();
    showToast("Device removed.", 4000, true);
    if (devicesModalList.children.length === 0) {
      devicesModalMessage.textContent = "No devices remaining.";
    }
  } catch (err) {
    btn.disabled = false;
    btn.textContent = "Remove";
    devicesModalMessage.textContent = reportError("removeDevice", err, "Failed to remove device. Please try again.");
  }
}

function renderDeviceList(devices: DeviceInfo[]): void {
  deviceList.innerHTML = "";
  if (devices.length === 0) {
    deviceLimitMessage.textContent = "No devices found. You can continue signing in now.";
    deviceLimitContinueBtn.hidden = false;
    return;
  }

  for (const device of devices) {
    const name = device.friendlyName || generateFallbackName();
    const dateStr = formatDeviceDate(device.createdAt);

    const item = document.createElement("div");
    item.className = "device-item";
    item.dataset.deviceId = device.id;

    const info = document.createElement("div");
    info.className = "device-info";
    const nameSpan = document.createElement("span");
    nameSpan.className = "device-name";
    nameSpan.textContent = name;
    const dateSpan = document.createElement("span");
    dateSpan.className = "device-date";
    dateSpan.textContent = `Added ${dateStr}`;
    info.appendChild(nameSpan);
    info.appendChild(dateSpan);

    const removeBtn = document.createElement("button");
    removeBtn.className = "device-remove-btn";
    removeBtn.textContent = "Remove";
    removeBtn.addEventListener("click", () => void handleDeviceRemove(device.id, item, removeBtn));

    item.appendChild(info);
    item.appendChild(removeBtn);
    deviceList.appendChild(item);
  }
}

async function handleDeviceRemove(deviceId: string, itemEl: HTMLElement, btn: HTMLButtonElement): Promise<void> {
  if (!pangeaApi) return;
  btn.disabled = true;
  btn.textContent = "Removing...";
  deviceLimitMessage.textContent = "";
  try {
    await pangeaApi.removeDevice(deviceId);
    itemEl.remove();
    deviceRemovedCount++;
    deviceLimitContinueBtn.hidden = false;
    showToast("Device removed.", 4000, true);
  } catch (err) {
    btn.disabled = false;
    btn.textContent = "Remove";
    deviceLimitMessage.textContent = reportError("removeDevice", err, "Failed to remove device. Please try again.");
  }
}

deviceLimitContinueBtn.addEventListener("click", async () => {
  if (!pangeaApi || !pendingLoginToken) return;
  deviceLimitContinueBtn.disabled = true;
  deviceLimitMessage.textContent = "Signing in...";
  try {
    authState = await pangeaApi.login(pendingLoginToken);
    if (authState.authenticated) {
      localStorage.setItem("pangea:lastToken", pendingLoginToken);
      pendingLoginToken = null;
      deviceLimitScreen.hidden = true;
      loginScreenMessage.textContent = "";
      loginTokenInput.value = "";
      updateAuthUI();
      if (authState.friendlyName) {
        localStorage.setItem(MY_DEVICE_NAME_KEY, authState.friendlyName);
        showToast(`Your device is called "${authState.friendlyName}".`, 6000, true);
      }
      await refreshServers();
    } else if (authState.error === "DEVICE_LIMIT_REACHED") {
      deviceLimitMessage.textContent = "Still at device limit. Please remove another device.";
      // Reload the updated device list
      try {
        const devices = await pangeaApi.listDevices();
        renderDeviceList(devices);
      } catch {
        // best-effort
      }
    } else {
      deviceLimitMessage.textContent = authState.error || "Sign in failed.";
    }
  } catch (err) {
    deviceLimitMessage.textContent = reportError("deviceLimitSignIn", err);
  } finally {
    deviceLimitContinueBtn.disabled = false;
  }
});

deviceLimitLogoutBtn.addEventListener("click", async () => {
  if (!pangeaApi) return;
  deviceLimitLogoutBtn.disabled = true;
  try {
    await pangeaApi.logout();
  } catch {
    // best-effort
  } finally {
    pendingLoginToken = null;
    deviceLimitScreen.hidden = true;
    loginScreen.hidden = false;
    loginScreenMessage.textContent = "";
    deviceLimitLogoutBtn.disabled = false;
  }
});

devicesModalCloseBtn.addEventListener("click", () => {
  devicesModal.classList.remove("visible");
});

devicesModal.addEventListener("click", (e) => {
  if (e.target === devicesModal) devicesModal.classList.remove("visible");
});

menuDevicesBtn.addEventListener("click", () => {
  menuDropdown.classList.remove("open");
  menuBtn.setAttribute("aria-expanded", "false");
  void showDevicesModal();
});

loginScreenBtn.addEventListener("click", async () => {
  if (!pangeaApi) return;
  const token = loginTokenInput.value.trim();
  if (!token) {
    loginScreenMessage.textContent = "Please enter your VPN token.";
    return;
  }
  loginScreenBtn.disabled = true;
  loginTokenInput.disabled = true;
  loginScreenMessage.textContent = "Signing in...";
  try {
    authState = await pangeaApi.login(token);
    if (authState.authenticated) {
      localStorage.setItem("pangea:lastToken", token);
      loginScreenMessage.textContent = "";
      loginTokenInput.value = "";
      updateAuthUI();
      if (authState.friendlyName) {
        localStorage.setItem(MY_DEVICE_NAME_KEY, authState.friendlyName);
        showToast(`Your device is called "${authState.friendlyName}".`, 6000, true);
      }
      await refreshServers();
    } else if (authState.error === "DEVICE_LIMIT_REACHED") {
      loginScreenBtn.disabled = false;
      loginTokenInput.disabled = false;
      loginScreenMessage.textContent = "";
      await showDeviceLimitScreen(token);
    } else if (authState.error) {
      loginScreenMessage.textContent = authState.error;
    } else {
      loginScreenMessage.textContent = "Invalid VPN token.";
    }
  } catch (error) {
    loginScreenMessage.textContent = reportError("signIn", error);
  } finally {
    if (deviceLimitScreen.hidden) {
      loginScreenBtn.disabled = false;
      loginTokenInput.disabled = false;
    }
  }
});

serverConnectBtn.addEventListener("click", async () => {
  if (!pangeaApi || !daemonApi) return;
  const serverId = serverSelect.value;
  if (!serverId) {
    setUiMessage("No server selected.");
    return;
  }

  serverWorking = true;
  connectCancelAllowed = false;
  if (connectCancelTimer) clearTimeout(connectCancelTimer);
  connectCancelTimer = setTimeout(() => {
    connectCancelAllowed = true;
    connectCancelTimer = null;
    updateServerControlStates();
  }, CONNECT_CANCEL_DELAY_MS);
  updateServerBusyIndicator(true, "Provisioning...");
  updateServerControlStates();
  try {
    setUiMessage("Provisioning and connecting...");
    const result = await pangeaApi.provisionAndConnect(serverId);
    if (result.ok) {
      setUiMessage("Connected.");
      notifyUserConnected();
      void refreshLastServer();
    } else {
      setUiMessage("Couldn't connect. Try again, or choose another server.");
    }
    await refreshStatus();
  } catch (error) {
    // If auth was invalidated, the onAuthInvalidated listener handles the UI
    if (pangeaApi) {
      try {
        const state = await pangeaApi.getAuthState();
        if (!state.authenticated) {
          authState = { authenticated: false, user: null };
          servers = [];
          updateAuthUI();
          renderServers();
          showToast("You've been signed out. Please sign in again.");
          return;
        }
      } catch {
        // ignore
      }
    }
    setUiMessage(reportError("serverConnect", error));
    await refreshStatus();
  } finally {
    serverWorking = false;
    if (connectCancelTimer) {
      clearTimeout(connectCancelTimer);
      connectCancelTimer = null;
    }
    connectCancelAllowed = false;
    updateServerBusyIndicator(false);
    updateServerControlStates();
  }
});

async function switchToServer(serverId: string): Promise<void> {
  if (!pangeaApi || !daemonApi) return;
  if (!serverId) return;

  serverWorking = true;
  connectCancelAllowed = false;
  if (connectCancelTimer) clearTimeout(connectCancelTimer);
  connectCancelTimer = setTimeout(() => {
    connectCancelAllowed = true;
    connectCancelTimer = null;
    updateServerControlStates();
  }, CONNECT_CANCEL_DELAY_MS);
  updateServerBusyIndicator(true, "Provisioning...");
  updateServerControlStates();
  try {
    setUiMessage("Provisioning new server...");
    const result = await pangeaApi.provisionAndSwitch(serverId);
    if (result.ok) {
      setUiMessage("Connected.");
      notifyUserConnected();
      void refreshLastServer();
    } else {
      setUiMessage("Couldn't switch servers. Try again, or pick another server.");
    }
    await refreshStatus();
  } catch (error) {
    if (pangeaApi) {
      try {
        const state = await pangeaApi.getAuthState();
        if (!state.authenticated) {
          authState = { authenticated: false, user: null };
          servers = [];
          updateAuthUI();
          renderServers();
          showToast("You've been signed out. Please sign in again.");
          return;
        }
      } catch {
        // ignore
      }
    }
    setUiMessage(reportError("serverSwitch", error));
    await refreshStatus();
  } finally {
    serverWorking = false;
    if (connectCancelTimer) {
      clearTimeout(connectCancelTimer);
      connectCancelTimer = null;
    }
    connectCancelAllowed = false;
    updateServerBusyIndicator(false);
    updateServerControlStates();
  }
}

serverDisconnectBtn.addEventListener("click", async () => {
  if (!daemonApi) return;
  notifyUserDisconnected();
  serverWorking = true;
  updateServerBusyIndicator(true, "Disconnecting...");
  updateServerControlStates();
  try {
    setUiMessage("Disconnecting...");
    const result = await daemonApi.disconnect();
    setUiMessage(result.ok ? "Disconnected." : "Couldn't disconnect. Please try again.");
    await refreshStatus();
  } catch (error) {
    setUiMessage(reportError("serverDisconnect", error));
  } finally {
    serverWorking = false;
    updateServerBusyIndicator(false);
    updateServerControlStates();
  }
});

serverRefreshBtn.addEventListener("click", async () => {
  await refreshServers();
});

// Settings toggles live inside the fullscreen overlay, which covers #uiMessage,
// so feedback is shown via showToast (renders above the overlay). Each toggle
// reverts its checkbox and surfaces the error if the backend call fails.
directIpToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  try {
    await pangeaApi.setDirectIp(directIpToggle.checked);
    showToast(directIpToggle.checked ? "Direct IP enabled." : "Direct IP disabled.", 3000, true);
  } catch (err) {
    directIpToggle.checked = !directIpToggle.checked;
    showToast(reportError("directIp", err, "Failed to update setting."));
  }
});

directIpOnlyToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  const enabled = directIpOnlyToggle.checked;
  try {
    await pangeaApi.setDirectIpOnly(enabled);
  } catch (err) {
    directIpOnlyToggle.checked = !enabled;
    showToast(reportError("directIpOnly", err, "Failed to update setting."));
    return;
  }
  if (enabled) {
    directIpToggle.checked = true;
    directIpToggle.disabled = true;
  } else {
    directIpToggle.disabled = false;
    // Forcing Direct IP on above was visual only; restore its real persisted value.
    try {
      directIpToggle.checked = await pangeaApi.getDirectIp();
    } catch {
      // keep current
    }
  }
  showToast(enabled ? "Direct IP only mode enabled." : "Direct IP only mode disabled.", 3000, true);
});

allowLanToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  try {
    await pangeaApi.setAllowLan(allowLanToggle.checked);
    showToast(allowLanToggle.checked
      ? "Allow LAN enabled. Reconnect for it to take effect."
      : "Allow LAN disabled. Reconnect for it to take effect.", 4000, true);
  } catch (err) {
    allowLanToggle.checked = !allowLanToggle.checked;
    showToast(reportError("allowLan", err, "Failed to update setting."));
  }
});

launchAtStartupToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  try {
    await pangeaApi.setLaunchAtStartup(launchAtStartupToggle.checked);
    showToast(launchAtStartupToggle.checked
      ? "PangeaVPN will launch at startup."
      : "Launch at startup disabled.", 3000, true);
  } catch (err) {
    launchAtStartupToggle.checked = !launchAtStartupToggle.checked;
    showToast(reportError("launchAtStartup", err, "Failed to update startup setting."));
  }
});

alwaysConnectedToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  alwaysConnectedLocal = alwaysConnectedToggle.checked;
  try {
    await pangeaApi.setAlwaysConnected(alwaysConnectedLocal);
  } catch (err) {
    alwaysConnectedLocal = !alwaysConnectedLocal;
    alwaysConnectedToggle.checked = alwaysConnectedLocal;
    showToast(reportError("lockdown", err, "Failed to update Lockdown."));
    return;
  }
  notifyToggleChanged(alwaysConnectedLocal);
  if (alwaysConnectedLocal) {
    showToast("Lockdown on — auto-connect enabled and the kill switch stays on until you turn it off.", 5000, true);
    if (lastServerIdLocal) {
      void attemptInitialAutoConnect();
    }
  } else {
    showToast("Lockdown off — normal internet restored.", 4000, true);
  }
});

async function refreshLastServer(): Promise<void> {
  if (!pangeaApi) return;
  try {
    const last = await pangeaApi.getLastServer();
    lastServerIdLocal = last.lastServerId;
  } catch {
    // best-effort
  }
}



const loadingScreen = document.getElementById("loadingScreen") as HTMLElement;
const loadingMessage = document.getElementById("loadingMessage") as HTMLParagraphElement;
const shell = document.querySelector<HTMLElement>(".shell")!;

function animateOut(el: HTMLElement): Promise<void> {
  return new Promise((resolve) => {
    el.style.transition = "opacity 250ms ease, transform 250ms ease";
    el.style.opacity = "1";
    el.style.transform = "translateX(0)";
    void el.offsetHeight;
    el.style.opacity = "0";
    el.style.transform = "translateX(-30px)";
    const done = () => {
      el.removeEventListener("transitionend", done);
      el.style.transition = "";
      el.style.opacity = "";
      el.style.transform = "";
      resolve();
    };
    el.addEventListener("transitionend", done, { once: true });
    setTimeout(done, 300);
  });
}

async function hideLoadingScreen(): Promise<void> {
  await animateOut(loadingScreen);
  loadingScreen.style.display = "none";
}

function showAppShell(): void {
  if (loginScreen.parentNode) {
    const ls = loginScreen;
    animateOut(ls).then(() => ls.remove());
  }
  shell.removeAttribute("hidden");
  shell.style.display = "";
}

function showLoginScreen(): void {
  shell.setAttribute("hidden", "");
  // Hide device limit screen if it was showing
  const dlScreen = document.getElementById("deviceLimitScreen");
  if (dlScreen) dlScreen.hidden = true;
  if (!loginScreen.parentNode) document.body.insertBefore(loginScreen, shell);
  loginScreen.hidden = false;
  loginScreen.style.opacity = "";
  loginScreen.style.transform = "";
  refreshCachedTokenBtn();
  loginTokenInput.focus();
}

async function init(): Promise<void> {
  initTheme();

  if (!daemonApi) {
    loadingMessage.textContent = "PangeaVPN couldn't start. Please restart the app.";
    return;
  }

  // Poll until daemon responds (max 30s)
  const maxAttempts = 60;
  for (let i = 0; i < maxAttempts; i++) {
    const remaining = Math.ceil((maxAttempts - i) * 0.5);
    loadingMessage.textContent = `Getting things ready... (${remaining}s)`;
    try {
      const status = await daemonApi.getStatus();
      if (status) break;
    } catch {
      // not ready
    }
    if (i === maxAttempts - 1) {
      loadingMessage.textContent = "PangeaVPN didn't start. Please restart the app.";
      return;
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  hideLoadingScreen();

  // Check auth state before showing any UI
  if (pangeaApi) {
    try {
      authState = await pangeaApi.getAuthState();
    } catch {
      // Auth unavailable
    }
  }
  updateAuthUI();

  // Listen for forced sign-out (e.g. device removed via website)
  window.onAuthInvalidated?.(() => {
    authState = { authenticated: false, user: null };
    servers = [];
    updateAuthUI();
    renderServers();
    showToast("You've been signed out. Please sign in again.");
  });

  initCollapsibleSections();
  await renderAppVersion();
  updateBusyIndicator();
  await refreshAll(true);

  if (pangeaApi) {
    try {
      directIpToggle.checked = await pangeaApi.getDirectIp();
      directIpOnlyToggle.checked = await pangeaApi.getDirectIpOnly();
      allowLanToggle.checked = await pangeaApi.getAllowLan();
      if (directIpOnlyToggle.checked) {
        directIpToggle.checked = true;
        directIpToggle.disabled = true;
      }
    } catch {
      // default off
    }

    try {
      const isPackaged = await pangeaApi.getIsPackaged();
      if (!isPackaged) {
        launchAtStartupToggle.disabled = true;
        launchAtStartupToggle.title = "Available in packaged builds only";
      } else {
        launchAtStartupToggle.checked = await pangeaApi.getLaunchAtStartup();
      }
      alwaysConnectedLocal = await pangeaApi.getAlwaysConnected();
      alwaysConnectedToggle.checked = alwaysConnectedLocal;
      const last = await pangeaApi.getLastServer();
      lastServerIdLocal = last.lastServerId;
    } catch {
      // defaults already in place
    }

    initAutoConnect({
      getEnabled: () => alwaysConnectedLocal,
      getAuthenticated: () => authState.authenticated,
      getDaemonState: () => currentDaemonState,
      getUserIntent,
      getLastServerId: () => lastServerIdLocal,
      provisionAndSwitch: (serverId: string) => pangeaApi.provisionAndSwitch(serverId)
    });

    await loadCachedServers();
    if (authState.authenticated) {
      await refreshServers();
    }

    if (alwaysConnectedLocal && lastServerIdLocal && authState.authenticated) {
      void attemptInitialAutoConnect();
    }
  }

  // Check for updates regardless of auth state.
  checkForUpdate();

  let pollInterval = 2000;
  const pollMin = 2000;
  const pollMax = 10000;

  function schedulePoll(): void {
    setTimeout(async () => {
      try {
        await refreshStatus();
        await refreshLogs();
        pollInterval = pollMin; // reset on success
      } catch {
        pollInterval = Math.min(pollInterval * 2, pollMax); // backoff on error
      }
      notifyStatusTick();
      schedulePoll();
    }, pollInterval);
  }
  schedulePoll();
}

const updateOverlay = document.getElementById("updateOverlay") as HTMLElement;
const updateCloseBtn = document.getElementById("updateCloseBtn") as HTMLButtonElement;
const updateCurrentVersionEl = document.getElementById("updateCurrentVersion") as HTMLSpanElement;
const updateLatestVersionEl = document.getElementById("updateLatestVersion") as HTMLSpanElement;
const updateDownloadBtn = document.getElementById("updateDownloadBtn") as HTMLButtonElement;
const updateMessageEl = document.getElementById("updateMessage") as HTMLParagraphElement;
const updateMacInstall = document.getElementById("updateMacInstall") as HTMLElement;
const updateMacCommand = document.getElementById("updateMacCommand") as HTMLElement;
const menuBadge = document.getElementById("menuBadge") as HTMLSpanElement;
const menuUpdateBtn = document.getElementById("menuUpdateBtn") as HTMLButtonElement;

const MAC_INSTALL_COMMAND = "curl -fsSL https://pangeavpn.org/install-mac.sh | bash";
const isMacPlatform = window.appPlatform === "darwin";

let pendingUpdate: { version: string; macOnly?: boolean } | null = null;
let updateDownloaded = false;
let currentAppVersion = "";
const UPDATE_DISMISSED_KEY = "pangea-vpn-update-dismissed";
const updater = window.autoUpdater;

function isUpdateDismissed(version: string): boolean {
  return localStorage.getItem(UPDATE_DISMISSED_KEY) === version;
}

function dismissUpdate(version: string): void {
  localStorage.setItem(UPDATE_DISMISSED_KEY, version);
}

function showUpdateModal(): void {
  if (!pendingUpdate || isUpdateDismissed(pendingUpdate.version)) return;
  updateCurrentVersionEl.textContent = currentAppVersion || "-";
  updateLatestVersionEl.textContent = pendingUpdate.version;
  updateDownloadBtn.disabled = false;
  if (isMacPlatform) {
    updateMacCommand.textContent = MAC_INSTALL_COMMAND;
    updateMacInstall.hidden = false;
    updateDownloadBtn.textContent = "Copy Install Command";
    updateMessageEl.textContent = "";
  } else {
    updateMacInstall.hidden = true;
    if (updateDownloaded) {
      updateDownloadBtn.textContent = "Restart to Update";
      updateMessageEl.textContent = "Update downloaded and ready to install.";
    } else {
      updateDownloadBtn.textContent = "Download Update";
      updateMessageEl.textContent = "";
    }
  }
  updateOverlay.classList.add("visible");
}

function hideUpdateModal(): void {
  if (pendingUpdate) dismissUpdate(pendingUpdate.version);
  updateOverlay.classList.remove("visible");
}

// Wire up electron-updater push events
if (updater) {
  updater.onUpdateAvailable((info) => {
    pendingUpdate = { version: info.version, macOnly: info.macOnly };
    menuBadge.hidden = false;
    menuUpdateBtn.hidden = false;
    showUpdateModal();
  });

  updater.onUpdateDownloaded(() => {
    updateDownloaded = true;
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "Restart to Update";
    updateMessageEl.textContent = "Update downloaded and ready to install.";
  });

  updater.onUpdateError((message) => {
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "Retry Download";
    updateMessageEl.textContent = message;
  });
}

updateCloseBtn.addEventListener("click", (e) => {
  e.stopPropagation();
  hideUpdateModal();
});

updateOverlay.addEventListener("click", (e) => {
  if (e.target === updateOverlay) hideUpdateModal();
});

menuUpdateBtn.addEventListener("click", () => {
  menuDropdown.classList.remove("open");
  menuBtn.setAttribute("aria-expanded", "false");
  if (pendingUpdate) localStorage.removeItem(UPDATE_DISMISSED_KEY);
  showUpdateModal();
});

// checkForUpdates() resolves with the latest release regardless of whether it's
// newer, so compare against the running version to decide the message. When it
// IS newer the onUpdateAvailable event also fires and surfaces the full modal.
function isNewerVersion(candidate: string, current: string): boolean {
  const parse = (v: string): number[] => v.replace(/^v/, "").split(".").map((n) => parseInt(n, 10) || 0);
  const a = parse(candidate);
  const b = parse(current);
  for (let i = 0; i < Math.max(a.length, b.length); i++) {
    const x = a[i] ?? 0;
    const y = b[i] ?? 0;
    if (x !== y) return x > y;
  }
  return false;
}

checkUpdatesBtn.addEventListener("click", async () => {
  if (!updater) {
    showToast("Updates aren't available in this build.");
    return;
  }
  checkUpdatesBtn.disabled = true;
  const label = checkUpdatesBtn.textContent;
  checkUpdatesBtn.textContent = "Checking...";
  try {
    const info = await updater.checkForUpdates();
    if (info && isNewerVersion(info.version, currentAppVersion)) {
      // Open the update modal directly so it's actionable from Settings, even if
      // this version was dismissed earlier (which would otherwise suppress it).
      pendingUpdate = pendingUpdate ?? { version: info.version };
      localStorage.removeItem(UPDATE_DISMISSED_KEY);
      showUpdateModal();
    } else if (info) {
      showToast("You're on the latest version.", 4000, true);
    } else {
      showToast("Couldn't check for updates. Try again later.");
    }
  } catch {
    showToast("Couldn't check for updates. Try again later.");
  } finally {
    checkUpdatesBtn.textContent = label;
    checkUpdatesBtn.disabled = false;
  }
});

async function copyMacInstallCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(MAC_INSTALL_COMMAND);
    updateDownloadBtn.textContent = "Copied!";
    updateMessageEl.textContent = "Now paste the command into Terminal and press Enter.";
    setTimeout(() => {
      updateDownloadBtn.textContent = "Copy Install Command";
    }, 2000);
  } catch (error) {
    updateMessageEl.textContent = reportError("updateCopyCommand", error);
  }
}

updateMacCommand.addEventListener("click", () => {
  void copyMacInstallCommand();
});

updateDownloadBtn.addEventListener("click", async () => {
  if (!pendingUpdate) return;

  if (isMacPlatform) {
    await copyMacInstallCommand();
    return;
  }

  if (!updater) return;

  if (updateDownloaded) {
    updater.installUpdate();
    return;
  }

  updateDownloadBtn.disabled = true;
  updateDownloadBtn.textContent = "Opening...";
  updateMessageEl.textContent = "";

  try {
    await updater.downloadUpdate();
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "View Download";
  } catch (error) {
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "Retry";
    updateMessageEl.textContent = reportError("updateDownload", error);
  }
});

async function checkForUpdate(): Promise<void> {
  if (!updater) return;
  try {
    await updater.checkForUpdates();
  } catch {
    // non-critical
  }
}

async function renderAppVersion(): Promise<void> {
  if (!daemonApi) {
    appVersionEl.textContent = "v-";
    return;
  }

  try {
    const version = await daemonApi.getAppVersion();
    currentAppVersion = version;
    appVersionEl.textContent = `v${version}`;
  } catch {
    appVersionEl.textContent = "v-";
  }
}

// Logs is a debug/advanced area; reveal it only when verbose errors are on.
function applyVerboseErrorsUi(): void {
  logsSection.hidden = !verboseErrors;
}
applyVerboseErrorsUi();

// Tap version label 5 times to toggle verbose error messages
{
  let versionTapCount = 0;
  let versionTapTimer = 0;
  appVersionEl.style.cursor = "pointer";
  appVersionEl.addEventListener("click", () => {
    versionTapCount++;
    clearTimeout(versionTapTimer);
    versionTapTimer = window.setTimeout(() => { versionTapCount = 0; }, 1500);
    if (versionTapCount >= 5) {
      versionTapCount = 0;
      verboseErrors = !verboseErrors;
      localStorage.setItem("pangea:verboseErrors", verboseErrors ? "1" : "0");
      applyVerboseErrorsUi();
      showToast(verboseErrors ? "Verbose errors enabled" : "Verbose errors disabled");
    }
  });
}

async function refreshAll(showIndicator = false): Promise<void> {
  if (showIndicator) {
    uiRefreshing = true;
    updateBusyIndicator();
  }

  try {
    await Promise.all([refreshStatus(), refreshLogs()]);
    setUiMessage("Ready.");
  } catch (error) {
    console.error("[daemonSync]", error);
    setUiMessage("Something went wrong. Retrying...");
    // Retry once after a short delay
    try {
      await new Promise((r) => setTimeout(r, 2000));
      await Promise.all([refreshStatus(), refreshLogs()]);
      setUiMessage("Ready.");
    } catch (retryError) {
      setUiMessage(reportError("daemonSyncRetry", retryError));
    }
  } finally {
    if (showIndicator) {
      uiRefreshing = false;
      updateBusyIndicator();
    }
  }
}

async function buildDiagnosticsReport(): Promise<string> {
  const now = new Date();
  const [statusResult, configResult, logsResult, appVersionResult] = await Promise.all([
    readDiagnosticValue(() => daemonApi?.getStatus()),
    readDiagnosticValue(() => daemonApi?.getConfig()),
    readDiagnosticValue(() => daemonApi?.getLogs()),
    readDiagnosticValue(() => daemonApi?.getAppVersion())
  ]);

  const payload = {
    generatedAtUtc: now.toISOString(),
    generatedAtLocal: now.toString(),
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    appVersion: appVersionResult,
    platform: navigator.platform,
    userAgent: navigator.userAgent,
    daemonStatus: statusResult,
    daemonConfig: redactSensitiveConfig(configResult),
    daemonLogs: logsResult
  };

  return `PangeaVPN Diagnostics\n\n${JSON.stringify(payload, null, 2)}\n`;
}

async function readDiagnosticValue<T>(reader: () => Promise<T> | undefined): Promise<T | { error: string }> {
  try {
    const value = reader();
    if (!value) {
      return { error: "daemonApi unavailable" };
    }
    return await value;
  } catch (error) {
    return { error: String(error) };
  }
}

function redactSensitiveConfig(value: unknown): unknown {
  if (!value || typeof value !== "object") {
    return value;
  }

  const cloned = JSON.parse(JSON.stringify(value)) as { profiles?: Array<Record<string, unknown>> };
  if (!Array.isArray(cloned.profiles)) {
    return cloned;
  }

  for (const profile of cloned.profiles) {
    const cloak = profile.cloak as Record<string, unknown> | undefined;
    if (cloak && typeof cloak === "object" && typeof cloak.password === "string" && cloak.password.length > 0) {
      cloak.password = "<redacted>";
    }

    const wireguard = profile.wireguard as Record<string, unknown> | undefined;
    if (wireguard && typeof wireguard === "object" && typeof wireguard.configText === "string") {
      wireguard.configText = redactWireGuardConfigText(wireguard.configText);
    }
  }

  return cloned;
}

function redactWireGuardConfigText(configText: string): string {
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

async function copyTextToClipboard(text: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(text);
    return;
  } catch {
    // Continue with legacy fallback.
  }

  const textarea = document.createElement("textarea");
  textarea.value = text;
  textarea.setAttribute("readonly", "true");
  textarea.style.position = "fixed";
  textarea.style.opacity = "0";
  textarea.style.pointerEvents = "none";
  document.body.append(textarea);
  textarea.focus();
  textarea.select();

  const copied = document.execCommand("copy");
  textarea.remove();
  if (!copied) {
    throw new Error("clipboard copy not available");
  }
}

async function refreshStatus(): Promise<StatusResponse | null> {
  if (!daemonApi) {
    return null;
  }
  try {
    const status = await daemonApi.getStatus();
    renderStatus(status);
    return status;
  } catch (error) {
    setUiMessage(reportError("status", error));
    return null;
  }
}

async function refreshLogs(): Promise<void> {
  if (!daemonApi) {
    return;
  }
  try {
    const since = logsCursor > 0 ? logsCursor + 1 : 0;
    const entries = await daemonApi.getLogs(since);
    if (entries.length > 0) {
      logsCursor = entries[entries.length - 1].ts;
      logEntries = [...logEntries, ...entries];
      if (logEntries.length > 4000) {
        logEntries = logEntries.slice(-4000);
      }
    }
    renderLogs(logEntries);
  } catch (error) {
    setUiMessage(reportError("logFetch", error));
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

let lastCloakWasDown = false;

function renderStatus(status: StatusResponse): void {
  latestStatus = status;
  currentDaemonState = status.state;
  stateEl.textContent = status.state;
  detailEl.textContent = status.detail;

  const cloakPid = status.cloak.pid ?? "none";
  cloakEl.textContent = `${status.cloak.running ? "running" : "stopped"} (pid: ${cloakPid})`;
  wireguardEl.textContent = `${status.wireguard.running ? "running" : "stopped"} (${status.wireguard.detail})`;

  // Drive hero card state
  heroCard.dataset.state = status.state;
  cloakDot.classList.toggle("on", status.cloak.running);
  wgDot.classList.toggle("on", status.wireguard.running);

  // Kill switch pill
  const ksActive = (status as StatusResponse & { killSwitchActive?: boolean }).killSwitchActive ?? false;
  ksDot.classList.toggle("on", ksActive);

  // Throughput stats
  const connected = status.state === "CONNECTED";
  const wg = status.wireguard as StatusResponse["wireguard"] & { bytesIn?: number; bytesOut?: number };
  throughputPanel.hidden = !connected;
  if (connected) {
    rxBytesEl.textContent = formatBytes(wg.bytesIn ?? 0);
    txBytesEl.textContent = formatBytes(wg.bytesOut ?? 0);
  }

  // Recovery toast — cloak was down last poll, now it's back
  if (lastCloakWasDown && status.cloak.running && connected) {
    showToast("Connection recovered.");
  }
  lastCloakWasDown = !status.cloak.running && connected;

  // Show connect vs disconnect button.
  // Show disconnect in ERROR state too — kill switch may still be active.
  const showDisconnect = connected || status.state === "CONNECTING" || status.state === "ERROR";
  serverConnectBtn.hidden = showDisconnect;
  serverDisconnectBtn.hidden = !showDisconnect;

  updateControlStates();
  updateBusyIndicator();
}

function renderLogs(entries: LogEntry[]): void {
  const lines = entries.slice(-300).map((entry) => {
    const date = new Date(entry.ts).toLocaleTimeString();
    return `[${date}] ${entry.level.toUpperCase()} ${entry.source}: ${entry.msg}`;
  });

  logsEl.textContent = lines.join("\n");
  logsEl.scrollTop = logsEl.scrollHeight;
}

function initTheme(): void {
  let stored: string | null = null;
  try {
    stored = window.localStorage.getItem(THEME_STORAGE_KEY);
  } catch {
    stored = null;
  }

  const theme: ThemeMode = stored === "dark" || stored === null ? "dark" : "light";
  applyTheme(theme);
}

function applyTheme(theme: ThemeMode): void {
  document.documentElement.dataset.theme = theme;
  document.body.dataset.theme = theme;
  themeToggleBtn.textContent = theme === "dark" ? "\u2600" : "\u263D";
  themeToggleBtn.setAttribute("aria-pressed", String(theme === "dark"));

  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Ignore storage failures in restricted environments.
  }
}

function setUiMessage(message: string): void {
  uiMessageEl.textContent = message;
}

function showToast(message: string, durationMs = 5000, success = false): void {
  const container = document.getElementById("toastContainer");
  if (!container) return;

  const toast = document.createElement("div");
  toast.className = success ? "toast toast-success" : "toast";
  toast.textContent = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.classList.add("toast-out");
    toast.addEventListener("animationend", () => toast.remove(), { once: true });
  }, durationMs);
}

function updateBusyIndicator(): void {
  updateControlStates();
}

function updateAuthUI(): void {
  if (authState.authenticated) {
    showAppShell();
    loginBtn.hidden = true;
    menuDevicesBtn.hidden = false;
    accountUserLabel.textContent = authState.user?.email || authState.user?.name || "—";
    refreshAccountToken();
    serverPanel.hidden = false;
  } else {
    showLoginScreen();
    loginBtn.hidden = !pangeaApi;
    menuDevicesBtn.hidden = true;
    accountUserLabel.textContent = "—";
    closeSettings();
    serverPanel.hidden = true;
  }
  updateServerControlStates();
}

async function loadCachedServers(): Promise<void> {
  if (!pangeaApi) return;
  try {
    const cached = await pangeaApi.getCachedServers();
    if (cached.length > 0 && servers.length === 0) {
      servers = cached;
      renderServers();
    }
  } catch {
    // cache miss is fine
  }
}

// Fetch the latest server list and re-render. Returns true on success; never throws.
async function fetchServers(): Promise<boolean> {
  if (!pangeaApi) return false;
  try {
    servers = await pangeaApi.getServers();
    renderServers();
    pangeaApi.cacheServers(servers).catch(() => {});
    return true;
  } catch (error) {
    console.warn("[fetchServers]", error instanceof Error ? error.message : error);
    return false;
  }
}

async function refreshServers(): Promise<void> {
  if (!(await fetchServers())) {
    setUiMessage("Couldn't refresh the server list. It will retry automatically.");
  }
}

// Refresh with backoff until it succeeds, so the list (and its load values) is
// always current. Triggered when the picker opens or the app is shown. A newer
// call supersedes the loop; it also stops on sign-out or while the window is hidden.
let serverRefreshGeneration = 0;
async function refreshServersWithRetry(): Promise<void> {
  if (!authState.authenticated || !pangeaApi) return;
  const gen = ++serverRefreshGeneration;
  const backoffMs = [0, 1000, 2000, 4000, 8000, 15000];
  for (let attempt = 0; ; attempt++) {
    if (gen !== serverRefreshGeneration || !authState.authenticated || document.hidden) return;
    if (await fetchServers()) return;
    await new Promise((resolve) => setTimeout(resolve, backoffMs[Math.min(attempt, backoffMs.length - 1)]));
  }
}

// Small load indicator (bar + %) for a server row. Returns null when load is
// unknown (older hub / node offline) so we never imply a state we don't have.
function buildLoadIndicator(load: number | null | undefined): HTMLElement | null {
  if (typeof load !== "number" || !Number.isFinite(load)) return null;
  const pct = Math.max(0, Math.min(100, Math.round(load)));
  const level = pct < 40 ? "low" : pct < 75 ? "mid" : "high";
  const el = document.createElement("div");
  el.className = `server-picker-overlay-item-load load-${level}`;
  el.title = `Server load ${pct}%`;
  const bar = document.createElement("span");
  bar.className = "load-bar";
  const fill = document.createElement("span");
  fill.className = "load-fill";
  fill.style.width = `${pct}%`;
  bar.append(fill);
  const label = document.createElement("span");
  label.className = "load-pct";
  label.textContent = `${pct}%`;
  el.append(bar, label);
  return el;
}

function renderServers(): void {
  const previousValue = serverSelect.value;
  serverSelect.innerHTML = "";
  serverPickerOverlayList.innerHTML = "";

  if (servers.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No servers available";
    serverSelect.append(option);
    serverSelect.disabled = true;
    serverPickerBtn.disabled = true;
    serverPickerLabel.textContent = "No servers available";
    serverConnectBtn.disabled = true;
    const empty = document.createElement("div");
    empty.className = "server-picker-overlay-empty";
    empty.textContent = "No servers available";
    serverPickerOverlayList.append(empty);
    return;
  }

  for (const server of servers) {
    const option = document.createElement("option");
    option.value = server.id;
    option.textContent = `${server.name} (${server.id})`;
    serverSelect.append(option);

    const item = document.createElement("div");
    item.className = "server-picker-overlay-item";
    item.dataset.value = server.id;
    item.setAttribute("role", "option");
    item.tabIndex = 0;

    const text = document.createElement("div");
    text.className = "server-picker-overlay-item-text";
    const nameSpan = document.createElement("span");
    nameSpan.className = "server-picker-overlay-item-name";
    nameSpan.textContent = server.name;
    const regionSpan = document.createElement("span");
    regionSpan.className = "server-picker-overlay-item-region";
    regionSpan.textContent = [server.country, server.region].filter(Boolean).join(" · ") || server.id;
    text.append(nameSpan, regionSpan);

    const idSpan = document.createElement("span");
    idSpan.className = "server-picker-overlay-item-id";
    idSpan.textContent = server.id;

    const right = document.createElement("div");
    right.className = "server-picker-overlay-item-right";
    right.append(idSpan);
    const loadEl = buildLoadIndicator(server.load);
    if (loadEl) right.append(loadEl);

    item.append(text, right);
    const activate = (): void => {
      closeServerPicker();
      if (serverWorking) return;
      // Only commit the selection when an action will actually run, so the
      // picker label can't drift out of sync with the connected server.
      if (currentDaemonState === "CONNECTED") {
        serverSelect.value = server.id;
        syncServerPicker();
        void switchToServer(server.id);
      } else if (!serverConnectBtn.disabled) {
        serverSelect.value = server.id;
        syncServerPicker();
        serverConnectBtn.click();
      }
    };
    item.addEventListener("click", activate);
    item.addEventListener("keydown", (e) => {
      if (e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        activate();
      }
    });
    serverPickerOverlayList.append(item);
  }

  serverSelect.disabled = false;
  serverPickerBtn.disabled = false;
  const hasSelection = servers.some((s) => s.id === previousValue);
  serverSelect.value = hasSelection ? previousValue : servers[0].id;
  syncServerPicker();

  updateServerControlStates();
}

function syncServerPicker(): void {
  const selected = servers.find((s) => s.id === serverSelect.value);
  if (selected) {
    serverPickerLabel.textContent = "";
    const nameText = document.createTextNode(selected.name + " ");
    const idSpan = document.createElement("span");
    idSpan.className = "server-picker-label-id";
    idSpan.textContent = selected.id;
    serverPickerLabel.append(nameText, idSpan);
  } else {
    serverPickerLabel.textContent = "Select server";
  }

  for (const opt of Array.from(serverPickerOverlayList.children)) {
    const el = opt as HTMLElement;
    if (el.dataset.value !== undefined) {
      const isSelected = el.dataset.value === serverSelect.value;
      el.classList.toggle("selected", isSelected);
      el.setAttribute("aria-selected", String(isSelected));
    }
  }
}

function updateServerControlStates(): void {
  if (!authState.authenticated || servers.length === 0) {
    serverConnectBtn.disabled = true;
    serverDisconnectBtn.disabled = true;
    return;
  }

  const daemonBusy = currentDaemonState === "CONNECTING" || currentDaemonState === "DISCONNECTING";
  const busy = uiRefreshing || uiWorking || serverWorking || daemonBusy;

  const fullyDisconnected = latestStatus
    ? latestStatus.state === "DISCONNECTED" && !latestStatus.cloak.running && !latestStatus.wireguard.running
    : true;

  serverConnectBtn.disabled = busy || !fullyDisconnected || !serverSelect.value;
  // Allow cancel mid-connect once the 1s grace window has elapsed, so the
  // user can bail before the 10s cloak timeout. Outside that window the
  // standard disabled-while-busy rule applies.
  const cancelAllowedNow = serverWorking && connectCancelAllowed;
  serverDisconnectBtn.disabled = cancelAllowedNow
    ? false
    : !latestStatus || latestStatus.state === "DISCONNECTED" || latestStatus.state === "DISCONNECTING" || busy;
}

function updateServerBusyIndicator(active: boolean, label?: string): void {
  serverIndicator.classList.toggle("active", active);
  serverIndicator.setAttribute("aria-hidden", String(!active));
  if (label) {
    serverIndicatorLabel.textContent = label;
  }
}

function updateControlStates(): void {
  updateServerControlStates();
}

function loadCollapseStates(): Record<string, boolean> {
  try {
    const raw = localStorage.getItem(COLLAPSE_STATE_KEY);
    return raw ? JSON.parse(raw) : {};
  } catch {
    return {};
  }
}

function saveCollapseState(key: string, open: boolean): void {
  const states = loadCollapseStates();
  states[key] = open;
  localStorage.setItem(COLLAPSE_STATE_KEY, JSON.stringify(states));
}

function initCollapsibleSections(): void {
  const saved = loadCollapseStates();
  for (const section of collapsibleSections) {
    const toggle = section.querySelector<HTMLButtonElement>(".collapse-toggle");
    const content = section.querySelector<HTMLElement>(".collapse-content");
    if (!toggle || !content) {
      continue;
    }

    const key = section.dataset.collapsible ?? "";
    const initialOpen = key ? (saved[key] ?? false) : false;
    setCollapseState(section, content, toggle, initialOpen, false);
    toggle.addEventListener("click", () => {
      const nextOpen = !section.classList.contains("is-open");
      setCollapseState(section, content, toggle, nextOpen, true);
      if (key) saveCollapseState(key, nextOpen);
    });
  }
}

function setCollapseState(
  section: HTMLElement,
  content: HTMLElement,
  toggle: HTMLButtonElement,
  open: boolean,
  animate: boolean
): void {
  toggle.setAttribute("aria-expanded", String(open));
  content.setAttribute("aria-hidden", String(!open));
  content.inert = !open;

  if (open) {
    section.classList.add("is-open");
    if (!animate) {
      content.style.transition = "none";
    }

    content.style.maxHeight = `${content.scrollHeight}px`;

    const finalizeOpen = () => {
      if (section.classList.contains("is-open")) {
        content.style.maxHeight = "none";
      }
      content.removeEventListener("transitionend", finalizeOpen);
    };

    if (animate) {
      content.addEventListener("transitionend", finalizeOpen);
    } else {
      content.style.maxHeight = "none";
      void content.offsetHeight;
      content.style.transition = "";
    }
    return;
  }

  if (content.style.maxHeight === "none" || !content.style.maxHeight) {
    content.style.maxHeight = `${content.scrollHeight}px`;
    void content.offsetHeight;
  }

  if (!animate) {
    content.style.transition = "none";
  }

  section.classList.remove("is-open");
  content.style.maxHeight = "0px";

  if (!animate) {
    void content.offsetHeight;
    content.style.transition = "";
  }
}

void init();
