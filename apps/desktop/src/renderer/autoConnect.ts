import type { StatusResponse } from "@pangeavpn/shared-types";

export type AutoConnectDeps = {
  getEnabled: () => boolean;
  getAuthenticated: () => boolean;
  getDaemonState: () => StatusResponse["state"];
  /** The daemon is rebuilding a dropped session itself; a Connect fired on top
   *  of that queues behind its cascade or restarts it from scratch. */
  getDaemonReconnecting?: () => boolean;
  getUserIntent: () => "connected" | "disconnected";
  getConnectionInFlight: () => boolean;
  /** Reports the attempt's own busy state to the rest of the UI, so a manual
   *  Disconnect click can reach `cancelConnect()` instead of a plain disconnect,
   *  and a manual Connect click can see an auto-connect attempt in progress. */
  setConnectionInFlight?: (inFlight: boolean) => void;
  getLastServerId: () => string | null;
  /** Server to use when nothing has been connected to yet. Re-rolled per attempt. */
  getFallbackServerId: () => string | null;
  /** Servers eligible under the current transport choice; validates a stored
   *  lastServerId that may have been decommissioned or filtered out. */
  getVisibleServers?: () => readonly ServerInfo[];
  provisionAndSwitch: (serverId: string) => Promise<ConnectResult>;
  /** Lets the UI catch up with a server auto-connect chose on the user's behalf. */
  onConnected?: (serverId?: string) => void;
};

// Backoff between retries. Caps at 60s and never gives up — the user asked for "always connected".
const BACKOFF_MS = [2000, 5000, 15000, 30000, 60000];

let deps: AutoConnectDeps | null = null;
let userIntent: "connected" | "disconnected" = "connected";
let consecutiveFailures = 0;
let nextAttemptAtMs = 0;
let inFlight = false;

export function initAutoConnect(d: AutoConnectDeps): void {
  deps = d;
}

export function getUserIntent(): "connected" | "disconnected" {
  return userIntent;
}

export function notifyUserConnected(): void {
  userIntent = "connected";
  consecutiveFailures = 0;
  nextAttemptAtMs = 0;
}

/** A deliberate Connect: the intent is "connected" from the click, so a Stop
 *  during the attempt is the only thing that can leave it "disconnected". */
export function notifyConnectRequested(): void {
  userIntent = "connected";
  consecutiveFailures = 0;
  nextAttemptAtMs = 0;
}

export function notifyUserDisconnected(): void {
  userIntent = "disconnected";
  consecutiveFailures = 0;
  nextAttemptAtMs = 0;
}

export function notifyToggleChanged(enabled: boolean): void {
  consecutiveFailures = 0;
  nextAttemptAtMs = 0;
  if (enabled) {
    userIntent = "connected";
  }
}

// Last connected server, or a fresh random one when there isn't one yet. Each
// call re-rolls the fallback, so a fresh install that hits a dead node moves on
// instead of retrying it forever. A stored id that dropped out of the visible
// set (decommissioned, or unsupported by the current transport) is treated the
// same as having none, so it doesn't saturate backoff on a dead target.
function resolveServerId(): string | null {
  if (!deps) return null;
  const lastId = deps.getLastServerId();
  if (lastId) {
    const visible = deps.getVisibleServers?.();
    if (!visible || visible.some((server) => server.id === lastId)) return lastId;
  }
  return deps.getFallbackServerId();
}

function shouldAttempt(): boolean {
  if (!deps) return false;
  if (!deps.getEnabled()) return false;
  if (!deps.getAuthenticated()) return false;
  if (userIntent !== "connected") return false;
  if (deps.getConnectionInFlight()) return false;
  if (inFlight) return false;
  const state = deps.getDaemonState();
  if (state !== "DISCONNECTED" && state !== "ERROR") return false;
  if (deps.getDaemonReconnecting?.()) return false;
  if (!resolveServerId()) return false;
  if (Date.now() < nextAttemptAtMs) return false;
  return true;
}

async function runAttempt(): Promise<void> {
  if (!deps) return;
  const serverId = resolveServerId();
  if (!serverId) return;
  inFlight = true;
  deps.setConnectionInFlight?.(true);
  try {
    const result = await deps.provisionAndSwitch(serverId);
    // The user may have hit Disconnect while this was in flight; don't
    // override their intent by committing a connected state now.
    if (userIntent !== "connected") return;
    if (result && result.ok) {
      consecutiveFailures = 0;
      // A short cooldown even on success, so a tunnel that drops right back
      // to DISCONNECTED/ERROR doesn't refire on every poll tick.
      nextAttemptAtMs = Date.now() + BACKOFF_MS[0];
      deps.onConnected?.(result.serverId);
    } else if (result?.error !== "cancelled") {
      bumpBackoff();
    }
  } catch {
    if (userIntent === "connected") bumpBackoff();
  } finally {
    inFlight = false;
    deps.setConnectionInFlight?.(false);
  }
}

function bumpBackoff(): void {
  const idx = Math.min(consecutiveFailures, BACKOFF_MS.length - 1);
  nextAttemptAtMs = Date.now() + BACKOFF_MS[idx];
  consecutiveFailures += 1;
}

export function notifyStatusTick(): void {
  if (!shouldAttempt()) return;
  void runAttempt();
}

export async function attemptInitialAutoConnect(): Promise<void> {
  userIntent = "connected";
  consecutiveFailures = 0;
  nextAttemptAtMs = 0;
  if (!shouldAttempt()) return;
  await runAttempt();
}
