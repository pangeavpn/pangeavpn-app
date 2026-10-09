import type { ServerInfo } from "./ipc";

/** Validation for the node list cached to settings.json. Only the fields
 *  provision() needs are checked; the daemon rejects a malformed transport block itself. */

function isNonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function isOptionalBlock(value: unknown): boolean {
  return (
    value === undefined ||
    value === null ||
    (typeof value === "object" && !Array.isArray(value))
  );
}

export function isCachedServer(value: unknown): value is ServerInfo {
  const s = value as Partial<ServerInfo> | null;
  if (!s || typeof s !== "object" || Array.isArray(s)) return false;
  if (!isNonEmptyString(s.id) || !isNonEmptyString(s.name)) return false;
  if (!isNonEmptyString(s.region) || !isNonEmptyString(s.country)) return false;

  const cloak = s.cloak;
  if (!cloak || typeof cloak !== "object" || Array.isArray(cloak)) return false;
  if (!isNonEmptyString(cloak.remoteHost)) return false;
  if (!isNonEmptyString(cloak.uid) || !isNonEmptyString(cloak.publicKey)) return false;

  return (
    isOptionalBlock(s.naive) &&
    isOptionalBlock(s.reality) &&
    isOptionalBlock(s.hysteria2) &&
    isOptionalBlock(s.shadowsocks) &&
    isOptionalBlock(s.snowflake)
  );
}

/** Validated and deduplicated by id, order preserved. */
export function restoreCachedServers(stored: unknown): ServerInfo[] {
  if (!Array.isArray(stored)) return [];
  const out: ServerInfo[] = [];
  for (const candidate of stored) {
    if (!isCachedServer(candidate)) continue;
    if (out.some((s) => s.id === candidate.id)) continue;
    out.push(candidate);
  }
  return out;
}
