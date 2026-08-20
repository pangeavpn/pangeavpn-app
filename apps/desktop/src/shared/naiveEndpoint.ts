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
  if (match === null) return false;
  // Go's net.ParseIP rejects leading zeros; a literal it would reject must not
  // be dialed verbatim here either.
  return match.slice(1, 5).every((octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255));
}

// Deliberately permissive (accepts some invalid IPv6): a false positive just
// means we dial verbatim instead of substituting the node, which is safe.
function isIPv6Literal(host: string): boolean {
  const bare = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  if (!bare.includes(":")) return false;
  const halves = bare.split("::");
  if (halves.length > 2) return false;
  const groups = halves.flatMap((half) => (half.length ? half.split(":") : []));
  if (halves.length === 1 && groups.length !== 8) return false;
  if (halves.length === 2 && groups.length >= 8) return false;
  return groups.every((group) => /^[0-9a-fA-F]{1,4}$/.test(group));
}

function isIpLiteral(host: string): boolean {
  return isIPv4Literal(host) || isIPv6Literal(host);
}

/**
 * @param naive the hub's naive block for this node
 * @param nodeIp the node address the hub already named (cloak.remoteHost)
 */
export function resolveNaiveEndpoint(naive: NaiveEndpointInput, nodeIp: string): NaiveEndpoint {
  const host = firstNonBlank(naive.remoteHost);
  // nodeIp is the last resort so serverName is never blank even when the hub
  // names neither a domain nor a per-transport IP.
  const serverName = firstNonBlank(naive.serverName, host, nodeIp);
  const perTransportIp = firstNonBlank(naive.remoteIp);

  // remoteIp first, the same precedence Reality and Hysteria2 use — the hub
  // naming an address for naive specifically always wins. It must still be an
  // address: junk here must not become the dial target.
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
