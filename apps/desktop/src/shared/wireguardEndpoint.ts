/**
 * The node's own WireGuard listener, for the direct connection method — the one
 * that runs no transport and dials the node itself.
 *
 * IP literals only. The host has to be subtracted from AllowedIPs by address
 * (that arithmetic produces garbage CIDRs for a hostname), and resolving a node
 * domain here would leak which node is about to be used and cannot be answered
 * behind a Lockdown lock at all — the same reason no transport resolves its own.
 */
export interface NodeWireGuardEndpoint {
  /** `host:port`, ready for a WireGuard config's Endpoint line. */
  endpoint: string;
  /** The address on its own, for the AllowedIPs and kill-switch exclusions. */
  host: string;
}

const IPV4_LITERAL = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
const PORT = /^\d{1,5}$/;

// Local copy: shared/ must not import from main/. Returns the normalized
// dotted form so an ambiguous literal (e.g. a leading zero) never flows
// through verbatim — Go's net.ParseIP would reject it as an IP outright.
function parseIPv4Literal(host: string): string | null {
  const match = IPV4_LITERAL.exec(host);
  if (match === null) return null;
  const octets = match.slice(1, 5);
  const valid = octets.every(
    (octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255)
  );
  return valid ? octets.map(Number).join(".") : null;
}

/**
 * @param value the `host:port` the hub reported for this node's WireGuard
 * listener, or anything else — null means the direct method is unavailable for
 * this profile, which is better than shipping an endpoint that cannot work.
 */
export function parseNodeWireGuardEndpoint(value: unknown): NodeWireGuardEndpoint | null {
  if (typeof value !== "string") return null;
  const endpoint = value.trim();
  const separator = endpoint.lastIndexOf(":");
  if (separator <= 0) return null;

  const host = parseIPv4Literal(endpoint.slice(0, separator));
  const portText = endpoint.slice(separator + 1);
  if (host === null || !PORT.test(portText)) return null;
  const port = Number(portText);
  if (port <= 0 || port > 65535) return null;

  return { endpoint: `${host}:${port}`, host };
}
