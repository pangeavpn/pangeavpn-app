// Single source of truth for the accent. styles.css keeps only the per-theme
// alphas, deriving every accent shade from --accent-rgb.
export const ACCENT_THEMES = {
  terra: { rgb: "195 86 43", hover: "#a84a25" },
  purple: { rgb: "132 81 201", hover: "#6f41b0" },
  ocean: { rgb: "43 123 195", hover: "#226499" },
  emerald: { rgb: "38 128 79", hover: "#1d6a40" },
  rose: { rgb: "195 43 94", hover: "#a32450" }
} as const;

export type AccentName = keyof typeof ACCENT_THEMES;

export const ACCENT_NAMES = Object.keys(ACCENT_THEMES) as AccentName[];

export const DEFAULT_ACCENT: AccentName = "terra";

export function isAccentName(value: unknown): value is AccentName {
  return typeof value === "string" && Object.hasOwn(ACCENT_THEMES, value);
}

export function resolveAccent(stored: string | null | undefined): AccentName {
  return isAccentName(stored) ? stored : DEFAULT_ACCENT;
}

export function swatchColor(name: AccentName): string {
  return `rgb(${ACCENT_THEMES[name].rgb})`;
}
