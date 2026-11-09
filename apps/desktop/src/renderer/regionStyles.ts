// Layouts for the region list (hero slots and the full picker), chosen under
// Settings → Appearance. The name lands on html[data-region-style]; styles.css
// keys each layout off it, with "classic" being the unscoped base styles.
export const REGION_STYLES = ["tiles", "classic", "ledger", "chips", "sheet", "poster", "drawer"] as const;

export type RegionStyle = (typeof REGION_STYLES)[number];

export const DEFAULT_REGION_STYLE: RegionStyle = "tiles";

export function isRegionStyle(value: unknown): value is RegionStyle {
  return typeof value === "string" && (REGION_STYLES as readonly string[]).includes(value);
}

export function resolveRegionStyle(stored: string | null | undefined): RegionStyle {
  return isRegionStyle(stored) ? stored : DEFAULT_REGION_STYLE;
}

/** The drawer hero shows only the current region, so the picker is the sole way to switch. */
export function pickerAlwaysReachable(style: RegionStyle): boolean {
  return style === "drawer";
}
