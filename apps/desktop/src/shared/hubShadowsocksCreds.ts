/** Control-plane Shadowsocks credentials, out of pangeaApiClient so the
 *  cache rules are testable without electron. */
export interface HubShadowsocksCreds {
  remoteHost: string;
  remotePort: number;
  method: string;
  password: string;
}

export function isHubShadowsocksCreds(value: unknown): value is HubShadowsocksCreds {
  const c = value as Partial<HubShadowsocksCreds> | null;
  return (
    !!c &&
    typeof c.remoteHost === "string" &&
    c.remoteHost.trim().length > 0 &&
    typeof c.remotePort === "number" &&
    Number.isInteger(c.remotePort) &&
    c.remotePort > 0 &&
    c.remotePort <= 65535 &&
    typeof c.method === "string" &&
    c.method.trim().length > 0 &&
    typeof c.password === "string" &&
    c.password.length > 0
  );
}

export function sameHubShadowsocks(a: HubShadowsocksCreds, b: HubShadowsocksCreds): boolean {
  return (
    a.remoteHost === b.remoteHost &&
    a.remotePort === b.remotePort &&
    a.method === b.method &&
    a.password === b.password
  );
}

/**
 * Every node the hub named, deduplicated. Caching one node's credentials means
 * a rotation past that node locks the client out of the only path it has left.
 * Returns null when nothing changed, so callers can skip a disk write.
 */
export function mergeAdvertisedCreds(
  current: HubShadowsocksCreds[],
  advertised: unknown[]
): HubShadowsocksCreds[] | null {
  const next: HubShadowsocksCreds[] = [];
  for (const candidate of advertised) {
    if (!isHubShadowsocksCreds(candidate)) continue;
    if (next.some((c) => sameHubShadowsocks(c, candidate))) continue;
    next.push({ ...candidate });
  }
  if (next.length === 0) return null;

  // Keep the node that last worked in front when the hub still lists it, so a
  // refresh does not undo promoteCreds.
  const leader = current[0];
  if (leader) {
    const at = next.findIndex((c) => sameHubShadowsocks(c, leader));
    if (at > 0) next.unshift(next.splice(at, 1)[0]);
  }

  const unchanged =
    next.length === current.length && next.every((c, i) => sameHubShadowsocks(c, current[i]));
  return unchanged ? null : next;
}

/** Moves the entry that just worked to the front, so the next start skips the dead ones. */
export function promoteCreds(
  list: HubShadowsocksCreds[],
  index: number
): HubShadowsocksCreds[] | null {
  if (index <= 0 || index >= list.length) return null;
  const next = list.slice();
  next.unshift(next.splice(index, 1)[0]);
  return next;
}

/**
 * Tries every cached node until one answers, reporting which index won so the
 * caller can promote it. A node whose key has rotated, or that throws, must not
 * end the search — it is usually the only thing standing between the client and
 * a hub it can no longer reach any other way.
 */
export async function firstWorkingCreds<T>(
  list: HubShadowsocksCreds[],
  attempt: (creds: HubShadowsocksCreds, index: number) => Promise<T | null>,
  onError?: (err: unknown, index: number) => void
): Promise<{ value: T; index: number } | null> {
  for (const [index, creds] of list.entries()) {
    try {
      const value = await attempt(creds, index);
      if (value !== null && value !== undefined) return { value, index };
    } catch (err) {
      onError?.(err, index);
    }
  }
  return null;
}

/** Accepts the pre-list single object an existing install still has on disk. */
export function restoreCachedCreds(stored: unknown): HubShadowsocksCreds[] {
  if (!stored) return [];
  const list = Array.isArray(stored) ? stored : [stored];
  const out: HubShadowsocksCreds[] = [];
  for (const candidate of list) {
    if (!isHubShadowsocksCreds(candidate)) continue;
    if (out.some((c) => sameHubShadowsocks(c, candidate))) continue;
    out.push({ ...candidate });
  }
  return out;
}
