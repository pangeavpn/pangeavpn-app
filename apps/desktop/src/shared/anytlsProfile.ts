import { isIpLiteral } from "./ipLiteral.ts";

/** Daemon-facing AnyTLS profile block, out of pangeaApiClient so it is
 *  testable without electron. Mirrors shadowsocksProfile.ts. */
export interface AnyTLSServerInfo {
  remoteHost: string;
  remoteIp?: string;
  remotePort: number;
  password: string;
  serverName?: string;
  insecure?: boolean;
  pinSha256?: string;
  targetHost?: string;
  targetPort?: number;
}

export interface AnyTLSProfileBlock {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  password: string;
  serverName?: string;
  insecure?: boolean;
  pinSha256?: string;
  targetHost?: string;
  targetPort?: number;
}

/** nodeIp is the node address the hub already named (cloak.remoteHost). */
export function buildAnyTLSProfile(
  anytls: AnyTLSServerInfo,
  nodeIp: string
): AnyTLSProfileBlock {
  // Dial an IP, never a domain: a lookup leaks the node to a third-party
  // resolver and is impossible behind an engaged Lockdown lock.
  const remoteIp = anytls.remoteIp?.trim() ?? "";
  const hubHost = anytls.remoteHost?.trim() ?? "";
  const remoteHost = isIpLiteral(remoteIp) ? remoteIp : isIpLiteral(hubHost) ? hubHost : nodeIp;
  // Same fallback as Hysteria2; an address is never sent as the SNI.
  const serverName = anytls.serverName?.trim() || (isIpLiteral(hubHost) ? "" : hubHost);
  return {
    localPort: 0,
    remoteHost,
    remotePort: anytls.remotePort,
    password: anytls.password,
    ...(serverName ? { serverName } : {}),
    ...(anytls.insecure ? { insecure: true } : {}),
    ...(anytls.pinSha256 ? { pinSha256: anytls.pinSha256 } : {}),
    // targetHost/targetPort stay absent unless the hub named them, so the
    // daemon applies its own 127.0.0.1:51820 default.
    ...(anytls.targetHost ? { targetHost: anytls.targetHost } : {}),
    ...(anytls.targetPort ? { targetPort: anytls.targetPort } : {})
  };
}
