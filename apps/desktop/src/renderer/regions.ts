/* A region groups nodes serving one location. Derived from the node id's
   numeric suffix until the hub sends an explicit region key. */

export interface Region {
  key: string;
  name: string;
  country: string;
  nodes: ServerInfo[];
}

const SUFFIX = /-(\d+)$/;

/** `eu-central-1` -> `eu-central`; ids without a numeric suffix stand alone. */
export function regionKeyOf(server: ServerInfo): string {
  return server.id.replace(SUFFIX, "");
}

/** Groups servers into regions, preserving the order they first appear in. */
export function groupRegions(servers: readonly ServerInfo[]): Region[] {
  const byKey = new Map<string, Region>();
  for (const server of servers) {
    const key = regionKeyOf(server);
    const existing = byKey.get(key);
    if (existing) {
      existing.nodes = [...existing.nodes, server];
      continue;
    }
    byKey.set(key, { key, name: server.name || key, country: server.country || "", nodes: [server] });
  }
  return [...byKey.values()];
}

/** Lowest-load node in the region. Nodes with no load report sort last. */
export function pickNode(region: Region): ServerInfo {
  return region.nodes.reduce((best, node) => (loadOf(node) < loadOf(best) ? node : best), region.nodes[0]);
}

/** Missing load is treated as fully loaded so a reporting node always wins. */
export function loadOf(server: ServerInfo): number {
  return typeof server.load === "number" && Number.isFinite(server.load) ? server.load : 100;
}

/** The load actually offered by a region — that of the node we would pick. */
export function regionLoad(region: Region): number | null {
  const node = pickNode(region);
  return typeof node.load === "number" && Number.isFinite(node.load) ? node.load : null;
}

export function findRegion(regions: readonly Region[], key: string): Region | undefined {
  return regions.find((region) => region.key === key);
}

export function regionOfServer(regions: readonly Region[], serverId: string): Region | undefined {
  return regions.find((region) => region.nodes.some((node) => node.id === serverId));
}

/** Selected node, then its siblings by load, then every later region in hub order. */
export function buildServerRetryOrder(servers: readonly ServerInfo[], initialServerId: string): string[] {
  const regions = groupRegions(servers);
  const initialRegion = regionOfServer(regions, initialServerId);
  if (!initialRegion) return [initialServerId];

  const byLoad = (nodes: readonly ServerInfo[]): ServerInfo[] =>
    [...nodes].sort((a, b) => loadOf(a) - loadOf(b));
  const sameRegion = byLoad(initialRegion.nodes)
    .filter((node) => node.id !== initialServerId)
    .map((node) => node.id);
  const initialRegionIndex = regions.indexOf(initialRegion);
  const laterRegions = [...regions.slice(initialRegionIndex + 1), ...regions.slice(0, initialRegionIndex)]
    .flatMap((region) => byLoad(region.nodes).map((node) => node.id));

  return [initialServerId, ...sameRegion, ...laterRegions];
}

/** Most-recent-first, then everything else in hub order. */
export function orderByRecent(regions: readonly Region[], recent: readonly string[]): Region[] {
  const ranked = recent
    .map((key) => regions.find((region) => region.key === key))
    .filter((region): region is Region => region !== undefined);
  const rest = regions.filter((region) => !ranked.includes(region));
  return [...ranked, ...rest];
}

/** Moves `key` to the front, dropping any earlier entry and capping the list. */
export function promoteRecent(recent: readonly string[], key: string, limit = 8): string[] {
  return [key, ...recent.filter((entry) => entry !== key)].slice(0, limit);
}
