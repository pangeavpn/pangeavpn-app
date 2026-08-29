/** Minimum gap between network-change reconnects. */
export const NETWORK_RECOVER_COOLDOWN_MS = 10_000;

export interface NetworkRecoveryDecision {
  autoConnectEnabled: boolean;
  /** The user's last explicit action was Disconnect; no reconnect is wanted. */
  userDisconnected: boolean;
  lastConnectedProfileId: string | null;
  connectionAttemptRunning: boolean;
  recoverInProgress: boolean;
  lastRecoverAtMs: number;
  nowMs: number;
}

/** Whether a network change should bring the last session back up. */
export function shouldRecoverFromNetworkChange(d: NetworkRecoveryDecision): boolean {
  if (d.recoverInProgress || d.connectionAttemptRunning) return false;
  if (d.nowMs - d.lastRecoverAtMs < NETWORK_RECOVER_COOLDOWN_MS) return false;
  if (!d.autoConnectEnabled || d.userDisconnected) return false;
  return d.lastConnectedProfileId !== null;
}
