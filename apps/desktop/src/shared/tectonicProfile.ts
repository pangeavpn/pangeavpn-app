import { isIPv6Literal, isIpLiteral } from "./ipLiteral.ts";

/** The hub's Tectonic block from /api/register, in Tectonic's own snake_case
 *  JSON; byte fields are base64. Per-device and expiring, so never cached. */
export interface TectonicHubBlock {
  node: string;
  cover_domain?: string;
  key_id: string;
  device_key: string;
  pins?: string[];
  borrowed_sni?: string[];
  cdn?: { host: string; path: string; edge?: string };
  shaping?: unknown[];
}

export interface TectonicProfileBlock {
  localPort: number;
  node: string;
  coverDomain?: string;
  keyId: string;
  deviceKey: string;
  pins?: string[];
  borrowedSni?: string[];
  cdn?: { host: string; path: string; edge?: string };
  shaping?: unknown[];
  sniMode?: string;
  sni?: string;
}

const DEFAULT_PORT = 443;

// Splits "host", "host:port", "[v6]" or "[v6]:port"; a bare v6 has no port.
function splitNode(node: string): { host: string; port?: number } {
  const bracketed = /^\[([^\]]+)\](?::(\d+))?$/.exec(node);
  if (bracketed) return { host: bracketed[1], port: parsePort(bracketed[2]) };
  if (isIPv6Literal(node)) return { host: node };
  const colon = node.lastIndexOf(":");
  if (colon < 0) return { host: node };
  return { host: node.slice(0, colon), port: parsePort(node.slice(colon + 1)) };
}

function parsePort(raw: string | undefined): number | undefined {
  if (!raw || !/^\d+$/.test(raw)) return undefined;
  const port = Number(raw);
  return port >= 1 && port <= 65535 ? port : undefined;
}

function joinHostPort(host: string, port: number): string {
  return isIPv6Literal(host) ? `[${host}]:${port}` : `${host}:${port}`;
}

/** nodeIp is the node address the hub already named (cloak.remoteHost). */
export function buildTectonicProfile(
  tectonic: TectonicHubBlock,
  nodeIp: string
): TectonicProfileBlock | undefined {
  const keyId = tectonic.key_id?.trim() ?? "";
  const deviceKey = tectonic.device_key?.trim() ?? "";
  if (!keyId || !deviceKey) return undefined;
  // Dial an IP, never a domain: the daemon refuses a hostname, which would
  // otherwise resolve outside the tunnel.
  const hub = splitNode(tectonic.node?.trim() ?? "");
  const fallback = nodeIp.trim();
  const host = isIpLiteral(hub.host) ? hub.host : isIpLiteral(fallback) ? fallback : "";
  if (!host) return undefined;
  const cdnHost = tectonic.cdn?.host?.trim() ?? "";
  const cdnEdge = tectonic.cdn?.edge?.trim() ?? "";
  const coverDomain = tectonic.cover_domain?.trim() ?? "";
  return {
    localPort: 0,
    node: joinHostPort(host, hub.port ?? DEFAULT_PORT),
    ...(coverDomain ? { coverDomain } : {}),
    keyId,
    deviceKey,
    ...(tectonic.pins?.length ? { pins: tectonic.pins } : {}),
    ...(tectonic.borrowed_sni?.length ? { borrowedSni: tectonic.borrowed_sni } : {}),
    ...(cdnHost
      ? { cdn: { host: cdnHost, path: tectonic.cdn?.path ?? "", ...(cdnEdge ? { edge: cdnEdge } : {}) } }
      : {}),
    // Left to the daemon as it came; sniMode/sni stay unset so Tectonic picks.
    ...(Array.isArray(tectonic.shaping) && tectonic.shaping.length ? { shaping: tectonic.shaping } : {})
  };
}

/** The CDN edge's host without its port; a name passes through for callers to drop. */
export function tectonicCdnEdgeIp(block: TectonicProfileBlock | undefined): string | undefined {
  const edge = block?.cdn?.edge;
  if (!edge) return undefined;
  return splitNode(edge).host;
}

/** The node's host without its port or brackets. */
export function tectonicNodeHost(block: TectonicProfileBlock | undefined): string | undefined {
  return block ? splitNode(block.node).host : undefined;
}
