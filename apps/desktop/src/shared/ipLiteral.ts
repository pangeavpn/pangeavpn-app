/** IP literal checks, shared by everything that decides whether a host is an
 *  address to dial verbatim or a name to resolve. */

const IPV4_LITERAL = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

export function isIPv4Literal(host: string): boolean {
  const match = IPV4_LITERAL.exec(host);
  if (match === null) return false;
  // Go's net.ParseIP rejects leading zeros; a literal it would reject must not
  // be dialed verbatim here either.
  return match
    .slice(1, 5)
    .every((octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255));
}

// Deliberately permissive (accepts some invalid IPv6): a false positive just
// means we dial verbatim instead of substituting the node, which is safe.
export function isIPv6Literal(host: string): boolean {
  const bare = host.startsWith("[") && host.endsWith("]") ? host.slice(1, -1) : host;
  if (!bare.includes(":")) return false;
  const halves = bare.split("::");
  if (halves.length > 2) return false;
  const groups = halves.flatMap((half) => (half.length ? half.split(":") : []));
  if (halves.length === 1 && groups.length !== 8) return false;
  if (halves.length === 2 && groups.length >= 8) return false;
  return groups.every((group) => /^[0-9a-fA-F]{1,4}$/.test(group));
}

export function isIpLiteral(host: string): boolean {
  return isIPv4Literal(host) || isIPv6Literal(host);
}
