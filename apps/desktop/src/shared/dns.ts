/** Ranges a resolver behind the tunnel can never sit in: unspecified, LAN,
 *  CGNAT, link-local, multicast and broadcast. Loopback stays: a local proxy works. */
const UNROUTABLE_DNS: readonly [string, number][] = [
  ["0.0.0.0", 8],
  ["10.0.0.0", 8],
  ["100.64.0.0", 10],
  ["169.254.0.0", 16],
  ["172.16.0.0", 12],
  ["192.168.0.0", 16],
  ["224.0.0.0", 3]
];

function ipv4ToInt(octets: readonly number[]): number {
  return ((octets[0] << 24) | (octets[1] << 16) | (octets[2] << 8) | octets[3]) >>> 0;
}

function unroutableDns(octets: readonly number[]): boolean {
  const ip = ipv4ToInt(octets);
  return UNROUTABLE_DNS.some(([prefix, bits]) => {
    const mask = (0xffffffff << (32 - bits)) >>> 0;
    return (ip & mask) >>> 0 === ipv4ToInt(prefix.split(".").map(Number));
  });
}

/** Empty means "use the VPN server default"; null means invalid input.
 *  The WireGuard daemon currently supports IPv4 DNS servers only. */
export function normalizeCustomDns(value: unknown): string[] | null {
  let values: unknown[];
  if (typeof value === "string") {
    const trimmed = value.trim();
    if (trimmed === "") return [];
    values = trimmed.split(/[\s,]+/);
  } else if (Array.isArray(value)) {
    values = value;
  } else {
    return null;
  }

  const normalized: string[] = [];
  for (const valuePart of values) {
    if (typeof valuePart !== "string") return null;
    const parts = valuePart.trim().split(".");
    if (parts.length !== 4 || parts.some((part) => !/^\d{1,3}$/.test(part))) {
      return null;
    }
    const octets = parts.map(Number);
    if (octets.some((octet) => octet > 255) || unroutableDns(octets)) return null;
    const address = octets.join(".");
    if (!normalized.includes(address)) normalized.push(address);
  }

  return normalized;
}

/** Resolve the values written to both the WireGuard config text and profile. */
export function resolveWireGuardDns(
  serverDns: string,
  customDns: readonly string[] | null
): { servers: string[]; configValue: string } {
  const servers =
    customDns && customDns.length > 0
      ? [...customDns]
      : serverDns.split(",").map((value) => value.trim()).filter(Boolean);
  return { servers, configValue: servers.join(", ") };
}
