/** Daemon-facing Shadowsocks profile block, out of pangeaApiClient so it is
 *  testable without electron. */
export interface ShadowsocksServerInfo {
  remoteHost: string;
  remoteIp?: string;
  remotePort: number;
  method: string;
  password: string;
  targetHost?: string;
  targetPort?: number;
  udpOverTcp?: boolean;
}

export interface ShadowsocksProfileBlock {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  method: string;
  password: string;
  targetHost?: string;
  targetPort?: number;
  udpOverTcp?: boolean;
}

/** Shape check for a block that arrives outside the validated server list. */
export function isShadowsocksServerInfo(value: unknown): value is ShadowsocksServerInfo {
  const s = value as Partial<ShadowsocksServerInfo> | null;
  const port = s?.remotePort;
  return (
    !!s &&
    typeof s === "object" &&
    typeof port === "number" &&
    Number.isInteger(port) &&
    port > 0 &&
    port <= 65535 &&
    typeof s.method === "string" &&
    s.method.length > 0 &&
    typeof s.password === "string" &&
    s.password.length > 0
  );
}

/** nodeIp is the node address the hub already named (cloak.remoteHost). */
export function buildShadowsocksProfile(
  shadowsocks: ShadowsocksServerInfo,
  nodeIp: string
): ShadowsocksProfileBlock {
  // Dial the IP, never a domain: a lookup leaks the node to a third-party
  // resolver and is impossible behind an engaged Lockdown lock.
  const remoteHost = shadowsocks.remoteIp?.trim() || nodeIp;
  return {
    localPort: 0,
    remoteHost,
    remotePort: shadowsocks.remotePort,
    method: shadowsocks.method,
    password: shadowsocks.password,
    // targetHost/targetPort stay absent unless the hub named them, so the
    // daemon applies its own 127.0.0.1:51820 default.
    ...(shadowsocks.targetHost ? { targetHost: shadowsocks.targetHost } : {}),
    ...(shadowsocks.targetPort ? { targetPort: shadowsocks.targetPort } : {}),
    ...(shadowsocks.udpOverTcp ? { udpOverTcp: true } : {})
  };
}
