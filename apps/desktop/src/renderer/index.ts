import type { LogEntry, Profile, StatusResponse } from "@pangeavpn/shared-types";

let verboseErrors = localStorage.getItem("pangea:verboseErrors") === "1";

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

const stateEl = must<HTMLSpanElement>("state");
const detailEl = must<HTMLSpanElement>("detail");
const cloakEl = must<HTMLSpanElement>("cloak");
const wireguardEl = must<HTMLSpanElement>("wireguard");
const ksDot = must<HTMLElement>("ksDot");
const throughputPanel = must<HTMLElement>("throughputPanel");
const txBytesEl = must<HTMLSpanElement>("txBytes");
const rxBytesEl = must<HTMLSpanElement>("rxBytes");
const profileSelect = must<HTMLSelectElement>("profileSelect");
const connectBtn = must<HTMLButtonElement>("connectBtn");
const disconnectBtn = must<HTMLButtonElement>("disconnectBtn");
const refreshBtn = must<HTMLButtonElement>("refreshBtn");
const refreshIndicator = must<HTMLSpanElement>("refreshIndicator");
const refreshIndicatorLabel = must<HTMLSpanElement>("refreshIndicatorLabel");
const themeToggleBtn = must<HTMLButtonElement>("themeToggleBtn");
const uiMessageEl = must<HTMLParagraphElement>("uiMessage");
const appVersionEl = must<HTMLSpanElement>("appVersion");
const toggleManualBuilderBtn = must<HTMLButtonElement>("toggleManualBuilderBtn");
const copyDiagnosticsBtn = must<HTMLButtonElement>("copyDiagnosticsBtn");
const copyLogsBtn = must<HTMLButtonElement>("copyLogsBtn");
const clearLogsBtn = must<HTMLButtonElement>("clearLogsBtn");
const logsEl = must<HTMLDivElement>("logs");
const collapsibleSections = Array.from(document.querySelectorAll<HTMLElement>("[data-collapsible]"));
const encodedImportInput = must<HTMLTextAreaElement>("encodedImportInput");
const importEncodedProfileBtn = must<HTMLButtonElement>("importEncodedProfileBtn");
const manualBuilderSection = must<HTMLElement>("manualBuilderSection");
const manualBuilderForm = must<HTMLFormElement>("manualBuilderForm");
const exportEncodedProfileBtn = must<HTMLButtonElement>("exportEncodedProfileBtn");
const copyEncodedExportBtn = must<HTMLButtonElement>("copyEncodedExportBtn");
const deleteManualProfileBtn = must<HTMLButtonElement>("deleteManualProfileBtn");
const encodedExportOutput = must<HTMLTextAreaElement>("encodedExportOutput");
const manualName = must<HTMLInputElement>("manualName");
const manualId = must<HTMLInputElement>("manualId");
const manualTunnelName = must<HTMLInputElement>("manualTunnelName");
const manualRemoteHost = must<HTMLInputElement>("manualRemoteHost");
const manualUid = must<HTMLInputElement>("manualUid");
const manualPublicKey = must<HTMLInputElement>("manualPublicKey");
const manualWgConfigText = must<HTMLTextAreaElement>("manualWgConfigText");

const daemonApi = window.daemonApi;
const pangeaApi = window.pangeaApi;

const loginBtn = must<HTMLButtonElement>("loginBtn");
const authBar = must<HTMLElement>("authBar");
const authUserLabel = must<HTMLSpanElement>("authUserLabel");
const logoutBtn = must<HTMLButtonElement>("logoutBtn");
const serverPanel = must<HTMLElement>("serverPanel");
const serverSelect = must<HTMLSelectElement>("serverSelect");
const serverConnectBtn = must<HTMLButtonElement>("serverConnectBtn");
const serverDisconnectBtn = must<HTMLButtonElement>("serverDisconnectBtn");
const serverRefreshBtn = must<HTMLButtonElement>("serverRefreshBtn");
const serverIndicator = must<HTMLSpanElement>("serverIndicator");
const serverIndicatorLabel = must<HTMLSpanElement>("serverIndicatorLabel");
const directIpToggle = must<HTMLInputElement>("directIpToggle");
const directIpOnlyToggle = must<HTMLInputElement>("directIpOnlyToggle");
const loginScreen = must<HTMLElement>("loginScreen");
const loginScreenBtn = must<HTMLButtonElement>("loginScreenBtn");
const loginScreenMessage = must<HTMLParagraphElement>("loginScreenMessage");
const heroCard = must<HTMLElement>("heroCard");
const menuBtn = must<HTMLButtonElement>("menuBtn");
const menuDropdown = must<HTMLElement>("menuDropdown");
const manageSubLink = must<HTMLAnchorElement>("manageSubLink");
const serverPickerBtn = must<HTMLButtonElement>("serverPickerBtn");
const serverPickerLabel = must<HTMLElement>("serverPickerLabel");
const serverPickerDropdown = must<HTMLElement>("serverPickerDropdown");
const heroMeta = must<HTMLElement>("heroMeta");
const heroServer = must<HTMLElement>("heroServer");
const cloakDot = must<HTMLElement>("cloakDot");
const wgDot = must<HTMLElement>("wgDot");

type ThemeMode = "light" | "dark";
const THEME_STORAGE_KEY = "pangea-vpn-theme";
const LAST_PROFILE_KEY = "pangea-vpn-last-profile";
const COLLAPSE_STATE_KEY = "pangea-vpn-collapse-state";

type ImportedProfilePayload = {
  profilename: string;
  remotehost: string;
  cloakuid: string;
  cloakpubkey: string;
  wgconfig: string;
};

let profiles: Profile[] = [];
let currentDaemonState: StatusResponse["state"] = "DISCONNECTED";
let latestStatus: StatusResponse | null = null;
let uiRefreshing = false;
let uiWorking = false;
let logsCursor = 0;
let logEntries: LogEntry[] = [];
let authState: AuthState = { authenticated: false, user: null };
let servers: ServerInfo[] = [];
let serverWorking = false;

profileSelect.addEventListener("change", () => {
  const selected = profiles.find((profile) => profile.id === profileSelect.value);
  if (selected) {
    fillManualProfileForm(selected);
    localStorage.setItem(LAST_PROFILE_KEY, selected.id);
  }
});

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
  menuDropdown.classList.remove("open");
  menuBtn.setAttribute("aria-expanded", "false");
  window.openExternal?.("https://pangeavpn.org");
});

serverPickerBtn.addEventListener("click", () => {
  const open = serverPickerDropdown.hidden;
  serverPickerDropdown.hidden = !open;
  serverPickerBtn.setAttribute("aria-expanded", String(open));
});

document.addEventListener("click", (e) => {
  const target = e.target as Node;
  if (!serverPickerDropdown.hidden && !serverPickerBtn.contains(target) && !serverPickerDropdown.contains(target)) {
    serverPickerDropdown.hidden = true;
    serverPickerBtn.setAttribute("aria-expanded", "false");
  }
});

connectBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  const selectedId = profileSelect.value;
  if (!selectedId) {
    setUiMessage("No profile selected.");
    return;
  }

  uiWorking = true;
  updateBusyIndicator();
  try {
    setUiMessage("Connecting...");
    const result = await daemonApi.connect(selectedId);
    if (!result.ok) {
      const status = await refreshStatus();
      const detail = status?.detail ?? "";
      setUiMessage(detail ? `Connect failed: ${detail}` : "Connect failed. Check logs for details.");
      return;
    }

    const status = await refreshStatus();
    if (status?.state === "CONNECTED") {
      setUiMessage("Connected.");
    } else if (status?.state === "ERROR") {
      setUiMessage(`Connect failed: ${status.detail}`);
    } else {
      setUiMessage(`Connect requested. Current state: ${status?.state ?? "unknown"}.`);
    }
  } catch (error) {
    const msg = String(error);
    if (msg.includes("timeout") || msg.includes("ETIMEDOUT")) {
      console.error("[connect]", error);
      setUiMessage("Connect failed: connection timed out. Check your network.");
    } else {
      setUiMessage(reportError("connect", error));
    }
  } finally {
    uiWorking = false;
    updateBusyIndicator();
  }
});

disconnectBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  uiWorking = true;
  updateBusyIndicator();
  try {
    setUiMessage("Disconnecting...");
    const result = await daemonApi.disconnect();
    if (result.ok) {
      setUiMessage("Disconnected.");
    } else {
      const status = await refreshStatus();
      const detail = status?.detail ?? "";
      setUiMessage(detail ? `Disconnect failed: ${detail}` : "Disconnect failed. Check logs for details.");
      return;
    }
    await refreshStatus();
  } catch (error) {
    setUiMessage(reportError("disconnect", error));
  } finally {
    uiWorking = false;
    updateBusyIndicator();
  }
});

refreshBtn.addEventListener("click", async () => {
  refreshBtn.disabled = true;
  await refreshAll(true);
  setTimeout(() => { refreshBtn.disabled = false; }, 2000);
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

toggleManualBuilderBtn.addEventListener("click", () => {
  setManualBuilderVisibility(manualBuilderSection.hidden);
});

manualBuilderForm.addEventListener("submit", async (event) => {
  event.preventDefault();

  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  try {
    const nextProfile = profileFromManualForm();
    const result = await daemonApi.setConfig(upsertProfile(profiles, nextProfile));
    if (!result.ok) {
      setUiMessage("Failed to save manual profile.");
      return;
    }

    await refreshConfig(nextProfile.id);
    setUiMessage(`Saved manual profile "${nextProfile.name}".`);
  } catch (error) {
    setUiMessage(reportError("manualProfileSave", error));
  }
});

exportEncodedProfileBtn.addEventListener("click", () => {
  try {
    const manualProfile = profileFromManualForm();
    const payload = importedPayloadFromProfile(manualProfile);
    encodedExportOutput.value = encodeUtf8ToBase64(JSON.stringify(payload));
    encodedExportOutput.focus();
    encodedExportOutput.select();
    setUiMessage(`Exported encoded profile for "${manualProfile.name}".`);
  } catch (error) {
    setUiMessage(reportError("encodedExport", error));
  }
});

copyEncodedExportBtn.addEventListener("click", async () => {
  const encodedPayload = encodedExportOutput.value.trim();
  if (!encodedPayload) {
    setUiMessage("No encoded payload to copy.");
    return;
  }

  try {
    await navigator.clipboard.writeText(encodedPayload);
    setUiMessage("Encoded payload copied to clipboard.");
  } catch (error) {
    setUiMessage(reportError("copyEncodedPayload", error));
  }
});

deleteManualProfileBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  const selectedId = profileSelect.value;
  if (!selectedId) {
    setUiMessage("No profile selected to delete.");
    return;
  }

  const result = await daemonApi.setConfig(profiles.filter((profile) => profile.id !== selectedId));
  if (!result.ok) {
    setUiMessage("Failed to delete profile.");
    return;
  }

  await refreshConfig();
  if (profiles.length === 0) {
    clearManualProfileForm();
  }
  setUiMessage(`Deleted profile "${selectedId}".`);
});

importEncodedProfileBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage("daemonApi bridge unavailable.");
    return;
  }

  const encodedPayload = encodedImportInput.value.trim();
  if (!encodedPayload) {
    setUiMessage("Encoded profile payload is required.");
    return;
  }

  uiWorking = true;
  updateBusyIndicator();
  try {
    const importedPayload = parseImportedProfilePayload(decodeBase64ToUtf8(encodedPayload));
    const importedProfile = profileFromImportedPayload(importedPayload);
    const result = await daemonApi.setConfig(upsertProfile(profiles, importedProfile));
    if (!result.ok) {
      setUiMessage("Failed to import profile.");
      return;
    }

    await refreshConfig(importedProfile.id);
    encodedImportInput.value = "";
    setUiMessage(`Imported profile "${importedProfile.name}".`);
  } catch (error) {
    setUiMessage(reportError("import", error));
  } finally {
    uiWorking = false;
    updateBusyIndicator();
  }
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
    authState = { authenticated: false, user: null };
    servers = [];
    updateAuthUI();
    renderServers();
    await refreshStatus();
    await refreshConfig();
    setUiMessage("Signed out.");
  } catch (error) {
    setUiMessage(reportError("signOut", error));
  } finally {
    logoutBtn.disabled = false;
  }
});

const loginTokenInput = must<HTMLInputElement>("loginTokenInput");
const cachedTokenBtn = must<HTMLButtonElement>("cachedTokenBtn");

// Show cached token button if a previous token exists
function refreshCachedTokenBtn(): void {
  const cached = localStorage.getItem("pangea:lastToken");
  if (cached) {
    cachedTokenBtn.textContent = cached;
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

const loginDashboardLink = must<HTMLAnchorElement>("loginDashboardLink");
loginDashboardLink.addEventListener("click", (e) => {
  e.preventDefault();
  window.openExternal?.("https://pangeavpn.org/");
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
    updateAuthUI();
    if (authState.authenticated) {
      localStorage.setItem("pangea:lastToken", token);
      loginScreenMessage.textContent = "";
      loginTokenInput.value = "";
      await refreshServers();
    } else if (authState.error) {
      loginScreenMessage.textContent = authState.error;
    } else {
      loginScreenMessage.textContent = "Invalid VPN token.";
    }
  } catch (error) {
    loginScreenMessage.textContent = reportError("signIn", error);
  } finally {
    loginScreenBtn.disabled = false;
    loginTokenInput.disabled = false;
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
  updateServerBusyIndicator(true, "Provisioning...");
  updateServerControlStates();
  try {
    setUiMessage("Provisioning and connecting...");
    const result = await pangeaApi.provisionAndConnect(serverId);
    if (result.ok) {
      setUiMessage("Connected.");
    } else {
      setUiMessage("Connection failed.");
    }
    await refreshStatus();
    await refreshConfig();
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
          showToast("You have been signed out. Please log in again.");
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
    updateServerBusyIndicator(false);
    updateServerControlStates();
  }
});

serverDisconnectBtn.addEventListener("click", async () => {
  if (!daemonApi) return;
  serverWorking = true;
  updateServerBusyIndicator(true, "Disconnecting...");
  updateServerControlStates();
  try {
    setUiMessage("Disconnecting...");
    const result = await daemonApi.disconnect();
    setUiMessage(result.ok ? "Disconnected." : "Disconnect failed.");
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

directIpToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  await pangeaApi.setDirectIp(directIpToggle.checked);
  setUiMessage(directIpToggle.checked ? "Direct IP enabled." : "Direct IP disabled.");
});

directIpOnlyToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  await pangeaApi.setDirectIpOnly(directIpOnlyToggle.checked);
  if (directIpOnlyToggle.checked) {
    directIpToggle.checked = true;
    directIpToggle.disabled = true;
  } else {
    directIpToggle.disabled = false;
  }
  setUiMessage(directIpOnlyToggle.checked ? "Direct IP only mode enabled." : "Direct IP only mode disabled.");
});



const loadingScreen = must<HTMLElement>("loadingScreen");
const loadingMessage = must<HTMLParagraphElement>("loadingMessage");
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
    loadingMessage.textContent = "Bridge unavailable.";
    return;
  }

  // Poll until daemon responds (max 30s)
  const maxAttempts = 60;
  for (let i = 0; i < maxAttempts; i++) {
    const remaining = Math.ceil((maxAttempts - i) * 0.5);
    loadingMessage.textContent = `Waiting for daemon... (${remaining}s)`;
    try {
      const status = await daemonApi.getStatus();
      if (status) break;
    } catch {
      // not ready
    }
    if (i === maxAttempts - 1) {
      loadingMessage.textContent = "Daemon did not respond. Try restarting the app.";
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
    showToast("You have been signed out. Please log in again.");
  });

  initCollapsibleSections();
  setManualBuilderVisibility(false, false);
  await renderAppVersion();
  updateBusyIndicator();
  await refreshAll(true);

  if (pangeaApi) {
    try {
      directIpToggle.checked = await pangeaApi.getDirectIp();
      directIpOnlyToggle.checked = await pangeaApi.getDirectIpOnly();
      if (directIpOnlyToggle.checked) {
        directIpToggle.checked = true;
        directIpToggle.disabled = true;
      }
    } catch {
      // default off
    }

    await loadCachedServers();
    if (authState.authenticated) {
      await refreshServers();
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
      schedulePoll();
    }, pollInterval);
  }
  schedulePoll();
}

const updateOverlay = must<HTMLElement>("updateOverlay");
const updateCloseBtn = must<HTMLButtonElement>("updateCloseBtn");
const updateCurrentVersionEl = must<HTMLSpanElement>("updateCurrentVersion");
const updateLatestVersionEl = must<HTMLSpanElement>("updateLatestVersion");
const updateDownloadBtn = must<HTMLButtonElement>("updateDownloadBtn");
const updateProgressWrap = must<HTMLElement>("updateProgressWrap");
const updateProgressFill = must<HTMLElement>("updateProgressFill");
const updateProgressText = must<HTMLSpanElement>("updateProgressText");
const updateMessageEl = must<HTMLParagraphElement>("updateMessage");
const menuBadge = must<HTMLSpanElement>("menuBadge");
const menuUpdateBtn = must<HTMLButtonElement>("menuUpdateBtn");

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
  updateProgressWrap.hidden = true;
  if (pendingUpdate.macOnly) {
    updateDownloadBtn.textContent = "View Download";
    updateMessageEl.textContent = "";
  } else if (updateDownloaded) {
    updateDownloadBtn.textContent = "Restart to Update";
    updateMessageEl.textContent = "Update downloaded and ready to install.";
  } else {
    updateDownloadBtn.textContent = "Download Update";
    updateMessageEl.textContent = "";
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

  updater.onDownloadProgress((percent) => {
    updateProgressFill.style.width = `${percent}%`;
    updateProgressText.textContent = `${Math.round(percent)}%`;
  });

  updater.onUpdateDownloaded(() => {
    updateDownloaded = true;
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "Restart to Update";
    updateProgressFill.style.width = "100%";
    updateProgressText.textContent = "100%";
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

updateDownloadBtn.addEventListener("click", async () => {
  if (!pendingUpdate) return;

  if (!updater || !pendingUpdate) return;

  // macOS: open release page in browser
  if (pendingUpdate.macOnly) {
    await updater.downloadUpdate();
    return;
  }

  // If update already downloaded, restart to install
  if (updateDownloaded) {
    updater.installUpdate();
    return;
  }

  updateDownloadBtn.disabled = true;
  updateDownloadBtn.textContent = "Downloading...";
  updateProgressFill.style.width = "0%";
  updateProgressText.textContent = "0%";
  updateProgressWrap.hidden = false;
  updateMessageEl.textContent = "";

  try {
    await updater.downloadUpdate();
  } catch (error) {
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = "Retry Download";
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
    await Promise.all([refreshConfig(), refreshStatus(), refreshLogs()]);
    setUiMessage("Ready.");
  } catch (error) {
    console.error("[daemonSync]", error);
    setUiMessage("Something went wrong. Retrying...");
    // Retry once after a short delay
    try {
      await new Promise((r) => setTimeout(r, 2000));
      await Promise.all([refreshConfig(), refreshStatus(), refreshLogs()]);
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
    selectedProfileId: profileSelect.value || null,
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

async function refreshConfig(preferredProfileId?: string): Promise<void> {
  if (!daemonApi) {
    return;
  }

  const config = await daemonApi.getConfig();
  profiles = config.profiles;
  renderProfiles(preferredProfileId);
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

function renderProfiles(preferredProfileId?: string): void {
  const previousValue = profileSelect.value;
  profileSelect.innerHTML = "";

  if (profiles.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No profiles in daemon config";
    profileSelect.append(option);
    profileSelect.disabled = true;
    connectBtn.disabled = true;
    deleteManualProfileBtn.disabled = true;
    clearManualProfileForm();
    updateControlStates();
    return;
  }

  for (const profile of profiles) {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = `${profile.name} (${profile.id})`;
    profileSelect.append(option);
  }

  profileSelect.disabled = false;
  connectBtn.disabled = false;
  deleteManualProfileBtn.disabled = false;

  const savedProfile = localStorage.getItem(LAST_PROFILE_KEY);
  const selectedId = preferredProfileId ?? previousValue ?? savedProfile ?? "";
  const hasSelection = profiles.some((profile) => profile.id === selectedId);
  profileSelect.value = hasSelection ? selectedId : profiles[0].id;

  const selectedProfile = profiles.find((profile) => profile.id === profileSelect.value);
  if (selectedProfile) {
    fillManualProfileForm(selectedProfile);
  }

  updateControlStates();
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

  heroMeta.hidden = !connected;

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

function fillManualProfileForm(profile: Profile): void {
  manualName.value = profile.name;
  manualId.value = profile.id;
  manualTunnelName.value = profile.wireguard.tunnelName;
  manualRemoteHost.value = profile.cloak.remoteHost;
  manualUid.value = profile.cloak.uid;
  manualPublicKey.value = profile.cloak.publicKey;
  manualWgConfigText.value = profile.wireguard.configText;
  encodedExportOutput.value = "";
}

function clearManualProfileForm(): void {
  manualBuilderForm.reset();
  encodedExportOutput.value = "";
}

function profileFromManualForm(): Profile {
  const name = required(manualName.value, "Profile name");
  const id = slugify(manualId.value.trim() || name);
  const tunnelName = slugify(manualTunnelName.value.trim() || id);

  return {
    id,
    name,
    cloak: {
      localPort: 51820,
      remoteHost: required(manualRemoteHost.value, "Cloak remote host"),
      remotePort: 443,
      uid: required(manualUid.value, "Cloak UID"),
      publicKey: required(manualPublicKey.value, "Cloak public key"),
      encryptionMethod: "plain",
      password: ""
    },
    wireguard: {
      configText: requiredRawText(manualWgConfigText.value, "WireGuard config text"),
      tunnelName,
      dns: []
    }
  };
}

function profileFromImportedPayload(payload: ImportedProfilePayload): Profile {
  const name = required(payload.profilename, "Profile name");
  const id = generateProfileId(name);

  return {
    id,
    name,
    cloak: {
      localPort: 51820,
      remoteHost: required(payload.remotehost, "Cloak remote host"),
      remotePort: 443,
      uid: required(payload.cloakuid, "Cloak UID"),
      publicKey: required(payload.cloakpubkey, "Cloak public key"),
      encryptionMethod: "plain",
      password: ""
    },
    wireguard: {
      configText: requiredRawText(payload.wgconfig, "WireGuard config text"),
      tunnelName: "pangeavpn",
      dns: []
    }
  };
}

function importedPayloadFromProfile(profile: Profile): ImportedProfilePayload {
  return {
    profilename: profile.name,
    remotehost: profile.cloak.remoteHost,
    cloakuid: profile.cloak.uid,
    cloakpubkey: profile.cloak.publicKey,
    wgconfig: profile.wireguard.configText
  };
}

function upsertProfile(list: Profile[], profile: Profile): Profile[] {
  const index = list.findIndex((item) => item.id === profile.id);
  if (index === -1) {
    return [...list, profile];
  }

  const next = [...list];
  next[index] = profile;
  return next;
}

function parseImportedProfilePayload(jsonText: string): ImportedProfilePayload {
  let payload: unknown;
  try {
    payload = JSON.parse(jsonText);
  } catch {
    throw new Error("Decoded payload is not valid JSON.");
  }

  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) {
    throw new Error("Decoded payload must be a JSON object.");
  }

  const record = payload as Record<string, unknown>;
  return {
    profilename: readPayloadField(record, "profilename"),
    remotehost: readPayloadField(record, "remotehost"),
    cloakuid: readPayloadField(record, "cloakuid"),
    cloakpubkey: readPayloadField(record, "cloakpubkey"),
    wgconfig: readPayloadField(record, "wgconfig")
  };
}

function readPayloadField(record: Record<string, unknown>, key: string): string {
  const value = record[key];
  if (typeof value !== "string") {
    throw new Error(`Import field "${key}" must be a string.`);
  }
  return value;
}

function decodeBase64ToUtf8(input: string): string {
  const normalized = normalizeBase64(input);

  let binary: string;
  try {
    binary = window.atob(normalized);
  } catch {
    throw new Error("Input is not valid base64.");
  }

  const bytes = Uint8Array.from(binary, (char) => char.charCodeAt(0));
  return new TextDecoder().decode(bytes);
}

function encodeUtf8ToBase64(input: string): string {
  const bytes = new TextEncoder().encode(input);
  let binary = "";
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return window.btoa(binary);
}

function normalizeBase64(input: string): string {
  const compact = input.trim().replace(/\s+/g, "").replace(/-/g, "+").replace(/_/g, "/");
  const paddingLength = (4 - (compact.length % 4)) % 4;
  return compact + "=".repeat(paddingLength);
}

function generateProfileId(profileName: string): string {
  const base = slugify(profileName);
  let candidate = base;
  let suffix = 1;

  while (profiles.some((profile) => profile.id === candidate)) {
    candidate = `${base}-${suffix}`;
    suffix += 1;
  }

  return candidate;
}

function required(value: string, label: string): string {
  const trimmed = value.trim();
  if (!trimmed) {
    throw new Error(`${label} is required.`);
  }
  return trimmed;
}

function requiredRawText(value: string, label: string): string {
  if (!value.trim()) {
    throw new Error(`${label} is required.`);
  }
  return value;
}

function slugify(input: string): string {
  const slug = input
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return slug || `profile-${Date.now()}`;
}

function setUiMessage(message: string): void {
  uiMessageEl.textContent = message;
}

function showToast(message: string, durationMs = 5000): void {
  const container = document.getElementById("toastContainer");
  if (!container) return;

  const toast = document.createElement("div");
  toast.className = "toast";
  toast.textContent = message;
  container.appendChild(toast);

  setTimeout(() => {
    toast.classList.add("toast-out");
    toast.addEventListener("animationend", () => toast.remove(), { once: true });
  }, durationMs);
}

function setManualBuilderVisibility(visible: boolean, scrollToPanel = true): void {
  manualBuilderSection.hidden = !visible;
  toggleManualBuilderBtn.textContent = visible ? "Hide Manual Profile Editor" : "Show Manual Profile Editor";
  toggleManualBuilderBtn.setAttribute("aria-pressed", String(visible));

  if (visible && scrollToPanel) {
    manualBuilderSection.scrollIntoView({ behavior: "smooth", block: "nearest" });
    manualName.focus();
  }
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

function updateBusyIndicator(): void {
  const daemonBusy = currentDaemonState === "CONNECTING" || currentDaemonState === "DISCONNECTING";
  const active = uiRefreshing || uiWorking || daemonBusy;

  refreshIndicator.classList.toggle("active", active);
  refreshIndicator.setAttribute("aria-hidden", String(!active));

  if (!active) {
    refreshIndicatorLabel.textContent = "Working...";
  } else if (currentDaemonState === "CONNECTING") {
    refreshIndicatorLabel.textContent = "Connecting...";
  } else if (currentDaemonState === "DISCONNECTING") {
    refreshIndicatorLabel.textContent = "Disconnecting...";
  } else if (uiRefreshing) {
    refreshIndicatorLabel.textContent = "Refreshing...";
  } else {
    refreshIndicatorLabel.textContent = "Working...";
  }

  updateControlStates();
}

function updateAuthUI(): void {
  if (authState.authenticated) {
    showAppShell();
    loginBtn.hidden = true;
    if (authState.user) {
      authBar.hidden = false;
      authUserLabel.textContent = authState.user.email || authState.user.name;
    } else {
      authBar.hidden = true;
    }
    serverPanel.hidden = false;
  } else {
    showLoginScreen();
    loginBtn.hidden = !pangeaApi;
    authBar.hidden = true;
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

async function refreshServers(): Promise<void> {
  if (!pangeaApi) return;
  try {
    servers = await pangeaApi.getServers();
    renderServers();
    pangeaApi.cacheServers(servers).catch(() => {});
  } catch (error) {
    setUiMessage(reportError("loadServers", error));
  }
}

function renderServers(): void {
  const previousValue = serverSelect.value;
  serverSelect.innerHTML = "";
  serverPickerDropdown.innerHTML = "";

  if (servers.length === 0) {
    const option = document.createElement("option");
    option.value = "";
    option.textContent = "No servers available";
    serverSelect.append(option);
    serverSelect.disabled = true;
    serverPickerBtn.disabled = true;
    serverPickerLabel.textContent = "No servers available";
    serverConnectBtn.disabled = true;
    return;
  }

  for (const server of servers) {
    const option = document.createElement("option");
    option.value = server.id;
    option.textContent = `${server.name} (${server.id})`;
    serverSelect.append(option);

    const item = document.createElement("div");
    item.className = "server-picker-option";
    item.dataset.value = server.id;
    const nameSpan = document.createElement("span");
    nameSpan.className = "server-picker-option-name";
    nameSpan.textContent = server.name;
    const regionSpan = document.createElement("span");
    regionSpan.className = "server-picker-option-region";
    regionSpan.textContent = server.id;
    item.append(nameSpan, regionSpan);
    item.addEventListener("click", () => {
      serverSelect.value = server.id;
      syncServerPicker();
      serverPickerDropdown.hidden = true;
      serverPickerBtn.setAttribute("aria-expanded", "false");
    });
    serverPickerDropdown.append(item);
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

  for (const opt of Array.from(serverPickerDropdown.children)) {
    const el = opt as HTMLElement;
    el.classList.toggle("selected", el.dataset.value === serverSelect.value);
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
  serverDisconnectBtn.disabled =
    !latestStatus || latestStatus.state === "DISCONNECTED" || latestStatus.state === "DISCONNECTING" || busy;
}

function updateServerBusyIndicator(active: boolean, label?: string): void {
  serverIndicator.classList.toggle("active", active);
  serverIndicator.setAttribute("aria-hidden", String(!active));
  if (label) {
    serverIndicatorLabel.textContent = label;
  }
}

function updateControlStates(): void {
  const daemonBusy = currentDaemonState === "CONNECTING" || currentDaemonState === "DISCONNECTING";
  const spinnerActive = uiRefreshing || uiWorking || daemonBusy;

  if (!latestStatus) {
    connectBtn.disabled = profiles.length === 0 || spinnerActive;
    disconnectBtn.disabled = true;
    updateServerControlStates();
    return;
  }

  const fullyDisconnected =
    latestStatus.state === "DISCONNECTED" &&
    !latestStatus.cloak.running &&
    !latestStatus.wireguard.running;

  connectBtn.disabled = profiles.length === 0 || spinnerActive || !fullyDisconnected;
  disconnectBtn.disabled = latestStatus.state === "DISCONNECTED" || latestStatus.state === "DISCONNECTING";
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

function must<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) {
    throw new Error(`missing element: ${id}`);
  }
  return element as T;
}

void init();
