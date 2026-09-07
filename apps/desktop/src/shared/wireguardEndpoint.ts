// The node's own WireGuard listener for the direct method. IP literals only:
// the host is subtracted from AllowedIPs by address, and a lookup leaks the node and dies under lockdown.
export interface NodeWireGuardEndpoint {
  /** `host:port`, ready for a WireGuard config's Endpoint line. */
  endpoint: string;
  /** The address on its own, for the AllowedIPs and kill-switch exclusions. */
  host: string;
}

const IPV4_LITERAL = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;
const PORT = /^\d{1,5}$/;

// Returns the normalized dotted form so an ambiguous literal (leading zero)
// never flows through verbatim — Go's net.ParseIP would reject it outright.
function parseIPv4Literal(host: string): string | null {
  const match = IPV4_LITERAL.exec(host);
  if (match === null) return null;
  const octets = match.slice(1, 5);
  const valid = octets.every(
    (octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255)
  );
  return valid ? octets.map(Number).join(".") : null;
}

// null (direct method unavailable) for anything but a valid host:port literal,
// which beats shipping an endpoint that cannot work.
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

// null when the registration carries a hop: the reported endpoint is then the
// exit, which the client never dials — keep it out of AllowedIPs, the kill switch, and direct mode.
export function nodeWireGuardEndpointForRegistration(
  serverEndpoint: unknown,
  hasHop: boolean
): NodeWireGuardEndpoint | null {
  return hasHop ? null : parseNodeWireGuardEndpoint(serverEndpoint);
}
