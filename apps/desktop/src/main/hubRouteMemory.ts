import { createHash } from "node:crypto";
import type { NetworkInterfaceInfo } from "node:os";
import { enabledHubMethods, isHubMethod, type HubMethod, type HubMethods } from "../shared/hubMethods.ts";

/** How long a remembered route jumps the queue before the whole ladder is walked
 *  again, so a network that has opened up is not held to a slower route for good. */
export const HUB_ROUTE_TTL_MS = 7 * 24 * 60 * 60 * 1000;
const MAX_NETWORKS = 32;

// Adapters that are not the network the machine sits on: our own tunnel, other VPNs, VM switches.
const VIRTUAL_PREFIXES = [
  "tun", "utun", "wg", "pangea", "tailscale", "ppp", "ipsec", "docker", "veth", "br-", "vmnet", "vboxnet"
];

export interface HubRouteRecord {
  method: HubMethod;
  /** When the full ladder last settled on this method. */
  at: number;
}

export type HubRoutes = Readonly<Record<string, HubRouteRecord>>;

/** Fingerprints the networks this machine is on, or "" when it is on none.
 *  Subnets rather than addresses, so a new lease is still the same network. */
export function networkKeyOf(interfaces: NodeJS.Dict<NetworkInterfaceInfo[]>): string {
  const parts: string[] = [];
  for (const [name, addrs] of Object.entries(interfaces)) {
    const lower = name.toLowerCase();
    if (VIRTUAL_PREFIXES.some((prefix) => lower.startsWith(prefix))) continue;
    for (const addr of addrs ?? []) {
      if (addr.internal || addr.family !== "IPv4" || addr.address.startsWith("169.254.")) continue;
      const mask = addr.netmask.split(".").map(Number);
      const subnet = addr.address.split(".").map((octet, i) => Number(octet) & mask[i]);
      parts.push(`${name}:${subnet.join(".")}/${addr.netmask}`);
    }
  }
  if (parts.length === 0) return "";
  return createHash("sha256").update(parts.sort().join("|")).digest("hex").slice(0, 16);
}

export function restoreHubRoutes(stored: unknown): HubRoutes {
  if (typeof stored !== "object" || stored === null || Array.isArray(stored)) return {};
  const routes: Record<string, HubRouteRecord> = {};
  for (const [network, value] of Object.entries(stored as Record<string, unknown>)) {
    const { method, at } = (value ?? {}) as Record<string, unknown>;
    if (isHubMethod(method) && typeof at === "number" && Number.isFinite(at)) routes[network] = { method, at };
  }
  return routes;
}

/** The method that last won on this network, while that is still fresh. */
export function rememberedHubRoute(routes: HubRoutes, network: string, now: number): HubMethod | null {
  const record = network ? routes[network] : undefined;
  if (!record) return null;
  const age = now - record.at;
  return age >= 0 && age < HUB_ROUTE_TTL_MS ? record.method : null;
}

/** Records the ladder's winner for this network, dropping the oldest networks past the cap. */
export function recordHubRoute(routes: HubRoutes, network: string, method: HubMethod, now: number): HubRoutes {
  if (!network) return routes;
  const entries = Object.entries({ ...routes, [network]: { method, at: now } });
  return Object.fromEntries(entries.sort(([, a], [, b]) => b.at - a.at).slice(0, MAX_NETWORKS));
}

/** The enabled methods in ladder order, with the remembered one moved to the front. */
export function hubMethodAttemptOrder(methods: HubMethods, remembered: HubMethod | null): HubMethod[] {
  const enabled = enabledHubMethods(methods);
  if (!remembered || !enabled.includes(remembered)) return enabled;
  return [remembered, ...enabled.filter((method) => method !== remembered)];
}
