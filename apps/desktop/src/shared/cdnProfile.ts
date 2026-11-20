import { isIpLiteral } from "./ipLiteral.ts";

/** Daemon-facing CDN profile block. remoteHost is the CDN-proxied hostname and
 *  remoteIp an edge address; the node's own address is what this transport hides. */
export interface CDNServerInfo {
  remoteHost: string;
  remoteIp?: string;
  remotePort?: number;
  uuid: string;
  path?: string;
  serverName?: string;
  targetHost?: string;
  targetPort?: number;
  /** The node's VMess-over-WebSocket route on the same hostname (the cdnws transport);
   *  its edge port defaults to 443. */
  vmess?: { uuid: string; path?: string; remotePort?: number };
}

/** The same block feeds both CDN transports: `cdn` (VLESS) and `cdnws` (VMess). */
export interface CDNProfileBlock {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  uuid: string;
  serverName: string;
  path: string;
  targetHost?: string;
  targetPort?: number;
}

/** VLESS over HTTPUpgrade through the edge: the `cdn` transport. */
export function buildCDNProfile(cdn: CDNServerInfo): CDNProfileBlock {
  return edgeProfile(cdn, cdn.uuid, cdn.path, cdn.remotePort);
}

/** VMess over a WebSocket through the same edge: the `cdnws` transport. Undefined when
 *  the node serves only VLESS there. */
export function buildCDNWSProfile(cdn: CDNServerInfo): CDNProfileBlock | undefined {
  const vmess = cdn.vmess;
  if (!vmess?.uuid) {
    return undefined;
  }
  return edgeProfile(cdn, vmess.uuid, vmess.path, vmess.remotePort);
}

function edgeProfile(cdn: CDNServerInfo, uuid: string, path: string | undefined, remotePort: number | undefined): CDNProfileBlock {
  const remoteIp = cdn.remoteIp?.trim() ?? "";
  const hubHost = cdn.remoteHost?.trim() ?? "";
  // Never falls back to the node IP: a hostname dial goes to the CDN, not the node.
  const remoteHost = isIpLiteral(remoteIp) ? remoteIp : hubHost;
  const serverName = cdn.serverName?.trim() || (isIpLiteral(hubHost) ? "" : hubHost);
  return {
    localPort: 0,
    remoteHost,
    remotePort: remotePort || 443,
    uuid,
    serverName,
    path: path?.trim() || "/",
    ...(cdn.targetHost ? { targetHost: cdn.targetHost } : {}),
    ...(cdn.targetPort ? { targetPort: cdn.targetPort } : {})
  };
}
