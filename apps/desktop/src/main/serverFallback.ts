export interface ServerFallbackResult<T> {
  serverId: string;
  value: T;
}

type RetryServer = { id: string; load?: number | null };
type Identified = { id: string };

const regionKeyOf = (serverId: string): string => serverId.replace(/-(\d+)$/, "");
const loadOf = (server: RetryServer): number =>
  typeof server.load === "number" && Number.isFinite(server.load) ? server.load : 100;

/** Main-process equivalent of the renderer plan, used by tray connects. */
export function buildServerRetryOrder(servers: readonly RetryServer[], initialServerId: string): string[] {
  const regionKeys: string[] = [];
  const nodesByRegion = new Map<string, RetryServer[]>();
  for (const server of servers) {
    const key = regionKeyOf(server.id);
    if (!nodesByRegion.has(key)) regionKeys.push(key);
    nodesByRegion.set(key, [...(nodesByRegion.get(key) ?? []), server]);
  }
  const initialRegion = regionKeyOf(initialServerId);
  const initialRegionIndex = regionKeys.indexOf(initialRegion);
  if (initialRegionIndex < 0) return [initialServerId];

  const orderedRegions = [
    initialRegion,
    ...regionKeys.slice(initialRegionIndex + 1),
    ...regionKeys.slice(0, initialRegionIndex)
  ];
  return orderedRegions.flatMap((key, regionIndex) => {
    const nodes = [...(nodesByRegion.get(key) ?? [])].sort((a, b) => loadOf(a) - loadOf(b));
    if (regionIndex !== 0) return nodes.map((node) => node.id);
    return [initialServerId, ...nodes.filter((node) => node.id !== initialServerId).map((node) => node.id)];
  });
}

export function replaceManagedProfile<T extends Identified>(
  profiles: readonly T[],
  previousProfileId: string | null,
  winner: T
): T[] {
  return [
    ...profiles.filter((profile) => profile.id !== previousProfileId && profile.id !== winner.id),
    winner
  ];
}

/** Runs a finite server plan, advancing only for failures the caller classifies as retryable. */
export async function runServerFallback<T>(
  serverIds: readonly string[],
  attempt: (serverId: string, index: number) => Promise<T>,
  isRetryable: (error: unknown) => boolean
): Promise<ServerFallbackResult<T>> {
  if (serverIds.length === 0) {
    throw new Error("A connection attempt requires at least one server");
  }

  for (let index = 0; index < serverIds.length; index += 1) {
    const serverId = serverIds[index];
    try {
      return { serverId, value: await attempt(serverId, index) };
    } catch (error) {
      if (!isRetryable(error) || index === serverIds.length - 1) {
        throw error;
      }
    }
  }

  throw new Error("Server retry plan ended unexpectedly");
}
