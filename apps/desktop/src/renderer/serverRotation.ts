// "Switch to another server": walks a retry order while remembering where this
// cycle has been, so repeat presses never land on the same server twice.

export interface RotationPlan {
  /** Servers to try in order: the fresh ones, then this cycle's visited as a fallback. */
  plan: string[];
  /** Visited list to carry forward: cleared when every alternative has had its turn. */
  visited: string[];
}

/** `order` is the retry order from regions.ts: current server first, siblings, then later regions. */
export function planRotation(
  order: readonly string[],
  currentId: string,
  visited: readonly string[]
): RotationPlan {
  const alternatives = order.filter((id) => id !== currentId);
  const stillListed = visited.filter((id) => alternatives.includes(id));
  const fresh = alternatives.filter((id) => !stillListed.includes(id));
  if (fresh.length === 0) return { plan: alternatives, visited: [] };
  const fallback = alternatives.filter((id) => stillListed.includes(id));
  return { plan: [...fresh, ...fallback], visited: stillListed };
}

/** The server left, every attempt that failed, and the one that connected (or all on failure). */
export function recordRotation(
  visited: readonly string[],
  currentId: string,
  plan: readonly string[],
  landedId: string | null
): string[] {
  const landedAt = landedId ? plan.indexOf(landedId) : -1;
  const attempted = landedAt >= 0 ? plan.slice(0, landedAt + 1) : plan;
  return [...new Set([...visited, currentId, ...attempted])];
}
