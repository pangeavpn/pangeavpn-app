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
import { pickRandomServer, resolveSelection } from "./serverPick.js";
import { buildDriftMap } from "./driftMap.js";
import { dnsChoiceFor, dnsServersFor, type DnsChoice } from "./dnsPresets.js";
import { buildFlag } from "./flags.js";
import { formatAccountNumberInput, normalizeAccountNumber } from "./accountNumber.js";
import {
  buildServerRetryOrder,
  groupRegions,
  orderByRecent,
  pickNode,
  promoteRecent,
  regionOfServer,
  type Region
} from "./regions.js";
import { scheduleConnectionMessages } from "./connectionProgress.js";
import {
  daemonHealthAfterFailure,
  daemonHealthAfterSuccess,
  initialDaemonHealth
} from "./daemonHealth.js";
import {
  t,
  initLocale,
  resolveLocale,
  localeTag,
  LOCALES,
  type MessageKey
} from "./i18n/index.js";

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
    return t("error.network");
  }
  return t("error.generic");
}

const stateEl = document.getElementById("state") as HTMLSpanElement;
const detailEl = document.getElementById("detail") as HTMLSpanElement;
const txBytesEl = document.getElementById("txBytes") as HTMLSpanElement;
const rxBytesEl = document.getElementById("rxBytes") as HTMLSpanElement;
const mapHost = document.getElementById("mapHost") as HTMLElement;
const heroHeadline = document.getElementById("heroHeadline") as HTMLElement;
const factSessionEl = document.getElementById("factSession") as HTMLSpanElement;
const factViaEl = document.getElementById("factVia") as HTMLSpanElement;
const regionSlots = document.getElementById("regionSlots") as HTMLElement;
const regionMoreCount = document.getElementById("regionMoreCount") as HTMLElement;
// One in the shell header, one on the sign-in screen — both stay in step.
const themeToggleBtns = Array.from(document.querySelectorAll<HTMLButtonElement>(".theme-toggle"));
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
const hubDirectIpToggle = document.getElementById("hubDirectIpToggle") as HTMLInputElement;
const hubShadowsocksToggle = document.getElementById("hubShadowsocksToggle") as HTMLInputElement;
const hubFrontedToggle = document.getElementById("hubFrontedToggle") as HTMLInputElement;
const hubNormalToggle = document.getElementById("hubNormalToggle") as HTMLInputElement;
const allowLanToggle = document.getElementById("allowLanToggle") as HTMLInputElement;
const dnsPresetSelect = document.getElementById("dnsPresetSelect") as HTMLSelectElement;
const customDnsField = document.getElementById("customDnsField") as HTMLElement;
const customDnsInput = document.getElementById("customDnsInput") as HTMLInputElement;
const wireguardMtuInput = document.getElementById("wireguardMtuInput") as HTMLInputElement;
const preferredTransportSelect = document.getElementById("preferredTransportSelect") as HTMLSelectElement;
const launchAtStartupToggle = document.getElementById("launchAtStartupToggle") as HTMLInputElement;
const autoConnectToggle = document.getElementById("autoConnectToggle") as HTMLInputElement;
const lockdownToggle = document.getElementById("lockdownToggle") as HTMLInputElement;
const loginScreen = document.getElementById("loginScreen") as HTMLElement;
const loginSettingsBtn = document.getElementById("loginSettingsBtn") as HTMLButtonElement;
const loginScreenBtn = document.getElementById("loginScreenBtn") as HTMLButtonElement;
const loginScreenMessage = document.getElementById("loginScreenMessage") as HTMLParagraphElement;
const heroCard = document.getElementById("heroCard") as HTMLElement;
const menuBtn = document.getElementById("menuBtn") as HTMLButtonElement;
const menuDropdown = document.getElementById("menuDropdown") as HTMLElement;
const manageSubLink = document.getElementById("manageSubLink") as HTMLAnchorElement;
const menuSettingsBtn = document.getElementById("menuSettingsBtn") as HTMLButtonElement;
const settingsOverlay = document.getElementById("settingsOverlay") as HTMLElement;
const settingsOverlayCloseBtn = document.getElementById("settingsOverlayCloseBtn") as HTMLButtonElement;
const settingsPane = document.getElementById("settingsPane") as HTMLElement;
const settingsNav = document.getElementById("settingsNav") as HTMLElement;
const settingsAccountActions = document.getElementById("settingsAccountActions") as HTMLElement;
const accountSubscription = document.getElementById("accountSubscription") as HTMLSpanElement;
const setProvisioningValue = document.getElementById("setProvisioningValue") as HTMLSpanElement;
const setTransportValue = document.getElementById("setTransportValue") as HTMLSpanElement;
const setNetworkValue = document.getElementById("setNetworkValue") as HTMLSpanElement;
const setStartupValue = document.getElementById("setStartupValue") as HTMLSpanElement;
const setLanguageValue = document.getElementById("setLanguageValue") as HTMLSpanElement;
const checkUpdatesBtn = document.getElementById("checkUpdatesBtn") as HTMLButtonElement;
const settingsVersionEl = document.getElementById("settingsVersion") as HTMLSpanElement;
const serverPickerBtn = document.getElementById("serverPickerBtn") as HTMLButtonElement;
const serverPickerOverlay = document.getElementById("serverPickerOverlay") as HTMLElement;
const serverPickerOverlayList = document.getElementById("serverPickerOverlayList") as HTMLElement;
const serverPickerOverlayCloseBtn = document.getElementById("serverPickerOverlayCloseBtn") as HTMLButtonElement;

type ThemeMode = "light" | "dark";
const THEME_STORAGE_KEY = "pangea-vpn-theme";
const COLLAPSE_STATE_KEY = "pangea-vpn-collapse-state";

let currentDaemonState: StatusResponse["state"] = "DISCONNECTED";
let latestStatus: StatusResponse | null = null;
let uiRefreshing = false;
let uiWorking = false;
let lastServerIdLocal: string | null = null;
let autoConnectLocal = false;
let lockdownLocal = false;
let logsCursor = 0;
let logEntries: LogEntry[] = [];
let authState: AuthState = { authenticated: false, user: null };
let servers: ServerInfo[] = [];
let serverWorking = false;
// Stop stays live for the whole attempt: cancelling is a hard cancel in main,
// so there is no unsafe window a grace delay would protect.
let connectInFlight = false;
// Hub's verdict on whether this account may connect; null until asked. Never
// derived from subscription.status — prepaid plans stay "active" once lapsed.
let entitled: boolean | null = null;

// Mirrors shared/mtu.ts — the renderer can't import it without clobbering
// main's CommonJS copy in dist. Display only; keep in step with the input.
const MTU_MIN = 1280;
const MTU_MAX = 1420;
const MTU_DEFAULT = 1380;

for (const btn of themeToggleBtns) {
  btn.addEventListener("click", () => {
    const nextTheme: ThemeMode = document.body.dataset.theme === "dark" ? "light" : "dark";
    applyTheme(nextTheme);
  });
}

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

/** Collapsed rows carry their current value, so state is readable without
 *  opening anything. Reuses the control labels rather than new strings. */
function updateSettingsSummaries(): void {
  const off = t("settings.summary.off");

  const provisioning: string[] = [];
  if (hubDirectIpToggle.checked) provisioning.push(t("settings.provisioning.directIp.title"));
  if (hubShadowsocksToggle.checked) provisioning.push(t("settings.provisioning.hubShadowsocks.title"));
  if (hubFrontedToggle.checked) provisioning.push(t("settings.provisioning.hubFronted.title"));
  if (hubNormalToggle.checked) provisioning.push(t("settings.provisioning.hubNormal.title"));
  const provisioningSummary = provisioning.length ? provisioning.join(" · ") : off;
  setProvisioningValue.textContent = provisioningSummary;
  provisioningPickerValue.textContent = provisioningSummary;

  const transport = preferredTransportSelect.selectedOptions[0];
  setTransportValue.textContent = transport ? transport.textContent : "";
  syncTransportChoice();

  const network = [`MTU ${wireguardMtuInput.value || MTU_DEFAULT}`];
  const dnsChoice = dnsPresetSelect.selectedOptions[0];
  if (dnsPresetSelect.value !== "automatic" && dnsChoice) network.push(dnsChoice.textContent ?? "DNS");
  if (allowLanToggle.checked) network.push(t("settings.network.allowLan.title"));
  setNetworkValue.textContent = network.join(" · ");

  const startup: string[] = [];
  if (launchAtStartupToggle.checked) startup.push(t("settings.startup.launch.title"));
  if (autoConnectToggle.checked) startup.push(t("settings.startup.autoConnect.title"));
  if (lockdownToggle.checked) startup.push(t("settings.startup.lockdown.title"));
  setStartupValue.textContent = startup.length ? startup.join(" · ") : off;

  const language = languageSelect?.selectedOptions[0];
  setLanguageValue.textContent = language ? language.textContent : "";
}

function formatSubscriptionDate(iso: string | null): string {
  if (!iso) return "";
  try {
    return ` ${new Date(iso).toLocaleDateString(localeTag(), { year: "numeric", month: "short", day: "numeric" })}`;
  } catch {
    return "";
  }
}

function subscriptionText(sub: SubscriptionInfo | null): { text: string; warn: boolean } {
  if (!sub) return { text: t("sub.none"), warn: false };
  const when = formatSubscriptionDate(sub.expiresAt);
  // Before the status branch: a lapsed prepaid plan still reads "active", so
  // status-first would quote an expiry date already in the past.
  if (sub.entitled === false) {
    return { text: `${t("sub.expired")}${when ? " —" + when : ""}`, warn: true };
  }
  if (sub.status === "active" || sub.status === "trialing") {
    const trial = sub.status === "trialing" ? t("sub.trialPrefix") : "";
    return { text: `${trial}${sub.renews ? t("sub.renews") : t("sub.expires")}${when}`, warn: false };
  }
  if (sub.status === "past_due") {
    return { text: `${t("sub.pastDue")}${when ? " —" + when : ""}`, warn: true };
  }
  return { text: t("sub.none"), warn: false };
}

/** Ask the hub whether this account may connect. Toasts once per transition
 *  into expired — the only notice a lapsed prepaid customer ever gets. */
async function refreshEntitlement(): Promise<void> {
  if (!pangeaApi) return;
  let sub: SubscriptionInfo | null = null;
  try {
    sub = await pangeaApi.getSubscription();
  } catch {
    return; // offline or hub down — leave the previous verdict alone
  }
  // Absent on older hubs: assume entitled rather than locking someone out.
  const next = sub === null ? null : sub.entitled !== false;
  const wasEntitled = entitled;
  entitled = next;
  if (next === false && wasEntitled !== false) {
    showToast(t("connect.expired"), 8000);
  }
  updateServerControlStates();
}

// Fetched fresh each time Settings opens so expiry/renewal is always current.
async function refreshSubscription(): Promise<void> {
  accountSubscription.classList.remove("account-subscription-warn");
  if (!pangeaApi) {
    accountSubscription.textContent = t("common.dash");
    return;
  }
  accountSubscription.textContent = t("common.loading");
  let sub: SubscriptionInfo | null = null;
  try {
    sub = await pangeaApi.getSubscription();
  } catch {
    sub = null;
  }
  const { text, warn } = subscriptionText(sub);
  accountSubscription.textContent = text;
  accountSubscription.classList.toggle("account-subscription-warn", warn);
  // Settings just told us the truth — keep the connect gate in step with it.
  if (sub) {
    entitled = sub.entitled !== false;
    updateServerControlStates();
  }
}

// ── Overlay focus management ──────────────────────────────────

// Overlays cover what's underneath but leave it in the tab order, so the layer
// below goes `inert` on open; focus moves in, and is restored on close. Settings
// opens over the sign-in screen too, so that gets the same treatment.
const overlayReturnFocus: Array<HTMLElement | null> = [];

// A function, not a const: `shell` is declared further down the module.
function overlayUnderlays(): HTMLElement[] {
  return [shell, loginScreen];
}

function activateOverlay(overlay: HTMLElement): void {
  overlayReturnFocus.push(document.activeElement as HTMLElement | null);
  for (const el of overlayUnderlays()) el.setAttribute("inert", "");
  const focusTarget = overlay.querySelector<HTMLElement>("button:not([hidden]), [href], input, select, textarea");
  window.setTimeout(() => (focusTarget ?? overlay).focus(), 0);
}

function deactivateOverlay(): void {
  // Only re-enable the layer below once no full-screen overlay remains open.
  if (!settingsOverlay.classList.contains("visible") && !serverPickerOverlay.classList.contains("visible")) {
    for (const el of overlayUnderlays()) el.removeAttribute("inert");
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
  // Always open on the section list, never on a picker left over from last time.
  closeSubpane(false);
  settingsVersionEl.textContent = appVersionEl.textContent || t("common.dash");
  updateSettingsSummaries();
  settingsSpyLockUntil = 0;
  settingsPane.scrollTo({ top: 0, behavior: "auto" });
  sizeSettingsTail();
  syncSettingsNav();
  // Signed out, the account rows have nothing to act on — Settings is still
  // reachable from the sign-in screen for language, theme and bypass methods.
  settingsAccountActions.hidden = !authState.authenticated;
  if (authState.authenticated) void refreshSubscription();
  activateOverlay(settingsOverlay);
}

function closeSettings(): void {
  settingsOverlay.classList.remove("visible");
  settingsOverlay.setAttribute("aria-hidden", "true");
  deactivateOverlay();
}

menuSettingsBtn.addEventListener("click", () => {
  menuDropdown.classList.remove("open");
  menuBtn.setAttribute("aria-expanded", "false");
  openSettings();
});

loginSettingsBtn.addEventListener("click", openSettings);

settingsOverlayCloseBtn.addEventListener("click", closeSettings);

// ── Settings nav rail ─────────────────────────────────────────

const settingsNavItems = Array.from(settingsNav.querySelectorAll<HTMLButtonElement>(".settings-nav-item"));
const settingsSections = settingsNavItems
  .map((item) => document.getElementById(item.dataset.settingsTarget ?? ""))
  .filter((el): el is HTMLElement => el !== null);

// Distance below the pane's top edge at which a section counts as "current".
const SETTINGS_JUMP_GUTTER = 14;
// Muzzles the scroll spy while a click-driven smooth scroll is in flight, so
// the rail doesn't flicker through every section the pane passes on the way.
let settingsSpyLockUntil = 0;

function markSettingsNav(sectionId: string): void {
  for (const item of settingsNavItems) {
    const active = item.dataset.settingsTarget === sectionId;
    item.classList.toggle("is-active", active);
    if (active) item.setAttribute("aria-current", "true");
    else item.removeAttribute("aria-current");
  }
}

/** Light up whichever section currently sits at the top of the pane. */
function syncSettingsNav(): void {
  if (Date.now() < settingsSpyLockUntil || settingsSections.length === 0) return;
  const scrollTop = settingsPane.scrollTop;
  if (scrollTop + settingsPane.clientHeight >= settingsPane.scrollHeight - 4) {
    markSettingsNav(settingsSections[settingsSections.length - 1].id);
    return;
  }
  let active = settingsSections[0];
  for (const section of settingsSections) {
    if (section.offsetTop - scrollTop <= SETTINGS_JUMP_GUTTER * 2) active = section;
  }
  markSettingsNav(active.id);
}

/** Tail room so the last (short) section can still scroll to the top of the
 *  pane — otherwise its rail row could only ever light up at full scroll. */
function sizeSettingsTail(): void {
  const last = settingsSections[settingsSections.length - 1];
  if (!last) return;
  const room = settingsPane.clientHeight - last.offsetHeight - SETTINGS_JUMP_GUTTER * 2;
  settingsPane.style.paddingBottom = `${Math.max(24, room)}px`;
}

for (const item of settingsNavItems) {
  item.addEventListener("click", () => {
    const target = document.getElementById(item.dataset.settingsTarget ?? "");
    if (!target) return;
    // The rail is also the way back out of a picker that took over the pane.
    closeSubpane(false);
    markSettingsNav(target.id);
    settingsSpyLockUntil = Date.now() + 700;
    // offsetTop is measured from the pane's padding edge, so subtracting the
    // gutter leaves the section sitting where the pane's own padding puts it.
    settingsPane.scrollTop = Math.max(0, target.offsetTop - SETTINGS_JUMP_GUTTER);
  });
}

settingsPane.addEventListener("scroll", syncSettingsNav, { passive: true });

// ── Connection method picker (full right pane) ────────────────

const transportPane = document.getElementById("transportPane") as HTMLElement;
const transportOptions = document.getElementById("transportOptions") as HTMLElement;
const transportPickerBtn = document.getElementById("transportPickerBtn") as HTMLButtonElement;
const transportPickerValue = document.getElementById("transportPickerValue") as HTMLElement;
const transportBackBtn = document.getElementById("transportBackBtn") as HTMLButtonElement;

// One line of explanation per method. Anything absent here just renders its
// label, so a newly ungated option can ship before its copy does.
const TRANSPORT_DESCRIPTIONS: Record<string, MessageKey> = {
  auto: "settings.transport.auto.desc",
  cloak: "settings.transport.cloak.desc",
  naive: "settings.transport.naive.desc",
  reality: "settings.transport.reality.desc",
  hysteria2: "settings.transport.hysteria2.desc",
  shadowsocks: "settings.transport.shadowsocks.desc",
  wireguard: "settings.transport.wireguard.desc"
};

const CHECK_SVG =
  '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12.5 4.5 4.5L19 7"/></svg>';

/** Build one row per option in the (hidden) select that holds the real state. */
function renderTransportOptions(): void {
  transportOptions.replaceChildren();
  for (const option of Array.from(preferredTransportSelect.options)) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "option-row";
    row.setAttribute("role", "radio");
    row.dataset.value = option.value;

    const copy = document.createElement("span");
    copy.className = "option-copy";

    const title = document.createElement("span");
    title.className = "option-title";
    title.textContent = option.textContent;
    copy.appendChild(title);

    const descKey = TRANSPORT_DESCRIPTIONS[option.value];
    if (descKey) {
      const desc = document.createElement("span");
      desc.className = "option-desc";
      desc.textContent = t(descKey);
      copy.appendChild(desc);
    }

    const check = document.createElement("span");
    check.className = "option-check";
    check.innerHTML = CHECK_SVG;

    row.append(copy, check);
    row.addEventListener("click", () => selectTransport(option.value));
    transportOptions.appendChild(row);
  }
  syncTransportChoice();
}

/** Mirror the select's current value onto the rows and the section button. */
function syncTransportChoice(): void {
  const current = preferredTransportSelect.value;
  transportPickerValue.textContent = preferredTransportSelect.selectedOptions[0]?.textContent ?? "";
  for (const row of Array.from(transportOptions.children)) {
    row.setAttribute("aria-checked", String((row as HTMLElement).dataset.value === current));
  }
}

/** Route the choice back through the select so every existing handler runs. */
function selectTransport(value: string): void {
  if (preferredTransportSelect.value === value) return;
  preferredTransportSelect.value = value;
  syncTransportChoice();
  preferredTransportSelect.dispatchEvent(new Event("change", { bubbles: true }));
}

// ── Sub-panes ─────────────────────────────────────────────────

// A section whose options are too wordy for the section list hands them a whole
// pane instead. Each entry owns the pane, the row that opens it, and the rail
// item that stays lit while it's up.
interface Subpane {
  pane: HTMLElement;
  trigger: HTMLButtonElement;
  back: HTMLButtonElement;
  section: string;
  render?: () => void;
  initialFocus: () => HTMLElement | null;
}

const provisioningPane = document.getElementById("provisioningPane") as HTMLElement;
const provisioningPickerBtn = document.getElementById("provisioningPickerBtn") as HTMLButtonElement;
const provisioningPickerValue = document.getElementById("provisioningPickerValue") as HTMLElement;
const provisioningBackBtn = document.getElementById("provisioningBackBtn") as HTMLButtonElement;

const subpanes: Subpane[] = [
  {
    pane: transportPane,
    trigger: transportPickerBtn,
    back: transportBackBtn,
    section: "secTransport",
    render: renderTransportOptions,
    initialFocus: () => transportOptions.querySelector<HTMLElement>('[aria-checked="true"]')
  },
  {
    pane: provisioningPane,
    trigger: provisioningPickerBtn,
    back: provisioningBackBtn,
    section: "secProvisioning",
    initialFocus: () => provisioningPane.querySelector<HTMLElement>(".toggle-switch")
  }
];

let activeSubpane: Subpane | null = null;

function isSubpaneOpen(): boolean {
  return activeSubpane !== null;
}

function openSubpane(entry: Subpane): void {
  entry.render?.();
  settingsPane.hidden = true;
  for (const other of subpanes) other.pane.hidden = other !== entry;
  entry.trigger.setAttribute("aria-expanded", "true");
  activeSubpane = entry;
  markSettingsNav(entry.section);
  (entry.initialFocus() ?? entry.back).focus();
}

function closeSubpane(restoreFocus: boolean): void {
  const entry = activeSubpane;
  if (!entry) return;
  entry.pane.hidden = true;
  entry.trigger.setAttribute("aria-expanded", "false");
  activeSubpane = null;
  settingsPane.hidden = false;
  syncSettingsNav();
  if (restoreFocus) entry.trigger.focus();
}

for (const entry of subpanes) {
  entry.trigger.addEventListener("click", () => openSubpane(entry));
  entry.back.addEventListener("click", () => closeSubpane(true));
}

document.addEventListener("keydown", (e) => {
  // Defer to a stacked modal (Devices / Update) so Escape backs out one layer.
  if (e.key === "Escape" && settingsOverlay.classList.contains("visible") && !isSubModalOpen()) {
    e.preventDefault();
    e.stopPropagation();
    // A picker filling the pane is a layer of its own: back out to the sections
    // first, and only close Settings on a second press.
    if (isSubpaneOpen()) closeSubpane(true);
    else closeSettings();
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
  if (document.visibilityState !== "visible") return;
  // Hidden drops to the idle cadence, so sample once straight away rather than
  // showing whatever was true up to two seconds before the window reappeared.
  pollNow();
  void refreshServersWithRetry();
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
    setUiMessage(t("logs.noneToCopy"));
    return;
  }

  try {
    await copyTextToClipboard(text);
    setUiMessage(t("logs.copied"));
  } catch (error) {
    setUiMessage(reportError("copyLogs", error));
  }
});

copyDiagnosticsBtn.addEventListener("click", async () => {
  if (!daemonApi) {
    setUiMessage(t("logs.bridgeUnavailable"));
    return;
  }

  uiWorking = true;
  updateBusyIndicator();
  try {
    const report = await buildDiagnosticsReport();
    await copyTextToClipboard(report);
    setUiMessage(t("logs.diagnosticsCopied"));
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
  setUiMessage(t("logs.cleared"));
});

loginBtn.addEventListener("click", () => {
  // Show the login screen so the user can enter their VPN token
  authState = { authenticated: false, user: null };
  updateAuthUI();
});

logoutBtn.addEventListener("click", async () => {
  if (!pangeaApi) return;
  logoutBtn.disabled = true;
  setUiMessage(t("auth.signingOut"));
  try {
    await pangeaApi.logout();
    localStorage.removeItem(MY_DEVICE_NAME_KEY);
    authState = { authenticated: false, user: null };
    servers = [];
    updateAuthUI();
    renderServers();
    await refreshStatus();
    setUiMessage(t("auth.signedOut"));
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
    cachedTokenBtn.setAttribute("aria-label", t("login.cachedTokenAriaLabel"));
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
  const formatted = formatAccountNumberInput(loginTokenInput.value);
  if (formatted !== loginTokenInput.value) {
    const caretAtEnd = loginTokenInput.selectionStart === loginTokenInput.value.length;
    const caret = (loginTokenInput.selectionStart ?? 0) + (formatted.length - loginTokenInput.value.length);
    loginTokenInput.value = formatted;
    const position = caretAtEnd ? formatted.length : Math.max(0, caret);
    loginTokenInput.setSelectionRange(position, position);
  }

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
  window.openExternal?.("https://pangeavpn.org/app");
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
    return new Date(iso).toLocaleDateString(localeTag(), { year: "numeric", month: "short", day: "numeric" });
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
  deviceLimitTitle.textContent = t("deviceLimit.title");
  deviceLimitSubtitle.textContent = t("deviceLimit.subtitle");
  deviceLimitContinueBtn.hidden = true;
  deviceLimitLogoutBtn.textContent = t("deviceLimit.cancel");
  deviceLimitMessage.textContent = "";
  loginScreen.hidden = true;
  deviceLimitScreen.hidden = false;

  deviceList.innerHTML = '<div class="device-list-loading"><span class="spinner"></span></div>';
  try {
    const devices = await pangeaApi.listDevices();
    renderDeviceList(devices);
  } catch (err) {
    deviceList.innerHTML = "";
    deviceLimitMessage.textContent = reportError("listDevices", err, t("deviceLimit.loadFailed"));
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
    devicesModalMessage.textContent = reportError("listDevices", err, t("deviceLimit.loadFailed"));
  }
}

function renderDevicesModalList(devices: DeviceInfo[]): void {
  devicesModalList.innerHTML = "";
  if (devices.length === 0) {
    devicesModalMessage.textContent = t("devices.none");
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
      badge.textContent = t("devices.thisDevice");
      nameSpan.appendChild(badge);
    }
    const dateSpan = document.createElement("span");
    dateSpan.className = "device-date";
    dateSpan.textContent = t("devices.added", { date: dateStr });
    info.appendChild(nameSpan);
    info.appendChild(dateSpan);
    const removeBtn = document.createElement("button");
    removeBtn.className = "device-remove-btn";
    if (isMine) {
      removeBtn.textContent = t("devices.current");
      removeBtn.disabled = true;
      removeBtn.title = t("devices.currentTitle");
    } else {
      removeBtn.textContent = t("devices.remove");
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
  btn.textContent = t("devices.removing");
  devicesModalMessage.textContent = "";
  try {
    await pangeaApi.removeDevice(deviceId);
    itemEl.remove();
    showToast(t("devices.removed"), 4000, true);
    if (devicesModalList.children.length === 0) {
      devicesModalMessage.textContent = t("devices.noneRemaining");
    }
  } catch (err) {
    btn.disabled = false;
    btn.textContent = t("devices.remove");
    devicesModalMessage.textContent = reportError("removeDevice", err, t("devices.removeFailed"));
  }
}

function renderDeviceList(devices: DeviceInfo[]): void {
  deviceList.innerHTML = "";
  if (devices.length === 0) {
    deviceLimitMessage.textContent = t("deviceLimit.noneCanContinue");
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
    dateSpan.textContent = t("devices.added", { date: dateStr });
    info.appendChild(nameSpan);
    info.appendChild(dateSpan);

    const removeBtn = document.createElement("button");
    removeBtn.className = "device-remove-btn";
    removeBtn.textContent = t("devices.remove");
    removeBtn.addEventListener("click", () => void handleDeviceRemove(device.id, item, removeBtn));

    item.appendChild(info);
    item.appendChild(removeBtn);
    deviceList.appendChild(item);
  }
}

async function handleDeviceRemove(deviceId: string, itemEl: HTMLElement, btn: HTMLButtonElement): Promise<void> {
  if (!pangeaApi) return;
  btn.disabled = true;
  btn.textContent = t("devices.removing");
  deviceLimitMessage.textContent = "";
  try {
    await pangeaApi.removeDevice(deviceId);
    itemEl.remove();
    deviceRemovedCount++;
    deviceLimitContinueBtn.hidden = false;
    showToast(t("devices.removed"), 4000, true);
  } catch (err) {
    btn.disabled = false;
    btn.textContent = t("devices.remove");
    deviceLimitMessage.textContent = reportError("removeDevice", err, t("devices.removeFailed"));
  }
}

deviceLimitContinueBtn.addEventListener("click", async () => {
  if (!pangeaApi || !pendingLoginToken) return;
  deviceLimitContinueBtn.disabled = true;
  deviceLimitMessage.textContent = t("login.signingIn");
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
        showToast(t("auth.deviceNamed", { name: authState.friendlyName }), 6000, true);
      }
      await refreshServers();
    } else if (authState.error === "DEVICE_LIMIT_REACHED") {
      deviceLimitMessage.textContent = t("deviceLimit.stillAtLimit");
      // Reload the updated device list
      try {
        const devices = await pangeaApi.listDevices();
        renderDeviceList(devices);
      } catch {
        // best-effort
      }
    } else {
      deviceLimitMessage.textContent = authState.error || t("login.signInFailed");
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
  const token = normalizeAccountNumber(loginTokenInput.value);
  if (!token) {
    loginScreenMessage.textContent = t("login.enterToken");
    return;
  }
  loginScreenBtn.disabled = true;
  loginTokenInput.disabled = true;
  loginScreenMessage.textContent = t("login.signingIn");
  try {
    authState = await pangeaApi.login(token);
    if (authState.authenticated) {
      localStorage.setItem("pangea:lastToken", token);
      loginScreenMessage.textContent = "";
      loginTokenInput.value = "";
      updateAuthUI();
      if (authState.friendlyName) {
        localStorage.setItem(MY_DEVICE_NAME_KEY, authState.friendlyName);
        showToast(t("auth.deviceNamed", { name: authState.friendlyName }), 6000, true);
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
      loginScreenMessage.textContent = t("login.invalidToken");
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
  let serverId = serverSelect.value;
  if (!serverId) {
    // Nothing picked — choose for them, and show which one before connecting.
    const picked = pickRandomServer(getVisibleServers());
    if (!picked) {
      setUiMessage(t("connect.noServer"));
      return;
    }
    serverId = picked.id;
    serverSelect.value = serverId;
    syncServerPicker();
  }

  // Refuse locally when the hub says this account is out of time — register
  // would 403 anyway, and this gives a clear message, not a failed connect.
  if (entitled === false) {
    showToast(t("connect.expired"));
    setUiMessage(t("sub.expired"));
    return;
  }

  serverWorking = true;
  connectInFlight = true;
  const connectingSince = showConnectingState();
  updateServerControlStates();
  setUiMessage(t("connect.provisioning"));
  const clearProgressMessages = startConnectionProgressMessages();
  try {
    const result = await pangeaApi.provisionAndConnect(serverRetryPlan(serverId));
    clearProgressMessages();
    if (result.ok) {
      applyConnectedServer(result.serverId);
      setUiMessage(t("connect.connected"));
      notifyUserConnected();
      void refreshLastServer();
    } else if ((result as { error?: string }).error === "cancelled") {
      // The user stopped it — not a failure, so no error styling.
      setUiMessage(t("connect.cancelled"));
    } else {
      setUiMessage(t("connect.failed"));
      // The most likely reason a connect is refused outright: re-check so the
      // UI settles into the expired state rather than inviting another try.
      void refreshEntitlement();
    }
    await settleConnectingState(connectingSince);
    await refreshStatus();
  } catch (error) {
    clearProgressMessages();
    // If auth was invalidated, the onAuthInvalidated listener handles the UI
    if (pangeaApi) {
      try {
        const state = await pangeaApi.getAuthState();
        if (!state.authenticated) {
          authState = { authenticated: false, user: null };
          servers = [];
          updateAuthUI();
          renderServers();
          showToast(t("auth.signedOutRetry"));
          return;
        }
      } catch {
        // ignore
      }
    }
    setUiMessage(reportError("serverConnect", error));
    await settleConnectingState(connectingSince);
    await refreshStatus();
  } finally {
    clearProgressMessages();
    connectingVisual = false;
    serverWorking = false;
    connectInFlight = false;
    updateServerControlStates();
  }
});

async function switchToServer(serverId: string): Promise<void> {
  if (!pangeaApi || !daemonApi) return;
  if (!serverId) return;

  serverWorking = true;
  connectInFlight = true;
  const connectingSince = showConnectingState();
  updateServerControlStates();
  setUiMessage(t("connect.switching"));
  const clearProgressMessages = startConnectionProgressMessages();
  try {
    const result = await pangeaApi.provisionAndSwitch(serverRetryPlan(serverId));
    clearProgressMessages();
    if (result.ok) {
      applyConnectedServer(result.serverId);
      setUiMessage(t("connect.connected"));
      notifyUserConnected();
      void refreshLastServer();
    } else if (result.error === "cancelled") {
      setUiMessage(t("connect.cancelled"));
    } else {
      setUiMessage(t("connect.switchFailed"));
    }
    await settleConnectingState(connectingSince);
    await refreshStatus();
  } catch (error) {
    clearProgressMessages();
    if (pangeaApi) {
      try {
        const state = await pangeaApi.getAuthState();
        if (!state.authenticated) {
          authState = { authenticated: false, user: null };
          servers = [];
          updateAuthUI();
          renderServers();
          showToast(t("auth.signedOutRetry"));
          return;
        }
      } catch {
        // ignore
      }
    }
    setUiMessage(reportError("serverSwitch", error));
    await settleConnectingState(connectingSince);
    await refreshStatus();
  } finally {
    clearProgressMessages();
    connectingVisual = false;
    serverWorking = false;
    connectInFlight = false;
    updateServerControlStates();
  }
}

// Deliberately not `async`: the teardown runs detached so the screen goes idle
// on the click, not on the daemon's reply. A blocked hub used to strand it here.
serverDisconnectBtn.addEventListener("click", () => {
  if (!daemonApi) return;

  // Mid-connect this is Stop: cancel the attempt in main, or it brings the
  // tunnel up a moment later. Its own finally clears the busy state.
  const stoppingAttempt = connectInFlight && pangeaApi !== null;

  clearActiveConnectionMessages?.();
  notifyUserDisconnected();
  beginOptimisticDisconnect();
  setUiMessage(stoppingAttempt ? t("connect.cancelled") : t("connect.disconnecting"));

  void (async () => {
    try {
      if (stoppingAttempt) {
        await pangeaApi?.cancelConnect();
      } else {
        const result = await daemonApi.disconnect();
        if (!result.ok) setUiMessage(t("connect.disconnectFailed"));
      }
    } catch (error) {
      setUiMessage(reportError(stoppingAttempt ? "cancelConnect" : "serverDisconnect", error));
    } finally {
      await reconcileDisconnect();
    }
  })();
});

/** Polls until the daemon confirms the teardown the user already sees, so a
 *  tunnel that outlived its optimistic window is not left misreported. */
async function reconcileDisconnect(): Promise<void> {
  for (let attempt = 0; attempt < 6; attempt += 1) {
    const status = await refreshStatus();
    if (!status || status.state === "DISCONNECTED") {
      if (status) setUiMessage(t("connect.disconnected"));
      return;
    }
    if (!disconnectingVisual) return;
    await new Promise((resolve) => setTimeout(resolve, 750));
  }
}

serverRefreshBtn.addEventListener("click", async () => {
  await refreshServers();
});

// Settings toggles sit under the overlay that covers #uiMessage, so feedback
// goes through showToast; each reverts its checkbox if the backend call fails.
settingsOverlay.addEventListener("change", updateSettingsSummaries);

type HubMethodName = "directIp" | "shadowsocks" | "fronted" | "normal";
type HubMethodState = {
  directIp: boolean;
  shadowsocks: boolean;
  fronted: boolean;
  normal: boolean;
};

const hubMethodToggles: Record<HubMethodName, HTMLInputElement> = {
  directIp: hubDirectIpToggle,
  shadowsocks: hubShadowsocksToggle,
  fronted: hubFrontedToggle,
  normal: hubNormalToggle
};

const hubMethodLabels: Record<HubMethodName, MessageKey> = {
  directIp: "settings.provisioning.directIp.title",
  shadowsocks: "settings.provisioning.hubShadowsocks.title",
  fronted: "settings.provisioning.hubFronted.title",
  normal: "settings.provisioning.hubNormal.title"
};

/** Mirrors main-process state onto the switches, and locks the last one on so
 *  the user cannot leave the app with no way to reach the hub. */
function renderHubMethods(methods: HubMethodState): void {
  const names = Object.keys(hubMethodToggles) as HubMethodName[];
  const enabled = names.filter((name) => methods[name]);
  for (const name of names) {
    const toggle = hubMethodToggles[name];
    toggle.checked = methods[name];
    toggle.disabled = enabled.length === 1 && methods[name];
    toggle.title = toggle.disabled ? t("settings.provisioning.lastMethod") : "";
  }
  updateSettingsSummaries();
}

function wireHubMethodToggle(name: HubMethodName): void {
  hubMethodToggles[name].addEventListener("change", async () => {
    if (!pangeaApi) return;
    const requested = hubMethodToggles[name].checked;
    try {
      const result = await pangeaApi.setHubMethod(name, requested);
      renderHubMethods(result.methods);
      if (!result.applied) {
        showToast(t("settings.provisioning.lastMethod"), 4000, true);
        return;
      }
      showToast(
        `${t(hubMethodLabels[name])} — ${requested ? t("toggle.hubMethod.on") : t("toggle.hubMethod.off")}`,
        3000,
        true
      );
    } catch (err) {
      hubMethodToggles[name].checked = !requested;
      showToast(reportError(`hubMethod:${name}`, err, t("toggle.updateFailed")));
    }
  });
}

(Object.keys(hubMethodToggles) as HubMethodName[]).forEach(wireHubMethodToggle);

allowLanToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  try {
    await pangeaApi.setAllowLan(allowLanToggle.checked);
    showToast(allowLanToggle.checked
      ? t("toggle.allowLan.on")
      : t("toggle.allowLan.off"), 4000, true);
  } catch (err) {
    allowLanToggle.checked = !allowLanToggle.checked;
    showToast(reportError("allowLan", err, t("toggle.updateFailed")));
  }
});

// Commits on blur/Enter. Main owns validation and returns what it actually
// stored, so the field always ends up showing the truth rather than the typo.
wireguardMtuInput.addEventListener("change", async () => {
  if (!pangeaApi) return;
  const requested = wireguardMtuInput.value.trim();
  try {
    const stored = await pangeaApi.setWireguardMtu(Number(requested));
    // Always show what was actually stored; the field self-corrects, so there's
    // no invalid state to style — the toast explains why it changed.
    wireguardMtuInput.value = String(stored);
    const rejected = requested === "" || Number(requested) !== stored;
    if (rejected) {
      showToast(t("settings.network.mtu.invalid", { min: MTU_MIN, max: MTU_MAX }));
    } else {
      showToast(t("settings.network.mtu.saved", { mtu: stored }), 4000, true);
    }
  } catch (err) {
    wireguardMtuInput.value = String(await pangeaApi.getWireguardMtu().catch(() => MTU_DEFAULT));
    showToast(reportError("wireguardMtu", err, t("toggle.updateFailed")));
  }
});

function syncDnsControls(servers: readonly string[]): void {
  const choice = dnsChoiceFor(servers);
  dnsPresetSelect.value = choice;
  customDnsField.hidden = choice !== "custom";
  customDnsInput.value = servers.join(", ");
}

async function saveDns(requested: string): Promise<void> {
  if (!pangeaApi) return;
  dnsPresetSelect.disabled = true;
  customDnsInput.disabled = true;
  try {
    const stored = await pangeaApi.setCustomDns(requested);
    syncDnsControls(stored);
    showToast(
      stored.length > 0
        ? t("settings.network.dns.saved", { dns: stored.join(", ") })
        : t("settings.network.dns.defaultSaved"),
      4000,
      true
    );
  } catch (err) {
    syncDnsControls(await pangeaApi.getCustomDns().catch(() => []));
    const invalid = err instanceof Error && err.message.includes("Custom DNS");
    showToast(invalid
      ? t("settings.network.dns.invalid")
      : reportError("customDns", err, t("toggle.updateFailed")));
  } finally {
    dnsPresetSelect.disabled = false;
    customDnsInput.disabled = false;
    updateSettingsSummaries();
  }
}

dnsPresetSelect.addEventListener("change", async () => {
  const choice = dnsPresetSelect.value as DnsChoice;
  const servers = dnsServersFor(choice);
  if (servers === null) {
    customDnsField.hidden = false;
    customDnsInput.focus();
    customDnsInput.select();
    return;
  }
  await saveDns(servers.join(", "));
});

customDnsInput.addEventListener("change", async () => {
  await saveDns(customDnsInput.value.trim());
});

preferredTransportSelect.addEventListener("change", async () => {
  if (!pangeaApi) return;
  const previous = preferredTransportSelect.dataset.previousValue ?? "auto";
  const choice = preferredTransportSelect.value as TransportChoice;
  try {
    await pangeaApi.setPreferredTransport(choice);
    preferredTransportSelect.dataset.previousValue = choice;
    // Re-filter the server list: hide servers that don't support the new choice.
    renderServers();
    showToast(t("toggle.preferredTransport.updated"), 4000, true);
  } catch (err) {
    preferredTransportSelect.value = previous;
    // The revert lands after the change event, so the picker needs telling.
    syncTransportChoice();
    showToast(reportError("preferredTransport", err, t("toggle.updateFailed")));
  }
});

launchAtStartupToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  try {
    await pangeaApi.setLaunchAtStartup(launchAtStartupToggle.checked);
    showToast(launchAtStartupToggle.checked
      ? t("toggle.launch.on")
      : t("toggle.launch.off"), 3000, true);
  } catch (err) {
    launchAtStartupToggle.checked = !launchAtStartupToggle.checked;
    showToast(reportError("launchAtStartup", err, t("toggle.launch.failed")));
  }
});

autoConnectToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  autoConnectLocal = autoConnectToggle.checked;
  try {
    await pangeaApi.setAutoConnect(autoConnectLocal);
  } catch (err) {
    autoConnectLocal = !autoConnectLocal;
    autoConnectToggle.checked = autoConnectLocal;
    showToast(reportError("autoConnect", err, t("toggle.autoConnect.failed")));
    return;
  }
  notifyToggleChanged(autoConnectLocal);
  if (autoConnectLocal) {
    showToast(t("toggle.autoConnect.on"), 4000, true);
    void attemptInitialAutoConnect();
  } else {
    showToast(t("toggle.autoConnect.off"), 4000, true);
  }
});

lockdownToggle.addEventListener("change", async () => {
  if (!pangeaApi) return;
  lockdownLocal = lockdownToggle.checked;
  try {
    await pangeaApi.setLockdown(lockdownLocal);
  } catch (err) {
    lockdownLocal = !lockdownLocal;
    lockdownToggle.checked = lockdownLocal;
    showToast(reportError("lockdown", err, t("toggle.lockdown.failed")));
    return;
  }
  showToast(lockdownLocal ? t("toggle.lockdown.on") : t("toggle.lockdown.off"), 5000, true);
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
const daemonRecoveryScreen = document.getElementById("daemonRecoveryScreen") as HTMLElement;
const daemonRecoveryBtn = document.getElementById("daemonRecoveryBtn") as HTMLButtonElement;
const daemonRecoveryMessage = document.getElementById("daemonRecoveryMessage") as HTMLParagraphElement;
const daemonRecoveryButtonLabel = daemonRecoveryBtn.querySelector<HTMLElement>(".daemon-recovery-button-label")!;
let daemonHealth = initialDaemonHealth();
let daemonRecoveryRestartInProgress = false;
let daemonRecoveryReturnFocus: HTMLElement | null = null;
let daemonRecoveryResolvers: Array<() => void> = [];
const daemonRecoveryInerted = new Set<HTMLElement>();

function showDaemonRecovery(): void {
  if (!daemonRecoveryScreen.hidden) return;

  daemonRecoveryReturnFocus = document.activeElement as HTMLElement | null;
  for (const child of Array.from(document.body.children)) {
    if (!(child instanceof HTMLElement) || child === daemonRecoveryScreen || child.tagName === "SCRIPT") continue;
    if (!child.hasAttribute("inert")) {
      child.setAttribute("inert", "");
      daemonRecoveryInerted.add(child);
    }
  }
  daemonRecoveryScreen.hidden = false;
  daemonRecoveryScreen.dataset.state = "error";
  daemonRecoveryMessage.textContent = "";
  daemonRecoveryBtn.disabled = false;
  daemonRecoveryBtn.removeAttribute("aria-busy");
  daemonRecoveryButtonLabel.textContent = t("daemonRecovery.restart");
  window.setTimeout(() => daemonRecoveryBtn.focus(), 0);
}

function completeDaemonRecovery(force = false): void {
  daemonHealth = daemonHealthAfterSuccess(daemonHealth);
  if (daemonRecoveryRestartInProgress && !force) return;

  daemonRecoveryRestartInProgress = false;
  if (!daemonRecoveryScreen.hidden) {
    daemonRecoveryScreen.hidden = true;
    delete daemonRecoveryScreen.dataset.state;
    daemonRecoveryBtn.removeAttribute("aria-busy");
    for (const element of daemonRecoveryInerted) element.removeAttribute("inert");
    daemonRecoveryInerted.clear();
    daemonRecoveryReturnFocus?.focus?.();
    daemonRecoveryReturnFocus = null;
  }
  const resolvers = daemonRecoveryResolvers;
  daemonRecoveryResolvers = [];
  for (const resolve of resolvers) resolve();
}

function waitForDaemonRecovery(): Promise<void> {
  showDaemonRecovery();
  return new Promise((resolve) => daemonRecoveryResolvers.push(resolve));
}

daemonRecoveryBtn.addEventListener("click", async () => {
  if (!daemonApi || daemonRecoveryBtn.disabled) return;

  daemonRecoveryBtn.disabled = true;
  daemonRecoveryRestartInProgress = true;
  daemonRecoveryScreen.dataset.state = "working";
  daemonRecoveryBtn.setAttribute("aria-busy", "true");
  daemonRecoveryButtonLabel.textContent = t("daemonRecovery.restarting");
  daemonRecoveryMessage.textContent = t("daemonRecovery.waitingForApproval");
  try {
    const result = await daemonApi.restartDaemon();
    if (!result.ok) {
      throw new Error(result.error || "daemon restart failed");
    }
    await daemonApi.getStatus();
    completeDaemonRecovery(true);
  } catch (error) {
    console.error("[daemonRecovery]", error);
    daemonRecoveryRestartInProgress = false;
    daemonRecoveryScreen.dataset.state = "error";
    daemonRecoveryBtn.removeAttribute("aria-busy");
    daemonRecoveryMessage.textContent = verboseErrors
      ? `${t("daemonRecovery.failed")} ${error instanceof Error ? error.message : String(error)}`
      : t("daemonRecovery.failed");
    daemonRecoveryBtn.disabled = false;
    daemonRecoveryButtonLabel.textContent = t("daemonRecovery.tryAgain");
    daemonRecoveryBtn.focus();
  }
});

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

// ── Language selection ────────────────────────────────

// Resolved once at startup, before the shell shows. Changing it persists the
// choice and takes effect on the next launch.
const LANGUAGE_SYSTEM = "system";
const languageSelect = document.getElementById("languageSelect") as HTMLSelectElement | null;
const languageRestartHint = document.getElementById("languageRestartHint") as HTMLElement | null;

async function readStoredLocale(): Promise<string | null> {
  try {
    return (await pangeaApi?.getLocale?.()) ?? null;
  } catch {
    return null;
  }
}

async function applyStoredLocale(): Promise<void> {
  initLocale(resolveLocale(await readStoredLocale()));
}

function initLanguagePicker(): void {
  if (!languageSelect) return;
  languageSelect.innerHTML = "";
  const systemOption = document.createElement("option");
  systemOption.value = LANGUAGE_SYSTEM;
  systemOption.textContent = t("settings.language.system");
  languageSelect.append(systemOption);
  for (const meta of LOCALES) {
    const option = document.createElement("option");
    option.value = meta.code;
    option.textContent = meta.nativeName;
    languageSelect.append(option);
  }

  // Reflect the stored preference (System default vs. a pinned language).
  void readStoredLocale().then((stored) => {
    languageSelect.value = stored && stored.length > 0 ? stored : LANGUAGE_SYSTEM;
  });

  languageSelect.addEventListener("change", () => {
    const choice = languageSelect.value;
    void pangeaApi?.setLocale?.(choice).catch(() => {});
    if (languageRestartHint) languageRestartHint.hidden = false;
    showToast(t("settings.language.restartHint"), 5000, true);
  });
}

async function init(): Promise<void> {
  initTheme();
  mapHost.replaceChildren(buildDriftMap());
  setInterval(renderSessionClock, 1000);
  await applyStoredLocale();
  initLanguagePicker();

  if (!daemonApi) {
    loadingMessage.textContent = t("app.loading.cantStart");
    return;
  }

  // Poll until daemon responds (max 30s), then offer an explicit elevated
  // recovery instead of leaving the user stranded on the loading screen.
  const maxAttempts = 60;
  for (let i = 0; i < maxAttempts; i++) {
    const remaining = Math.ceil((maxAttempts - i) * 0.5);
    loadingMessage.textContent = t("app.loading.progress", { remaining });
    try {
      const status = await daemonApi.getStatus();
      if (status) {
        break;
      }
    } catch {
      // not ready
    }
    if (i === maxAttempts - 1) {
      loadingMessage.textContent = t("app.loading.didntStart");
      await waitForDaemonRecovery();
    }
    await new Promise((r) => setTimeout(r, 500));
  }

  void hideLoadingScreen();

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
    showToast(t("auth.signedOutRetry"));
  });

  initCollapsibleSections();
  await renderAppVersion();
  updateBusyIndicator();
  await refreshAll(true);

  if (pangeaApi) {
    try {
      renderHubMethods(await pangeaApi.getHubMethods());
      allowLanToggle.checked = await pangeaApi.getAllowLan();
      syncDnsControls(await pangeaApi.getCustomDns());
      wireguardMtuInput.value = String(await pangeaApi.getWireguardMtu());
      const preferredTransport = await pangeaApi.getPreferredTransport();
      preferredTransportSelect.value = preferredTransport;
      preferredTransportSelect.dataset.previousValue = preferredTransport;
    } catch {
      // default off
    }

    try {
      const isPackaged = await pangeaApi.getIsPackaged();
      if (!isPackaged) {
        launchAtStartupToggle.disabled = true;
        launchAtStartupToggle.title = t("toggle.launch.packagedOnly");
      } else {
        launchAtStartupToggle.checked = await pangeaApi.getLaunchAtStartup();
      }
      autoConnectLocal = await pangeaApi.getAutoConnect();
      autoConnectToggle.checked = autoConnectLocal;
      lockdownLocal = await pangeaApi.getLockdown();
      lockdownToggle.checked = lockdownLocal;
      const last = await pangeaApi.getLastServer();
      lastServerIdLocal = last.lastServerId;
    } catch {
      // defaults already in place
    }

    initAutoConnect({
      getEnabled: () => autoConnectLocal,
      getAuthenticated: () => authState.authenticated,
      getDaemonState: () => currentDaemonState,
      getUserIntent,
      getConnectionInFlight: () => connectInFlight,
      getLastServerId: () => lastServerIdLocal,
      getFallbackServerId: () => pickRandomServer(getVisibleServers())?.id ?? null,
      provisionAndSwitch: (serverId: string) => pangeaApi.provisionAndConnect(serverRetryPlan(serverId)),
      // Pull the server auto-connect settled on into the picker, so it can't sit
      // on "Select server" while we're actually connected.
      onConnected: () => void refreshLastServer().then(renderServers)
    });

    await loadCachedServers();
    if (authState.authenticated) {
      await refreshServers();
    }

    if (autoConnectLocal && authState.authenticated) {
      void attemptInitialAutoConnect();
    }
  }

  // Check for updates regardless of auth state.
  checkForUpdate();

  schedulePoll();

  // Logs stay slow deliberately: each pass re-renders the whole pane, and no
  // one reads a log tail four times a second.
  let logsInterval = 2000;
  const logsMax = 15000;

  function scheduleLogsPoll(): void {
    setTimeout(async () => {
      try {
        await refreshLogs();
        logsInterval = 2000;
      } catch {
        logsInterval = Math.min(logsInterval * 2, logsMax);
      }
      scheduleLogsPoll();
    }, logsInterval);
  }
  scheduleLogsPoll();
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
    updateDownloadBtn.textContent = t("update.copyCommand");
    updateMessageEl.textContent = "";
  } else {
    updateMacInstall.hidden = true;
    if (updateDownloaded) {
      updateDownloadBtn.textContent = t("update.restartToUpdate");
      updateMessageEl.textContent = t("update.readyToInstall");
    } else {
      updateDownloadBtn.textContent = t("update.download");
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
    updateDownloadBtn.textContent = t("update.restartToUpdate");
    updateMessageEl.textContent = t("update.readyToInstall");
  });

  updater.onUpdateError((message) => {
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = t("update.retryDownload");
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

// checkForUpdates() resolves with the latest release whether or not it's newer,
// so compare against the running version; onUpdateAvailable shows the modal.
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
    showToast(t("update.unavailable"));
    return;
  }
  checkUpdatesBtn.disabled = true;
  const label = checkUpdatesBtn.textContent;
  checkUpdatesBtn.textContent = t("update.checking");
  try {
    const info = await updater.checkForUpdates();
    if (info && isNewerVersion(info.version, currentAppVersion)) {
      // Open the update modal directly so it's actionable from Settings, even if
      // this version was dismissed earlier (which would otherwise suppress it).
      pendingUpdate = pendingUpdate ?? { version: info.version };
      localStorage.removeItem(UPDATE_DISMISSED_KEY);
      showUpdateModal();
    } else if (info) {
      showToast(t("update.onLatest"), 4000, true);
    } else {
      showToast(t("update.checkFailed"));
    }
  } catch {
    showToast(t("update.checkFailed"));
  } finally {
    checkUpdatesBtn.textContent = label;
    checkUpdatesBtn.disabled = false;
  }
});

async function copyMacInstallCommand(): Promise<void> {
  try {
    await navigator.clipboard.writeText(MAC_INSTALL_COMMAND);
    updateDownloadBtn.textContent = t("update.copied");
    updateMessageEl.textContent = t("update.macPasteHint");
    setTimeout(() => {
      updateDownloadBtn.textContent = t("update.copyCommand");
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
  updateDownloadBtn.textContent = t("update.opening");
  updateMessageEl.textContent = "";

  try {
    await updater.downloadUpdate();
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = t("update.viewDownload");
  } catch (error) {
    updateDownloadBtn.disabled = false;
    updateDownloadBtn.textContent = t("update.retry");
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
      showToast(verboseErrors ? t("verbose.on") : t("verbose.off"));
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
    setUiMessage("");
  } catch (error) {
    console.error("[daemonSync]", error);
    setUiMessage(t("common.retrying"));
    // Retry once after a short delay
    try {
      await new Promise((r) => setTimeout(r, 2000));
      await Promise.all([refreshStatus(), refreshLogs()]);
      setUiMessage("");
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
    completeDaemonRecovery();
    renderStatus(status);
    return status;
  } catch (error) {
    daemonHealth = daemonHealthAfterFailure(daemonHealth);
    if (daemonHealth.recoveryRequired) showDaemonRecovery();
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

// Display names for the active-transport pill — protocol/product proper nouns,
// identical across locales, so they are not translated.
const TRANSPORT_LABELS: Record<string, string> = {
  cloak: "Cloak",
  naive: "NaiveProxy",
  reality: "VLESS+REALITY",
  shadowsocks: "Shadowsocks",
  hysteria2: "Hysteria2",
  snowflake: "Snowflake",
  wireguard: "WireGuard",
};

// The status block for whichever transport is currently active, so the pill
// reflects the live transport rather than always Cloak.
const EM_DASH = "—";

/** The {x} placeholder lets each locale put the stressed word where its own
 *  grammar needs it, without putting markup in the catalogues. */
function setHeadline(state: StatusResponse["state"]): void {
  const [before, after = ""] = t(("hero.headline." + state) as MessageKey).split("{x}");
  const strong = document.createElement("strong");
  strong.textContent = t(("hero.emphasis." + state) as MessageKey);
  heroHeadline.replaceChildren(document.createTextNode(before), strong, document.createTextNode(after));
}

const MIN_CONNECTING_MS = 600;

// 4Hz while a transition is in flight, so a state change lands on screen
// almost immediately; 2s once things settle, so an idle app is not spinning.
const POLL_FAST_MS = 250;
const POLL_IDLE_MS = 2000;
const POLL_MAX_MS = 10000;

// The daemon reports CONNECTED before the last of the teardown/route work is
// done, so the fast cadence outlives the transition that triggered it.
const POLL_FAST_TRAILING_MS = 3000;

let lastTransitionAt = 0;
let pollTimer: ReturnType<typeof setTimeout> | null = null;

/** Is the connection moving, either really or as far as the user can see? */
function inTransition(): boolean {
  return (
    connectingVisual ||
    disconnectingVisual ||
    connectInFlight ||
    serverWorking ||
    currentDaemonState === "CONNECTING" ||
    currentDaemonState === "DISCONNECTING"
  );
}

function nextPollDelay(): number {
  // Hidden means the tray is the only thing on screen, and main polls that
  // itself — visibilitychange forces a fresh sample the moment we come back.
  if (document.hidden) return POLL_IDLE_MS;
  if (inTransition()) {
    lastTransitionAt = Date.now();
    return POLL_FAST_MS;
  }
  return Date.now() - lastTransitionAt < POLL_FAST_TRAILING_MS ? POLL_FAST_MS : POLL_IDLE_MS;
}

// Fast only while something is actually moving. A settled tunnel changes state
// when the user asks it to, so 4Hz idle polling buys nothing.
let pollBackoff = 0;

function schedulePoll(): void {
  const delay = pollBackoff > 0 ? pollBackoff : nextPollDelay();
  pollTimer = setTimeout(async () => {
    pollTimer = null;
    // refreshStatus swallows its own errors and reports null; treat that as the
    // failure the backoff was always meant to respond to.
    const status = await refreshStatus();
    pollBackoff = status ? 0 : Math.min(Math.max(pollBackoff * 2, POLL_IDLE_MS), POLL_MAX_MS);
    notifyStatusTick();
    schedulePoll();
  }, delay);
}

/** Collapses the wait when something has just changed, so a click is not left
 *  sitting behind an idle-cadence timer that was scheduled before it. */
function pollNow(): void {
  lastTransitionAt = Date.now();
  if (pollTimer === null) return;
  clearTimeout(pollTimer);
  pollTimer = null;
  void (async () => {
    await refreshStatus();
    notifyStatusTick();
    schedulePoll();
  })();
}

// How long the UI will present a disconnect the daemon has not confirmed. Past
// this the truth wins, however unwelcome — a lie that never expires is worse.
const OPTIMISTIC_DISCONNECT_MS = 12000;

// Set the instant Stop or Disconnect is pressed, so the user is out of the
// tunnel on screen before the daemon has finished tearing it down.
let disconnectingVisual = false;
let disconnectRequestedAt = 0;

function beginOptimisticDisconnect(): void {
  disconnectingVisual = true;
  disconnectRequestedAt = Date.now();
  pollNow();
  connectingVisual = false;
  connectedSince = null;
  renderDisconnectedState();
  updateControlStates();
}

function renderDisconnectedState(): void {
  heroCard.dataset.state = "DISCONNECTED";
  document.body.dataset.state = "DISCONNECTED";
  stateEl.textContent = t("state.DISCONNECTED");
  detailEl.textContent = "";
  setHeadline("DISCONNECTED");
  serverConnectBtn.hidden = false;
  serverDisconnectBtn.hidden = true;
}

/** True while the optimistic view still stands: the daemon has not yet agreed,
 *  and the grace window has not run out. */
function holdingOptimisticDisconnect(state: StatusResponse["state"]): boolean {
  if (!disconnectingVisual) return false;
  if (state === "DISCONNECTED") {
    disconnectingVisual = false;
    return false;
  }
  if (Date.now() - disconnectRequestedAt > OPTIMISTIC_DISCONNECT_MS) {
    disconnectingVisual = false;
    return false;
  }
  return true;
}

// The 2s status poll samples straight past a connect/switch's transient
// states, so while we are driving one the hero follows the operation, not the poll.
let connectingVisual = false;

function showConnectingState(): number {
  // A new Connect supersedes any Stop still waiting on its daemon confirmation.
  disconnectingVisual = false;
  connectingVisual = true;
  renderConnectingState();
  pollNow();
  return Date.now();
}

function renderConnectingState(): void {
  heroCard.dataset.state = "CONNECTING";
  document.body.dataset.state = "CONNECTING";
  stateEl.textContent = t("state.CONNECTING");
  detailEl.textContent = "";
  setHeadline("CONNECTING");
}

// Holds a fast connect/switch on CONNECTING long enough to read as a
// transition. Only ever delays the render; a slower op waits not at all.
async function settleConnectingState(startedAt: number): Promise<void> {
  const remaining = MIN_CONNECTING_MS - (Date.now() - startedAt);
  if (remaining > 0) {
    await new Promise((resolve) => setTimeout(resolve, remaining));
  }
  connectingVisual = false;
}

// Wall-clock start of the current session, derived in the renderer — the
// daemon does not report uptime.
let connectedSince: number | null = null;

function renderSessionClock(): void {
  if (connectedSince === null) {
    factSessionEl.textContent = EM_DASH;
    return;
  }
  const total = Math.max(0, Math.floor((Date.now() - connectedSince) / 1000));
  const hours = Math.floor(total / 3600);
  const minutes = Math.floor((total % 3600) / 60);
  const seconds = total % 60;
  const pad = (n: number): string => String(n).padStart(2, "0");
  factSessionEl.textContent = hours > 0
    ? `${hours}:${pad(minutes)}:${pad(seconds)}`
    : `${pad(minutes)}:${pad(seconds)}`;
}

function renderStatus(status: StatusResponse): void {
  latestStatus = status;
  currentDaemonState = status.state;

  // Beats connectingVisual: Stop was pressed after Connect, so it is the newer
  // instruction, and a teardown in progress is already "off" to the user.
  const optimisticallyOff = holdingOptimisticDisconnect(status.state);

  // A poll landing mid-switch must not yank the hero back off CONNECTING.
  if (optimisticallyOff) {
    renderDisconnectedState();
  } else if (connectingVisual) {
    renderConnectingState();
  } else {
    stateEl.textContent = t(("state." + status.state) as MessageKey);
    detailEl.textContent = status.detail;
    heroCard.dataset.state = status.state;
    document.body.dataset.state = status.state;
    setHeadline(status.state);
  }

  // Throughput stats
  const connected = status.state === "CONNECTED" && !optimisticallyOff;
  const wg = status.wireguard as StatusResponse["wireguard"] & { bytesIn?: number; bytesOut?: number };
  connectedSince = connected ? connectedSince ?? Date.now() : null;
  rxBytesEl.textContent = connected ? formatBytes(wg.bytesIn ?? 0) : EM_DASH;
  txBytesEl.textContent = connected ? formatBytes(wg.bytesOut ?? 0) : EM_DASH;
  factViaEl.textContent = status.activeTransport
    ? TRANSPORT_LABELS[status.activeTransport] ?? status.activeTransport
    : EM_DASH;
  renderSessionClock();

  // Recovery toast — cloak was down last poll, now it's back
  if (lastCloakWasDown && status.cloak.running && connected) {
    showToast(t("connect.recovered"));
  }
  lastCloakWasDown = !status.cloak.running && connected;

  // Show connect vs disconnect button.
  // Show disconnect in ERROR state too — kill switch may still be active.
  const showDisconnect =
    !optimisticallyOff && (connected || status.state === "CONNECTING" || status.state === "ERROR");
  serverConnectBtn.hidden = showDisconnect;
  serverDisconnectBtn.hidden = !showDisconnect;

  updateControlStates();
  updateBusyIndicator();
}

function renderLogs(entries: LogEntry[]): void {
  const lines = entries.slice(-300).map((entry) => {
    const date = new Date(entry.ts).toLocaleTimeString(localeTag());
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
  for (const btn of themeToggleBtns) {
    btn.textContent = theme === "dark" ? "\u2600" : "\u263D";
    btn.setAttribute("aria-pressed", String(theme === "dark"));
  }

  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme);
  } catch {
    // Ignore storage failures in restricted environments.
  }
}

function setUiMessage(message: string): void {
  uiMessageEl.textContent = message;
}

let clearActiveConnectionMessages: (() => void) | null = null;

function startConnectionProgressMessages(): () => void {
  clearActiveConnectionMessages?.();
  const clearScheduled = scheduleConnectionMessages(
    [t("connect.stillConnecting"), t("connect.takingLonger"), t("connect.stillWorking")],
    setUiMessage
  );
  const cleanup = (): void => {
    clearScheduled();
    if (clearActiveConnectionMessages === cleanup) clearActiveConnectionMessages = null;
  };
  clearActiveConnectionMessages = cleanup;
  return cleanup;
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
    serverPanel.hidden = false;
  } else {
    showLoginScreen();
    loginBtn.hidden = !pangeaApi;
    menuDevicesBtn.hidden = true;
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
    setUiMessage(t("connect.refreshServersFailed"));
  }
  // Piggyback on every server refresh (login, picker open, app shown) so the
  // expired state is discovered without its own polling loop.
  void refreshEntitlement();
}

// Backoff until it succeeds so load values stay current. A newer call
// supersedes the loop; it stops on sign-out or while the window is hidden.
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
  el.title = t("serverPicker.load", { pct });
  const bar = document.createElement("span");
  bar.className = "load-bar";
  const fill = document.createElement("span");
  fill.className = "load-fill";
  fill.style.width = `${pct}%`;
  bar.append(fill);
  const label = document.createElement("span");
  label.className = "load-pct";
  label.textContent = t("serverPicker.loadPct", { pct });
  el.append(bar, label);
  return el;
}

type TransportChoice =
  | "auto"
  | "cloak"
  | "naive"
  | "reality"
  | "hysteria2"
  | "shadowsocks"
  | "snowflake"
  | "wireguard";

// Supported means the hub advertised that transport's block. cloak is always
// present; "auto" matches every server and falls back across what it offers.
// "wireguard" needs nothing advertised — every node listens for it.
function serverSupportsTransport(server: ServerInfo, transport: TransportChoice): boolean {
  switch (transport) {
    case "naive":
      return Boolean(server.naive);
    case "reality":
      return Boolean(server.reality);
    case "hysteria2":
      return Boolean(server.hysteria2);
    case "shadowsocks":
      return Boolean(server.shadowsocks);
    case "snowflake":
      return Boolean(server.snowflake);
    default:
      return true; // auto + cloak: every server qualifies
  }
}

// Servers to show for the currently selected transport — hide any that can't
// carry the chosen transport so the user can't pick an unsupported combination.
function getVisibleServers(): ServerInfo[] {
  const choice = (preferredTransportSelect.value as TransportChoice) || "auto";
  return servers.filter((s) => serverSupportsTransport(s, choice));
}

function serverRetryPlan(initialServerId: string): string[] {
  const choice = (preferredTransportSelect.value as TransportChoice) || "auto";
  return choice === "auto"
    ? buildServerRetryOrder(getVisibleServers(), initialServerId)
    : [initialServerId];
}

function applyConnectedServer(serverId: string | undefined): void {
  if (!serverId) return;
  lastServerIdLocal = serverId;
  serverSelect.value = serverId;
  syncServerPicker();
}

const RECENT_REGIONS_KEY = "pangea:recentRegions";
const SLOT_COUNT = 2;

let visibleRegions: Region[] = [];
let recentRegions: string[] = readRecentRegions();
// Set only when the user opened a region and chose a node by hand; otherwise
// the lowest-load node is picked fresh on every connect.
let pinnedNodeId: string | null = null;
const expandedRegions = new Set<string>();

function readRecentRegions(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_REGIONS_KEY);
    const parsed: unknown = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((k): k is string => typeof k === "string") : [];
  } catch {
    return [];
  }
}

function rememberRegion(key: string): void {
  recentRegions = promoteRecent(recentRegions, key);
  try {
    localStorage.setItem(RECENT_REGIONS_KEY, JSON.stringify(recentRegions));
  } catch {
    // A full or blocked localStorage only costs us the ordering, so carry on.
  }
}

/** The node a region would connect to: the user's pin, else the lightest. */
function nodeForRegion(region: Region): ServerInfo {
  const pinned = pinnedNodeId && region.nodes.find((n) => n.id === pinnedNodeId);
  return pinned || pickNode(region);
}

function selectedRegion(): Region | undefined {
  return regionOfServer(visibleRegions, serverSelect.value);
}

/** Connects to (or switches to) a region, optionally pinned to one node. */
function activateRegion(region: Region, nodeId: string | null): void {
  closeServerPicker();
  if (serverWorking) return;

  pinnedNodeId = nodeId;
  const target = nodeId ? region.nodes.find((n) => n.id === nodeId) ?? pickNode(region) : pickNode(region);
  rememberRegion(region.key);

  // Only commit the selection when an action will actually run, so the slots
  // can't drift out of sync with the connected node.
  if (currentDaemonState === "CONNECTED") {
    serverSelect.value = target.id;
    syncServerPicker();
    void switchToServer(target.id);
  } else if (!serverConnectBtn.disabled) {
    serverSelect.value = target.id;
    syncServerPicker();
    serverConnectBtn.click();
  } else {
    serverSelect.value = target.id;
    syncServerPicker();
  }
}

function buildRegionRow(region: Region, forPicker: boolean): HTMLElement {
  const node = nodeForRegion(region);
  const isCurrent = region.nodes.some((n) => n.id === serverSelect.value);
  const count = region.nodes.length;

  const row = document.createElement("button");
  row.type = "button";
  row.className = "region-row";
  row.dataset.key = region.key;
  row.setAttribute("aria-current", String(isCurrent));

  row.append(buildFlag(region.country));

  const text = document.createElement("span");
  text.className = "region-text";
  const name = document.createElement("span");
  name.className = "region-name";
  name.textContent = region.name;
  const sub = document.createElement("span");
  sub.className = "region-sub";
  if (isCurrent) {
    sub.textContent = pinnedNodeId
      ? t("region.pinned", { node: node.id })
      : count > 1 ? t("region.bestOf", { node: node.id, count: String(count) }) : node.id;
  } else {
    sub.textContent = count > 1 ? t("region.autoOf", { count: String(count) }) : node.id;
  }
  text.append(name, sub);
  row.append(text);

  const load = buildLoadIndicator(node.load);
  if (load) row.append(load);

  const tick = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  tick.setAttribute("class", "region-tick");
  tick.setAttribute("viewBox", "0 0 24 24");
  tick.setAttribute("fill", "none");
  tick.setAttribute("aria-hidden", "true");
  tick.innerHTML =
    '<path d="m5 13 4.5 4.5L19 7" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"/>';
  row.append(tick);

  row.addEventListener("click", () => activateRegion(region, null));

  if (!forPicker || count < 2) return row;
  return buildExpandableRegion(region, row);
}

/** Wraps a picker row so its sibling chevron can disclose the region's nodes. */
function buildExpandableRegion(region: Region, row: HTMLElement): HTMLElement {
  const wrap = document.createElement("div");
  wrap.className = "region-wrap";
  wrap.dataset.key = region.key;
  wrap.dataset.current = String(region.nodes.some((n) => n.id === serverSelect.value));

  const head = document.createElement("div");
  head.className = "region-head";

  const expanded = expandedRegions.has(region.key);
  const toggle = document.createElement("button");
  toggle.type = "button";
  toggle.className = "region-expand";
  toggle.setAttribute("aria-expanded", String(expanded));
  toggle.setAttribute("aria-label", t("region.showServers", { region: region.name }));
  toggle.innerHTML =
    '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m6 9 6 6 6-6"/></svg>';

  head.append(row, toggle);
  wrap.append(head);

  const list = document.createElement("div");
  list.className = "region-nodes";
  list.hidden = !expanded;

  const best = pickNode(region);
  for (const node of region.nodes) {
    const item = document.createElement("button");
    item.type = "button";
    item.className = "region-node";

    const id = document.createElement("span");
    id.className = "region-node-id";
    id.textContent = node.id;
    item.append(id);

    if (node.id === pinnedNodeId || node.id === best.id) {
      const badge = document.createElement("span");
      badge.className = "region-node-badge";
      badge.textContent = node.id === pinnedNodeId ? t("region.badgePinned") : t("region.badgeBest");
      item.append(badge);
    }

    const load = buildLoadIndicator(node.load);
    if (load) item.append(load);

    item.addEventListener("click", () => activateRegion(region, node.id));
    list.append(item);
  }

  toggle.addEventListener("click", () => {
    const open = expandedRegions.has(region.key);
    if (open) expandedRegions.delete(region.key);
    else expandedRegions.add(region.key);
    toggle.setAttribute("aria-expanded", String(!open));
    list.hidden = open;
  });

  wrap.append(list);
  return wrap;
}

function renderServers(): void {
  const previousValue = serverSelect.value;
  const visible = getVisibleServers();
  visibleRegions = groupRegions(visible);
  serverSelect.innerHTML = "";

  if (visible.length === 0) {
    const noneForTransport = servers.length > 0;
    const emptyText = noneForTransport ? t("hero.noServersForTransport") : t("hero.noServers");
    const option = document.createElement("option");
    option.value = "";
    option.textContent = emptyText;
    serverSelect.append(option);
    serverSelect.disabled = true;
    serverPickerBtn.disabled = true;
    serverConnectBtn.disabled = true;
    regionSlots.replaceChildren(emptyNotice(noneForTransport));
    serverPickerOverlayList.replaceChildren(emptyNotice(noneForTransport));
    regionMoreCount.textContent = "";
    return;
  }

  for (const server of visible) {
    const option = document.createElement("option");
    option.value = server.id;
    option.textContent = `${server.name} (${server.id})`;
    serverSelect.append(option);
  }

  serverSelect.disabled = false;
  serverPickerBtn.disabled = false;
  const resolved = resolveSelection(visible, previousValue, lastServerIdLocal);
  if (!resolved) {
    // A <select> snaps to index 0 for an unmatched value, so an empty selection
    // needs a real placeholder to sit on. Hidden — the region list is the UI.
    const placeholder = document.createElement("option");
    placeholder.value = "";
    placeholder.textContent = t("hero.selectServer");
    serverSelect.prepend(placeholder);
  }
  serverSelect.value = resolved;
  syncServerPicker();

  updateServerControlStates();
}

function emptyNotice(noneForTransport: boolean): HTMLElement {
  const empty = document.createElement("div");
  empty.className = "server-picker-overlay-empty";
  empty.textContent = noneForTransport ? t("serverPicker.noServersForTransport") : t("serverPicker.noServers");
  return empty;
}

function syncServerPicker(): void {
  const ordered = orderByRecent(visibleRegions, recentRegions);
  const current = selectedRegion();

  // The connected region always holds a slot, so it can never scroll away.
  const slots = current && !ordered.slice(0, SLOT_COUNT).includes(current)
    ? [current, ...ordered.filter((r) => r !== current)].slice(0, SLOT_COUNT)
    : ordered.slice(0, SLOT_COUNT);

  regionSlots.replaceChildren(...slots.map((region) => buildRegionRow(region, false)));

  const remaining = visibleRegions.length - slots.length;
  regionMoreCount.textContent = remaining > 0 ? t("region.more", { count: String(remaining) }) : "";
  serverPickerBtn.hidden = visibleRegions.length <= SLOT_COUNT;

  renderRegionPicker(ordered);
}

function renderRegionPicker(ordered: readonly Region[]): void {
  const recent = ordered.filter((region) => recentRegions.includes(region.key));
  const rest = ordered.filter((region) => !recentRegions.includes(region.key));

  const groups: HTMLElement[] = [];
  const addGroup = (labelKey: MessageKey, list: readonly Region[]): void => {
    if (list.length === 0) return;
    const heading = document.createElement("p");
    heading.className = "region-group-heading";
    heading.textContent = t(labelKey);
    const box = document.createElement("div");
    box.className = "region-group";
    box.append(...list.map((region) => buildRegionRow(region, true)));
    groups.push(heading, box);
  };

  if (recent.length > 0) {
    addGroup("serverPicker.recent", recent);
    addGroup("serverPicker.all", rest);
  } else {
    addGroup("serverPicker.all", ordered);
  }

  serverPickerOverlayList.replaceChildren(...groups);
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

  // Not hoisted into the guard above: that would also disable Disconnect and
  // strand a connected user who switched to a transport no server supports.
  serverConnectBtn.disabled =
    busy || !fullyDisconnected || getVisibleServers().length === 0 || entitled === false;
  // Stop is live for the whole attempt — a hard cancel, no unsafe window.
  // Outside an attempt the standard disabled-while-busy rule applies.
  serverDisconnectBtn.disabled = connectInFlight
    ? false
    : !latestStatus || latestStatus.state === "DISCONNECTED" || latestStatus.state === "DISCONNECTING" || busy;
  serverDisconnectBtn.textContent = connectInFlight ? t("hero.stop") : t("hero.disconnect");
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
      // A collapsing row hands its state back to the summary; recompute so a
      // reverted toggle can't leave a stale value on the header.
      updateSettingsSummaries();
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
