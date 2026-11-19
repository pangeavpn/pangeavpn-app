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
  /** The node's VMess-over-WebSocket route on the same hostname; its edge port defaults to 443. */
  vmess?: { uuid: string; path?: string; remotePort?: number };
}

export interface CDNProfileBlock {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  protocol?: "vmess";
  uuid: string;
  serverName: string;
  path: string;
  targetHost?: string;
  targetPort?: number;
}

export function buildCDNProfile(cdn: CDNServerInfo): CDNProfileBlock {
  const remoteIp = cdn.remoteIp?.trim() ?? "";
  const hubHost = cdn.remoteHost?.trim() ?? "";
  // Never falls back to the node IP: a hostname dial goes to the CDN, not the node.
  const remoteHost = isIpLiteral(remoteIp) ? remoteIp : hubHost;
  const serverName = cdn.serverName?.trim() || (isIpLiteral(hubHost) ? "" : hubHost);
  // VMess leaves nothing readable past the upgrade, so it wins wherever the node serves it.
  const vmess = cdn.vmess?.uuid ? cdn.vmess : undefined;
  return {
    localPort: 0,
    remoteHost,
    remotePort: (vmess ? vmess.remotePort : cdn.remotePort) || 443,
    ...(vmess ? { protocol: "vmess" as const } : {}),
    uuid: vmess ? vmess.uuid : cdn.uuid,
    serverName,
    path: (vmess ? vmess.path : cdn.path)?.trim() || "/",
    ...(cdn.targetHost ? { targetHost: cdn.targetHost } : {}),
    ...(cdn.targetPort ? { targetPort: cdn.targetPort } : {})
  };
}
