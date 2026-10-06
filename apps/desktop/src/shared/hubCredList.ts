/** Cache rules shared by every control-plane proxy method: one list of nodes,
 *  refreshed from the hub, reordered by what last worked, seeded when empty. */
export interface CredKind<C> {
  isValid(value: unknown): value is C;
  /** Trims a validated candidate, so padding from the hub never reaches the cache. */
  normalize(candidate: C): C;
  same(a: C, b: C): boolean;
}

/** Every node the hub named, deduplicated, or null when nothing changed. Caches
 *  every one, since one node's credentials would strand the client on rotation. */
export function mergeAdvertised<C>(kind: CredKind<C>, current: readonly C[], advertised: unknown[]): C[] | null {
  const next = restoreCached(kind, advertised);
  if (next.length === 0) return null;

  // Keep the node that last worked in front when the hub still lists it, so a
  // refresh does not undo promoteEntry.
  const leader = current[0];
  if (leader) {
    const at = next.findIndex((c) => kind.same(c, leader));
    if (at > 0) next.unshift(next.splice(at, 1)[0]);
  }

  const unchanged = next.length === current.length && next.every((c, i) => kind.same(c, current[i]));
  return unchanged ? null : next;
}

/** Moves the entry that just worked to the front, so the next start skips the dead ones. */
export function promoteEntry<C>(list: readonly C[], index: number): C[] | null {
  if (index <= 0 || index >= list.length) return null;
  const next = list.slice();
  next.unshift(next.splice(index, 1)[0]);
  return next;
}

/** Tries every cached node until one answers, reporting which index won. A node
 *  that throws or has a rotated key must not end the search. */
export async function firstWorking<C, T>(
  list: readonly C[],
  attempt: (creds: C, index: number) => Promise<T | null>,
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

/** Valid entries, normalized and deduplicated. Accepts a bare object too, the
 *  pre-list shape an existing install may still have on disk. */
export function restoreCached<C>(kind: CredKind<C>, stored: unknown): C[] {
  if (!stored) return [];
  const list = Array.isArray(stored) ? stored : [stored];
  const out: C[] = [];
  for (const candidate of list) {
    if (!kind.isValid(candidate)) continue;
    const normalized = kind.normalize(candidate);
    if (out.some((c) => kind.same(c, normalized))) continue;
    out.push(normalized);
  }
  return out;
}

/** The stored list, or copies of the shipped nodes when nothing usable is stored. */
export function seedCached<C extends object>(kind: CredKind<C>, stored: unknown, defaults: readonly Readonly<C>[]): C[] {
  const restored = restoreCached(kind, stored);
  return restored.length > 0 ? restored : defaults.map((c) => ({ ...c }) as C);
}
