/**
 * Parse custom DNS servers from the settings UI or settings.json.
 * An empty value means "use the VPN server default"; null means invalid input.
 * The WireGuard daemon currently supports IPv4 DNS servers only.
 */
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
    if (octets.some((octet) => octet > 255)) return null;
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
  const servers = customDns
    ? [...customDns]
    : serverDns.split(",").map((value) => value.trim()).filter(Boolean);
  return { servers, configValue: servers.join(", ") };
}
