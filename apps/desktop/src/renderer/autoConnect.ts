import type { OkResponse, StatusResponse } from "@pangeavpn/shared-types";

export type AutoConnectDeps = {
  getEnabled: () => boolean;
  getAuthenticated: () => boolean;
  getDaemonState: () => StatusResponse["state"];
  getUserIntent: () => "connected" | "disconnected";
  getLastServerId: () => string | null;
  provisionAndSwitch: (serverId: string) => Promise<OkResponse>;
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

function shouldAttempt(): boolean {
  if (!deps) return false;
  if (!deps.getEnabled()) return false;
  if (!deps.getAuthenticated()) return false;
  if (userIntent !== "connected") return false;
  if (inFlight) return false;
  const state = deps.getDaemonState();
  if (state !== "DISCONNECTED" && state !== "ERROR") return false;
  if (!deps.getLastServerId()) return false;
  if (Date.now() < nextAttemptAtMs) return false;
  return true;
}

async function runAttempt(): Promise<void> {
  if (!deps) return;
  const serverId = deps.getLastServerId();
  if (!serverId) return;
  inFlight = true;
  try {
    const result = await deps.provisionAndSwitch(serverId);
    if (result && result.ok) {
      consecutiveFailures = 0;
      nextAttemptAtMs = 0;
    } else {
      bumpBackoff();
    }
  } catch {
    bumpBackoff();
  } finally {
    inFlight = false;
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
