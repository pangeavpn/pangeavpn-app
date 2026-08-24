// When a kill switch left armed by a failed connect may be cleared. The daemon
// is fail-closed, so nothing else releases it once the cascade has given up.

export interface KillSwitchReleaseStatus {
  state: string;
  killSwitchActive?: boolean;
  offline?: boolean;
  cloak: { running: boolean };
  wireguard: { running: boolean };
}

// A connect that fails settles on ERROR, not DISCONNECTED — that is the state
// the stranded user is actually left in, so it is the one that matters most.
const SETTLED_STATES = new Set(["DISCONNECTED", "ERROR"]);

export function shouldReleaseKillSwitch(
  status: KillSwitchReleaseStatus,
  lockdownEnabled: boolean
): boolean {
  // Lockdown is a standing instruction to stay blocked while disconnected.
  if (lockdownEnabled) return false;
  if (status.killSwitchActive !== true) return false;
  // Offline is a hold, not a failure: the daemon still owns the session and
  // re-dials when a link returns, and the lock is what keeps that fail-closed.
  if (status.offline === true) return false;
  // Never unblock around something still running: the lock may be the only
  // thing keeping a half-built session from leaking.
  if (!SETTLED_STATES.has(status.state)) return false;
  return !status.cloak.running && !status.wireguard.running;
}
