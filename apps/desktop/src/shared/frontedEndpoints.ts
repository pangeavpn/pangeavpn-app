/** Hostnames of edge workers that relay the secure envelope to the hub, out of
 *  pangeaApiClient so the cache rules are testable without electron.
 *
 *  Only the host is stored. The relay always answers on 443 at /v1/secure, and
 *  accepting a scheme, port or path from disk or from the hub would let a
 *  hand-edited file or a compromised response aim the client somewhere the
 *  validation below cannot reason about. */

const MAX_HOSTNAME_LENGTH = 253;
const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;

/**
 * A public DNS name, lowercased. Rejects anything without a dot: the relay is
 * always a real registered host, never a bare label an attacker on a hostile
 * LAN could claim through a local search domain.
 */
export function normalizeFrontedEndpoint(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const host = value.trim().toLowerCase();
  if (host.length === 0 || host.length > MAX_HOSTNAME_LENGTH) return null;
  const labels = host.split(".");
  if (labels.length < 2) return null;
  if (!labels.every((label) => LABEL.test(label))) return null;
  return host;
}

export function isFrontedEndpoint(value: unknown): boolean {
  return normalizeFrontedEndpoint(value) !== null;
}

/** Validated and deduplicated, order preserved. */
export function restoreFrontedEndpoints(stored: unknown): string[] {
  if (!stored) return [];
  const list = Array.isArray(stored) ? stored : [stored];
  const out: string[] = [];
  for (const candidate of list) {
    const host = normalizeFrontedEndpoint(candidate);
    if (host && !out.includes(host)) out.push(host);
  }
  return out;
}

/**
 * Every relay the hub named. Returns null when nothing changed, so callers can
 * skip a disk write. An empty or entirely invalid advertisement leaves the
 * cache alone: a hub that has stopped naming relays is far more likely to be a
 * rollback or a truncated response than an instruction to discard the only
 * addresses that still work when everything else is blocked.
 */
export function mergeFrontedEndpoints(
  current: readonly string[],
  advertised: unknown
): string[] | null {
  const next = restoreFrontedEndpoints(advertised);
  if (next.length === 0) return null;

  // Keep the relay that last worked in front when the hub still lists it, so a
  // refresh does not undo promoteFrontedEndpoint.
  const leader = current[0];
  if (leader) {
    const at = next.indexOf(leader);
    if (at > 0) next.unshift(next.splice(at, 1)[0]);
  }

  const unchanged = next.length === current.length && next.every((h, i) => h === current[i]);
  return unchanged ? null : next;
}

/** Moves the relay that just worked to the front, so the next start skips the dead ones. */
export function promoteFrontedEndpoint(list: readonly string[], index: number): string[] | null {
  if (index <= 0 || index >= list.length) return null;
  const next = list.slice();
  next.unshift(next.splice(index, 1)[0]);
  return next;
}
