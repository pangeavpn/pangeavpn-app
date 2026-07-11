import { en } from "./locales/en.js";

// The English catalogue defines the canonical key set. Every other locale is
// checked against `Messages` (via `satisfies`) so a missing or misspelled key
// is a compile error — guaranteeing 100% coverage across all languages.
export type MessageKey = keyof typeof en;
export type Messages = Record<MessageKey, string>;

export type Locale = "en" | "es" | "fr" | "ru" | "uk" | "zh" | "ar" | "fa";

export type TextDirection = "ltr" | "rtl";

export interface LocaleMeta {
  code: Locale;
  /** Endonym — the language's name in its own script. */
  nativeName: string;
  dir: TextDirection;
}

// Order here drives the Settings language picker.
export const LOCALES: readonly LocaleMeta[] = [
  { code: "en", nativeName: "English", dir: "ltr" },
  { code: "es", nativeName: "Español", dir: "ltr" },
  { code: "fr", nativeName: "Français", dir: "ltr" },
  { code: "ru", nativeName: "Русский", dir: "ltr" },
  { code: "uk", nativeName: "Українська", dir: "ltr" },
  { code: "zh", nativeName: "中文", dir: "ltr" },
  { code: "ar", nativeName: "العربية", dir: "rtl" },
  { code: "fa", nativeName: "فارسی", dir: "rtl" }
] as const;

export const SUPPORTED_LOCALES: readonly Locale[] = LOCALES.map((l) => l.code);
export const DEFAULT_LOCALE: Locale = "en";

export function isLocale(value: unknown): value is Locale {
  return typeof value === "string" && (SUPPORTED_LOCALES as readonly string[]).includes(value);
}

export function localeMeta(code: Locale): LocaleMeta {
  return LOCALES.find((l) => l.code === code) ?? LOCALES[0];
}

/**
 * Map an OS/browser language tag (e.g. "es-419", "zh-Hans-CN") to a supported
 * locale, or null when unsupported. Chinese variants collapse to "zh".
 */
export function matchLocale(tag: string | null | undefined): Locale | null {
  if (!tag) return null;
  const lower = tag.toLowerCase();
  const primary = lower.split("-")[0];
  if (isLocale(primary)) return primary;
  // A few common aliases.
  if (primary === "fas" || primary === "per") return "fa";
  if (primary === "zho" || primary === "chi") return "zh";
  if (primary === "ukr") return "uk";
  return null;
}
