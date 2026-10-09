import { isIpLiteral } from "./ipLiteral.ts";

/** Splits the naive endpoint into the address to dial and the TLS name. The
 *  engine MAPs serverName to remoteHost, so remoteHost must be an address, not a domain. */
export interface NaiveEndpointInput {
  remoteHost: string;
  remoteIp?: string;
  serverName?: string;
}

export interface NaiveEndpoint {
  /** What the engine connects to — an address, not a name to look up. */
  remoteHost: string;
  /** The SNI/certificate name, kept as the node's naive domain. */
  serverName: string;
}

function firstNonBlank(...values: (string | undefined)[]): string {
  for (const value of values) {
    const trimmed = value?.trim();
    if (trimmed) return trimmed;
  }
  return "";
}

/** nodeIp is the node address the hub already named (cloak.remoteHost). */
export function resolveNaiveEndpoint(naive: NaiveEndpointInput, nodeIp: string): NaiveEndpoint {
  const host = firstNonBlank(naive.remoteHost);
  // nodeIp is the last resort so serverName is never blank even when the hub
  // names neither a domain nor a per-transport IP.
  const serverName = firstNonBlank(naive.serverName, host, nodeIp);
  const perTransportIp = firstNonBlank(naive.remoteIp);

  // remoteIp first, same precedence as Reality/Hysteria2, but only if it's an address.
  if (perTransportIp && isIpLiteral(perTransportIp)) {
    return { remoteHost: perTransportIp, serverName };
  }

  // Already an address — dial it as given rather than substituting the node.
  if (isIpLiteral(host)) {
    return { remoteHost: host, serverName };
  }

  // Otherwise the shared node; hostname only as a last resort.
  return { remoteHost: firstNonBlank(nodeIp, host), serverName };
}
