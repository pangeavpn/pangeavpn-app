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

// Marker for catalogue values with CLDR plural forms, e.g.
// "@plural:one={count} server|few={count} servers|other={count} servers".
// Selected via Intl.PluralRules on the "count" param; unmarked strings pass through.
const PLURAL_PREFIX = "@plural:";

function resolvePlural(template: string, locale: Locale, params?: Params): string {
  if (!template.startsWith(PLURAL_PREFIX)) return template;
  const forms = new Map(
    template
      .slice(PLURAL_PREFIX.length)
      .split("|")
      .map((seg) => {
        const i = seg.indexOf("=");
        return [seg.slice(0, i), seg.slice(i + 1)] as const;
      })
  );
  const count = Number(params?.count ?? 0);
  const category = new Intl.PluralRules(locale).select(count);
  return forms.get(category) ?? forms.get("other") ?? template;
}

/**
 * Translate a key for the active locale. Falls back to English if the active
 * catalogue somehow lacks the key (defensive — the type system prevents it).
 */
export function t(key: MessageKey, params?: Params): string {
  const raw = activeCatalogue[key] ?? en[key] ?? key;
  const template = resolvePlural(raw, activeLocale, params);
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

// Keys whose markup (e.g. <strong>/<em>) must survive translation, not be
// flattened to text. Catalogue values for these are trusted static markup.
const HTML_KEYS: ReadonlySet<MessageKey> = new Set(["update.macStep"] as MessageKey[]);

/** Apply `data-i18n*` annotations under `root` to their translated strings. */
export function applyStaticText(root: ParentNode = document): void {
  root.querySelectorAll<HTMLElement>("[data-i18n]").forEach((el) => {
    const key = el.dataset.i18n as MessageKey | undefined;
    if (!key) return;
    if (HTML_KEYS.has(key)) el.innerHTML = t(key);
    else el.textContent = t(key);
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
  forceLtr("logs");
  forceLtr("loginTokenInput");
}

/** Diagnostic/input text is always LTR content, even under an RTL page direction. */
function forceLtr(id: string): void {
  const el = document.getElementById(id);
  if (el) el.dir = "ltr";
}
