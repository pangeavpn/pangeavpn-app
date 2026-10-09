// Entry selection for multihop. Mirrors shared/multihop.ts, which main uses
// for tray-driven reconnects; the renderer cannot import across tsconfigs.

const SUFFIX = /-(\d+)$/;

/** `eu-west-1` -> `eu-west`, the same grouping regions.ts uses. */
const regionKeyOf = (serverId: string): string => serverId.replace(SUFFIX, "");

const loadOf = (server: ServerInfo): number =>
  typeof server.load === "number" && Number.isFinite(server.load) ? server.load : 100;

/** Entry-capable servers outside the exit's region, lightest first, hub order on ties. */
export function entryCandidates(servers: readonly ServerInfo[], exitServerId: string): ServerInfo[] {
  const exitRegion = regionKeyOf(exitServerId);
  return servers
    .filter((server) => server.multihop === true && regionKeyOf(server.id) !== exitRegion)
    .sort((a, b) => loadOf(a) - loadOf(b));
}

/** The chosen entry while it still qualifies, else the lightest candidate, else null. */
export function resolveEntry(
  servers: readonly ServerInfo[],
  exitServerId: string,
  chosenEntryId: string | null
): ServerInfo | null {
  const candidates = entryCandidates(servers, exitServerId);
  return (chosenEntryId && candidates.find((server) => server.id === chosenEntryId)) || candidates[0] || null;
}
