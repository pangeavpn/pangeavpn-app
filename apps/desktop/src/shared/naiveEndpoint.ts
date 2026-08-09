/**
 * Splits the naive endpoint into the address to dial and the TLS name.
 *
 * The engine builds its `--proxy` URL from `serverName` and, when that differs
 * from `remoteHost`, installs a `MAP <serverName> <remoteHost>` resolver rule,
 * so the dial needs no DNS. Passing the node domain as `remoteHost` collapses
 * both, drops the MAP rule, and forces a lookup the kill switch blocks.
 */
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

const IPV4_LITERAL = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

// Local copy: shared/ must not import from main/.
function isIPv4Literal(host: string): boolean {
  const match = IPV4_LITERAL.exec(host);
  return match !== null && match.slice(1, 5).every((octet) => Number(octet) <= 255);
}

/**
 * @param naive the hub's naive block for this node
 * @param nodeIp the node address the hub already named (cloak.remoteHost)
 */
export function resolveNaiveEndpoint(naive: NaiveEndpointInput, nodeIp: string): NaiveEndpoint {
  const host = firstNonBlank(naive.remoteHost);
  const serverName = firstNonBlank(naive.serverName, host);
  const perTransportIp = firstNonBlank(naive.remoteIp);

  // remoteIp first, the same precedence Reality and Hysteria2 use — the hub
  // naming an address for naive specifically always wins.
  if (perTransportIp) {
    return { remoteHost: perTransportIp, serverName };
  }

  // Already an address — dial it as given rather than substituting the node.
  if (isIPv4Literal(host)) {
    return { remoteHost: host, serverName };
  }

  // Otherwise the shared node; hostname only as a last resort.
  return { remoteHost: firstNonBlank(nodeIp, host), serverName };
}
