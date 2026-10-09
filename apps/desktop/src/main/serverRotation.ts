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

/** Servers to try after failedServerId ran out of transports: retry it first
 *  (a fresh peer revives one the hub dropped), then the rest in retry order. */
export function planAfterServerExhausted(
  retryOrder: readonly string[],
  failedServerId: string | null
): string[] {
  const rest = retryOrder.filter((serverId) => serverId !== failedServerId);
  if (failedServerId && retryOrder.includes(failedServerId)) {
    return [failedServerId, ...rest];
  }
  return rest;
}
