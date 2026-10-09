/** Hostnames of edge workers that relay the secure envelope to the hub. Only
 *  the host is stored — the relay always answers on 443 at /v1/secure. */

/** Shipped relays. Without them an install that has never reached the hub has
 *  no relay at all; the hub's list replaces them once one arrives. */
export const DEFAULT_FRONTED_ENDPOINTS: readonly string[] = [
  "cdn.pangeavpn.it",
  "pangea-relay-org.purple-field-fb05.workers.dev",
  "pangea-relay-alt.purple-field-fb05.workers.dev"
];

const MAX_HOSTNAME_LENGTH = 253;
const LABEL = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/;
// All-numeric labels also match LABEL, so a dotted-quad IP would otherwise
// pass as four valid labels; reject it explicitly.
const IPV4_LIKE = /^\d{1,3}(\.\d{1,3}){3}$/;

/** A public DNS name, lowercased. Rejects anything without a dot so a hostile
 *  LAN's search domain cannot pass a bare label off as the relay. */
export function normalizeFrontedEndpoint(value: unknown): string | null {
  if (typeof value !== "string") return null;
  const host = value.trim().toLowerCase();
  if (host.length === 0 || host.length > MAX_HOSTNAME_LENGTH) return null;
  if (IPV4_LIKE.test(host)) return null;
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

/** The stored list, or the shipped relays when nothing usable is stored. */
export function seedFrontedEndpoints(stored: unknown): string[] {
  const restored = restoreFrontedEndpoints(stored);
  return restored.length > 0 ? restored : [...DEFAULT_FRONTED_ENDPOINTS];
}

/** Every relay the hub named, or null when nothing changed or the advertisement
 *  is empty/invalid — never treated as an instruction to discard what still works. */
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
