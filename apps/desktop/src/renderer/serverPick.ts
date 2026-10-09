// Random server selection for connects with no server picked. Biased toward lightly loaded nodes
// so a first-timer avoids a congested one, but still random so clients don't herd onto one node.

/** Servers at or below this load are preferred. */
export const PREFERRED_MAX_LOAD = 75;

/** Only the fields the pick depends on — keeps this decoupled from ServerInfo. */
type Loadable = { id: string; load?: number | null };

function isLightlyLoaded(server: Loadable): boolean {
  return typeof server.load === "number" && Number.isFinite(server.load) && server.load <= PREFERRED_MAX_LOAD;
}

/** Uniform pick from the lightly loaded servers, falling back to a uniform pick
 *  across all of them when every server is congested or unreported. */
export function pickRandomServer<T extends Loadable>(
  servers: readonly T[],
  random: () => number = Math.random
): T | null {
  if (servers.length === 0) return null;
  const light = servers.filter(isLightlyLoaded);
  const pool = light.length > 0 ? light : servers;
  // Clamp: Math.random() is [0,1), but an injected source at exactly 1 would
  // index past the end and hand the connect path an undefined server.
  const index = Math.min(pool.length - 1, Math.max(0, Math.floor(random() * pool.length)));
  return pool[index];
}

/** The in-session pick if still listed, else the last connected server if still
 *  listed, else "" — self-healing, so a not-yet-loaded id gets another chance. */
export function resolveSelection(
  visible: readonly { id: string }[],
  previousValue: string,
  lastServerId: string | null
): string {
  const listed = (id: string): boolean => visible.some((s) => s.id === id);
  if (previousValue && listed(previousValue)) return previousValue;
  if (lastServerId && listed(lastServerId)) return lastServerId;
  return "";
}
