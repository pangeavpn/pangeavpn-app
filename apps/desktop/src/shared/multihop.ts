// Multihop preferences and entry selection for main. The renderer keeps its
// own copy in renderer/multihop.ts, as regions.ts mirrors serverFallback.ts.

export interface MultihopPrefs {
  enabled: boolean;
  /** Entry chosen by hand; null lets the lightest qualifying entry win. */
  entryServerId: string | null;
}

export interface HopServer {
  id: string;
  load?: number | null;
  multihop?: boolean;
}

const SUFFIX = /-(\d+)$/;

/** `eu-west-1` -> `eu-west`, matching the renderer's region grouping. */
export const regionKeyOf = (serverId: string): string => serverId.replace(SUFFIX, "");

export function normalizeMultihopPrefs(value: unknown): MultihopPrefs {
  const v =
    value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
  const entry = typeof v.entryServerId === "string" ? v.entryServerId.trim() : "";
  return { enabled: v.enabled === true, entryServerId: entry ? entry : null };
}

/** IPC argument: absent or empty means single-hop; anything else must be an id. */
export function normalizeEntryServer(value: unknown): string | null {
  if (value === undefined || value === null || value === "") return null;
  if (typeof value !== "string" || value.trim() === "" || value.length > 128) {
    throw new Error("Invalid entry server");
  }
  return value.trim();
}

const loadOf = (server: HopServer): number =>
  typeof server.load === "number" && Number.isFinite(server.load) ? server.load : 100;

/** Entry-capable servers outside the exit's region, lightest first, hub order on ties. */
export function entryCandidates<T extends HopServer>(servers: readonly T[], exitServerId: string): T[] {
  const exitRegion = regionKeyOf(exitServerId);
  return servers
    .filter((server) => server.multihop === true && regionKeyOf(server.id) !== exitRegion)
    .sort((a, b) => loadOf(a) - loadOf(b));
}

/** The chosen entry while it still qualifies, else the lightest candidate, else null. */
export function resolveEntry<T extends HopServer>(
  servers: readonly T[],
  exitServerId: string,
  chosenEntryId: string | null
): T | null {
  const candidates = entryCandidates(servers, exitServerId);
  return (chosenEntryId && candidates.find((server) => server.id === chosenEntryId)) || candidates[0] || null;
}
