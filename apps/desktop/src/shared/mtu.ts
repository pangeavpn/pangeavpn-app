// WireGuard tunnel MTU. Written into the [Interface] block of the generated
// config; the daemon parses it and applies it to the TUN device.

/** IPv6's minimum link MTU — below this, IPv6 breaks outright. */
export const MTU_MIN = 1280;
/** wg-quick's standard ceiling for a 1500-byte underlay. */
export const MTU_MAX = 1420;
/** Conservative default, unchanged from before the setting existed. */
export const MTU_DEFAULT = 1380;

/**
 * Parse and range-check an MTU from an untrusted source — IPC input or a
 * hand-editable settings.json. Returns null when the value is unusable.
 */
export function normalizeMtu(value: unknown): number | null {
  let n: number;
  if (typeof value === "number") {
    n = value;
  } else if (typeof value === "string") {
    const trimmed = value.trim();
    // Number("") is 0 and Number(" ") is 0, so an empty string would otherwise
    // sail through as a number and get rejected only by the range check.
    if (trimmed === "") return null;
    n = Number(trimmed);
  } else {
    return null;
  }
  if (!Number.isInteger(n)) return null;
  if (n < MTU_MIN || n > MTU_MAX) return null;
  return n;
}

/** Same, but falls back to the default rather than reporting failure. */
export function normalizeMtuOrDefault(value: unknown): number {
  return normalizeMtu(value) ?? MTU_DEFAULT;
}
