/** How long recovery waits before rotating servers again. A network that blocks
 *  every transport everywhere would otherwise reconnect in a loop. */
export const SERVER_ROTATION_COOLDOWN_MS = 60_000;

export interface RotationDecision {
  /** The daemon ran out of transports on the current server. */
  transportsExhausted: boolean;
  connectionAttemptRunning: boolean;
  rotationInFlight: boolean;
  lastRotationAtMs: number;
  nowMs: number;
}

/** Whether to reconnect on a different server; the daemon cannot, since each
 *  server is a profile the app has to provision. */
export function shouldRotateServers(decision: RotationDecision): boolean {
  if (!decision.transportsExhausted) return false;
  if (decision.connectionAttemptRunning || decision.rotationInFlight) return false;
  return decision.nowMs - decision.lastRotationAtMs > SERVER_ROTATION_COOLDOWN_MS;
}
