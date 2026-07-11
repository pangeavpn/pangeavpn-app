import { en } from "./locales/en.js";
import { es } from "./locales/es.js";
import { fr } from "./locales/fr.js";
import { ru } from "./locales/ru.js";
import { uk } from "./locales/uk.js";
import { zh } from "./locales/zh.js";
import { ar } from "./locales/ar.js";
import { fa } from "./locales/fa.js";
import {
  DEFAULT_LOCALE,
  localeMeta,
  matchLocale,
  type Locale,
  type MessageKey,
  type Messages
} from "./messages.js";

export {
  LOCALES,
  SUPPORTED_LOCALES,
  DEFAULT_LOCALE,
  isLocale,
  localeMeta,
  type Locale,
  type LocaleMeta,
  type MessageKey
} from "./messages.js";

const CATALOGUES: Record<Locale, Messages> = { en, es, fr, ru, uk, zh, ar, fa };

let activeLocale: Locale = DEFAULT_LOCALE;
let activeCatalogue: Messages = CATALOGUES[DEFAULT_LOCALE];

export function getLocale(): Locale {
  return activeLocale;
}

/** BCP-47 tag for the active locale, for Intl date/number formatting. */
export function localeTag(): string {
  return activeLocale;
}

type Params = Record<string, string | number>;

function interpolate(template: string, params?: Params): string {
  if (!params) return template;
  return template.replace(/\{(\w+)\}/g, (match, key: string) =>
    Object.prototype.hasOwnProperty.call(params, key) ? String(params[key]) : match
  );
}

/**
 * Translate a key for the active locale. Falls back to English if the active
 * catalogue somehow lacks the key (defensive — the type system prevents it).
 */
export function t(key: MessageKey, params?: Params): string {
  const template = activeCatalogue[key] ?? en[key] ?? key;
  return interpolate(template, params);
}

/**
 * Resolve the concrete locale to use. `stored` is the user's persisted choice
 * ("system" or a code); when it doesn't pin a language we detect from the
 * browser/OS, then fall back to English.
 */
export function resolveLocale(stored?: string | null): Locale {
  const explicit = matchLocale(stored ?? null);
  if (explicit) return explicit;
  const detected = matchLocale(typeof navigator !== "undefined" ? navigator.language : null);
  return detected ?? DEFAULT_LOCALE;
}

/** Apply `data-i18n*` annotations under `root` to their translated strings. */
export function applyStaticText(root: ParentNode = document): void {
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((el) => {
    const key = el.dataset.i18n as MessageKey | undefined;
    if (key) el.textContent = t(key);
  });
  applyAttr(root, "data-i18n-placeholder", "placeholder");
  applyAttr(root, "data-i18n-aria-label", "aria-label");
  applyAttr(root, "data-i18n-title", "title");
}

function applyAttr(root: ParentNode, dataAttr: string, target: string): void {
  root.querySelectorAll<HTMLElement>(`[${dataAttr}]`).forEach((el) => {
    const key = el.getAttribute(dataAttr) as MessageKey | null;
    if (key) el.setAttribute(target, t(key as MessageKey));
  });
}

/**
 * Activate a locale: set the catalogue, reflect language/direction on <html>,
 * and hydrate all static markup. Called once at startup (before the shell is
 * shown). The language only changes on next launch, so this is not re-run live.
 */
export function initLocale(locale: Locale): void {
  activeLocale = locale;
  activeCatalogue = CATALOGUES[locale] ?? CATALOGUES[DEFAULT_LOCALE];
  const meta = localeMeta(locale);
  const html = document.documentElement;
  html.lang = locale;
  html.dir = meta.dir;
  applyStaticText(document);
}
