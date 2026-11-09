import { isIpLiteral } from "./ipLiteral.ts";

/** Daemon-facing TUIC profile block; same dial and SNI rules as anytlsProfile.ts. */
export interface TUICServerInfo {
  remoteHost: string;
  remoteIp?: string;
  remotePort: number;
  uuid: string;
  password: string;
  serverName?: string;
  pinSha256?: string;
  targetHost?: string;
  targetPort?: number;
}

export interface TUICProfileBlock {
  localPort: number;
  remoteHost: string;
  remotePort: number;
  uuid: string;
  password: string;
  serverName?: string;
  pinSha256?: string;
  targetHost?: string;
  targetPort?: number;
}

/** nodeIp is the node address the hub already named (cloak.remoteHost). */
export function buildTUICProfile(tuic: TUICServerInfo, nodeIp: string): TUICProfileBlock {
  const remoteIp = tuic.remoteIp?.trim() ?? "";
  const hubHost = tuic.remoteHost?.trim() ?? "";
  const remoteHost = isIpLiteral(remoteIp) ? remoteIp : isIpLiteral(hubHost) ? hubHost : nodeIp;
  const serverName = tuic.serverName?.trim() || (isIpLiteral(hubHost) ? "" : hubHost);
  return {
    localPort: 0,
    remoteHost,
    remotePort: tuic.remotePort,
    uuid: tuic.uuid,
    password: tuic.password,
    ...(serverName ? { serverName } : {}),
    ...(tuic.pinSha256 ? { pinSha256: tuic.pinSha256 } : {}),
    ...(tuic.targetHost ? { targetHost: tuic.targetHost } : {}),
    ...(tuic.targetPort ? { targetPort: tuic.targetPort } : {})
  };
}
