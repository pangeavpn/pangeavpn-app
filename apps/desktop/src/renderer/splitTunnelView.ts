import type { MessageKey } from "./i18n/messages.js";
import type { Translate } from "./hubStatusText.js";

export type SplitLoad = "loading" | "unreachable" | "unsupported" | "loaded";

type SplitLists = Pick<SplitTunnelConfig, "enabled" | "apps" | "cidrs">;

/** Mirrors splitRuleKey in shared/splitTunnel.ts: Windows and macOS paths compare case-insensitively. */
export function splitRuleKey(rule: string, platform: string): string {
  if (platform === "win32") return rule.replace(/\//g, "\\").toLowerCase();
  if (platform === "darwin") return rule.replace(/\.app\/+$/i, ".app").toLowerCase();
  return rule;
}

/** A row is its rule; unsupported rows have none, so their catalog id stands in. */
export function splitEntryKey(entry: Pick<SplitTunnelAppEntry, "id" | "rule">, platform: string): string {
  return entry.rule ? `rule:${splitRuleKey(entry.rule, platform)}` : `id:${entry.id}`;
}

export function splitEntryPath(entry: Pick<SplitTunnelAppEntry, "rule" | "exe">): string {
  return entry.rule || entry.exe;
}

/** Stands in for an entry the main process couldn't describe, so the rule can still be removed. */
export function fallbackSplitEntry(rule: string, platform: string): SplitTunnelAppEntry {
  const kind: SplitTunnelAppEntry["kind"] =
    platform === "darwin" && /\.app\/?$/i.test(rule) ? "bundle" : /[\\/]$/.test(rule) ? "dir" : "file";
  const base = rule.replace(/[\\/]+$/, "").split(/[\\/]/).pop() ?? "";
  return {
    id: `rule:${splitRuleKey(rule, platform)}`,
    name: base.replace(/\.(exe|app)$/i, "") || rule,
    rule,
    exe: kind === "dir" ? "" : rule,
    kind
  };
}

const ELLIPSIS = "…";

/** Cuts a path from the middle, keeping its last two segments when they fit. */
export function middleEllipsis(path: string, max: number): string {
  if (path.length <= max) return path;
  if (max <= 1) return ELLIPSIS;
  const trimmed = path.replace(/[\\/]+$/, "");
  const trail = path.slice(trimmed.length);
  const separators: number[] = [];
  for (let i = 0; i < trimmed.length; i++) {
    if (trimmed[i] === "\\" || trimmed[i] === "/") separators.push(i);
  }
  for (const keep of [2, 1]) {
    const at = separators.length >= keep ? separators[separators.length - keep] : -1;
    if (at <= 0) continue;
    const tail = trimmed.slice(at) + trail;
    const room = max - ELLIPSIS.length - tail.length;
    if (room >= 1) return path.slice(0, room) + ELLIPSIS + tail;
  }
  return ELLIPSIS + path.slice(path.length - (max - ELLIPSIS.length));
}

export function foldForSearch(text: string, locale?: string): string {
  return text.normalize("NFD").replace(/\p{M}+/gu, "").toLocaleLowerCase(locale);
}

export function splitEntryMatches(entry: SplitTunnelAppEntry, foldedQuery: string, locale?: string): boolean {
  if (!foldedQuery) return true;
  return (
    foldForSearch(entry.name, locale).includes(foldedQuery) ||
    foldForSearch(splitEntryPath(entry), locale).includes(foldedQuery)
  );
}

export interface SplitGroupInput {
  entries: ReadonlyMap<string, SplitTunnelAppEntry>;
  /** Picked with Browse during this visit, newest first; they head the Excluded group. */
  pinned: readonly string[];
  /** Everything else that belongs in the Excluded group. */
  excluded: ReadonlySet<string>;
  catalog: readonly string[];
  query: string;
  locale: string;
}

/** Excluded rows first, then the rest of the catalog, both filtered by the search. */
export function groupSplitRows(input: SplitGroupInput): { excluded: string[]; catalog: string[] } {
  const { entries, pinned, excluded, catalog, locale } = input;
  const collator = new Intl.Collator(locale, { sensitivity: "base", numeric: true });
  const query = foldForSearch(input.query.trim(), locale);
  const byName = (a: string, b: string): number => {
    const x = entries.get(a)!;
    const y = entries.get(b)!;
    return collator.compare(x.name, y.name) || collator.compare(splitEntryPath(x), splitEntryPath(y));
  };
  const shown = (key: string): boolean => {
    const entry = entries.get(key);
    return entry !== undefined && splitEntryMatches(entry, query, locale);
  };
  const top = pinned.filter((key) => entries.has(key));
  const topSet = new Set(top);
  const rest = [...excluded].filter((key) => !topSet.has(key) && entries.has(key)).sort(byName);
  const taken = new Set([...top, ...rest]);
  return {
    excluded: [...top, ...rest].filter(shown),
    catalog: catalog.filter((key) => !taken.has(key) && entries.has(key)).sort(byName).filter(shown)
  };
}

export function splitRailSummary(load: SplitLoad, config: SplitLists | null, t: Translate): string {
  if (load === "unsupported") return t("settings.splitTunnel.summary.unavailable");
  if (load !== "loaded" || !config) return "";
  if (!config.enabled) return t("settings.summary.off");
  const parts: string[] = [];
  if (config.apps.length > 0) parts.push(t("settings.splitTunnel.summary.apps", { count: config.apps.length }));
  if (config.cidrs.length > 0) parts.push(t("settings.splitTunnel.summary.ranges", { count: config.cidrs.length }));
  return parts.length > 0 ? parts.join(" · ") : t("settings.splitTunnel.summary.empty");
}

export function splitPickerSummary(config: SplitLists | null, t: Translate): string {
  const count = config?.apps.length ?? 0;
  return count > 0 ? t("settings.splitTunnel.summary.apps", { count }) : t("settings.splitTunnel.apps.none");
}

const APP_ERROR_KEYS: Partial<Record<SplitTunnelErrorCode, MessageKey>> = {
  notAbsolute: "settings.splitTunnel.error.apps.notAbsolute",
  tooLong: "settings.splitTunnel.error.apps.tooLong",
  nul: "settings.splitTunnel.error.apps.nul",
  unsupportedForm: "settings.splitTunnel.error.apps.unsupportedForm",
  systemProcess: "settings.splitTunnel.error.apps.systemProcess",
  tooBroad: "settings.splitTunnel.error.apps.tooBroad",
  ownImage: "settings.splitTunnel.error.apps.ownImage",
  tooMany: "settings.splitTunnel.error.apps.tooMany",
  duplicate: "settings.splitTunnel.error.apps.duplicate"
};

export function splitAppErrorText(code: SplitTunnelErrorCode, t: Translate): string {
  return t(APP_ERROR_KEYS[code] ?? "settings.splitTunnel.error.apps.invalid");
}

const CIDR_TOKEN_ERROR_KEYS: Partial<Record<SplitTunnelErrorCode, MessageKey>> = {
  notIPv4: "settings.splitTunnel.error.cidrs.notIPv4",
  prefixTooShort: "settings.splitTunnel.error.cidrs.prefixTooShort"
};

const CIDR_LIST_ERROR_KEYS: Partial<Record<SplitTunnelErrorCode, MessageKey>> = {
  tooMany: "settings.splitTunnel.error.cidrs.tooMany",
  tooManyRoutes: "settings.splitTunnel.error.cidrs.tooManyRoutes"
};

/** Left-to-right isolate: keeps a path or address intact inside right-to-left text. */
export function isolateLtr(text: string): string {
  return `⁦${text}⁩`;
}

/** First-strong isolate, for names that may be in either direction. */
export function isolateAuto(text: string): string {
  return `⁨${text}⁩`;
}

/** One sentence per kind of problem in the ranges field, naming the entries it applies to. */
export function splitCidrErrorText(invalid: readonly SplitTunnelInvalid[], t: Translate): string {
  const tokens = new Map<MessageKey, string[]>();
  const lists = new Set<MessageKey>();
  for (const item of invalid) {
    if (item.field !== "cidrs") continue;
    const listKey = CIDR_LIST_ERROR_KEYS[item.code];
    if (listKey) {
      lists.add(listKey);
      continue;
    }
    const key = CIDR_TOKEN_ERROR_KEYS[item.code] ?? "settings.splitTunnel.error.cidrs.invalid";
    const values = tokens.get(key) ?? [];
    if (item.value !== undefined && !values.includes(item.value)) values.push(item.value);
    tokens.set(key, values);
  }
  const sentences: string[] = [];
  for (const [key, values] of tokens) {
    sentences.push(t(key, { entries: values.map(isolateLtr).join(", ") || "—" }));
  }
  for (const key of lists) sentences.push(t(key));
  return sentences.join(" ");
}

/** The ranges field's error after a refused save. A refusal over a stored app alone still
 *  says the ranges weren't saved, and which app to fix first. */
export function splitRangesSaveError(invalid: readonly SplitTunnelInvalid[], platform: string, t: Translate): string {
  const ranges = splitCidrErrorText(invalid, t);
  if (ranges || !invalid.some((item) => item.field === "apps")) return ranges;
  const names = new Set<string>();
  for (const item of invalid) {
    if (item.field === "apps" && item.value) names.add(isolateAuto(fallbackSplitEntry(item.value, platform).name));
  }
  return t("settings.splitTunnel.ranges.blockedByApps", { apps: [...names].join(", ") || "—" });
}

/** Maps the apps half of a rejected write back to the rows it names. */
export function splitInvalidByRule(invalid: readonly SplitTunnelInvalid[], platform: string): Map<string, SplitTunnelErrorCode> {
  const out = new Map<string, SplitTunnelErrorCode>();
  for (const item of invalid) {
    if (item.field === "apps" && item.value) out.set(`rule:${splitRuleKey(item.value, platform)}`, item.code);
  }
  return out;
}

const APPS_UNAVAILABLE_KEYS: Record<string, MessageKey> = {
  classifierFailed: "settings.splitTunnel.appsUnavailable.classifierFailed",
  egressFailed: "settings.splitTunnel.appsUnavailable.egressFailed",
  permitFailed: "settings.splitTunnel.appsUnavailable.permitFailed",
  stackFailed: "settings.splitTunnel.appsUnavailable.stackFailed",
  strictReversePath: "settings.splitTunnel.appsUnavailable.strictReversePath"
};

/** Why excluded apps can't bypass; `blocking` when the service can't exclude apps at all. */
export function splitAppsUnavailable(
  config: Pick<SplitTunnelConfig, "appsSupported" | "unavailableReason">,
  t: Translate
): { text: string; blocking: boolean } | null {
  const reason = config.unavailableReason;
  if (!config.appsSupported) {
    const key = Object.hasOwn(APPS_UNAVAILABLE_KEYS, reason)
      ? APPS_UNAVAILABLE_KEYS[reason]
      : "settings.splitTunnel.appsUnavailable.unsupportedOS";
    return { text: t(key), blocking: true };
  }
  if (!reason) return null;
  const key = Object.hasOwn(APPS_UNAVAILABLE_KEYS, reason)
    ? APPS_UNAVAILABLE_KEYS[reason]
    : "settings.splitTunnel.appsUnavailable.unknown";
  return { text: t(key), blocking: false };
}

/** The picker's standing note: why apps can't bypass, or else that the master switch is off. */
export function splitPaneNote(
  apps: { text: string; blocking: boolean } | null,
  enabled: boolean | null,
  t: Translate
): { text: string; tone: SplitNoteTone; turnOn: boolean } | null {
  if (apps) return { text: apps.text, tone: apps.blocking ? "error" : "warn", turnOn: false };
  if (enabled === false) return { text: t("settings.splitTunnel.paneOff"), tone: "warn", turnOn: true };
  return null;
}

/** Puts `nodes` in `parent` in order, moving rows that stay instead of detaching them:
 *  a detached row that had keyboard focus would drop it to <body>. */
export function placeChildren(parent: Element, nodes: readonly Node[]): void {
  const keep = new Set(nodes);
  for (const child of Array.from(parent.childNodes)) {
    if (!keep.has(child)) parent.removeChild(child);
  }
  let next = parent.firstChild;
  for (const node of nodes) {
    if (node === next) next = node.nextSibling;
    else if (node.parentNode === parent) parent.moveBefore(node, next);
    else parent.insertBefore(node, next);
  }
}

const UNSUPPORTED_KEYS: Record<NonNullable<SplitTunnelAppEntry["unsupported"]>, MessageKey> = {
  flatpak: "settings.splitTunnel.apps.unsupported.flatpak",
  appimage: "settings.splitTunnel.apps.unsupported.appimage",
  script: "settings.splitTunnel.apps.unsupported.script"
};

export type SplitNoteTone = "error" | "warn" | "muted";

/** The line under a row's path: a rejection first, then what's wrong with the app, then caveats. */
export function splitRowNote(
  entry: SplitTunnelAppEntry,
  invalid: SplitTunnelErrorCode | undefined,
  t: Translate
): { text: string; tone: SplitNoteTone } | null {
  if (invalid) return { text: splitAppErrorText(invalid, t), tone: "error" };
  if (entry.missing) return { text: t("settings.splitTunnel.apps.missing"), tone: "error" };
  if (!entry.rule) {
    const key = entry.unsupported ? UNSUPPORTED_KEYS[entry.unsupported] : "settings.splitTunnel.apps.unsupported.script";
    return { text: t(key), tone: "muted" };
  }
  const name = isolateAuto(entry.name);
  if (entry.warning === "inherits") return { text: t("settings.splitTunnel.apps.inherits", { name }), tone: "warn" };
  if (entry.warning === "mayNotWork") return { text: t("settings.splitTunnel.apps.mayNotWork", { name }), tone: "warn" };
  return null;
}

/** Whether the service stored a different set than was typed, e.g. 10.1.2.3/8 kept as 10.0.0.0/8. */
export function splitCidrsAdjusted(typed: string, stored: readonly string[]): boolean {
  const asTyped = new Set(
    typed
      .split(/[\s,;]+/)
      .filter((token) => token.length > 0)
      .map((token) => (token.includes("/") ? token : `${token}/32`))
  );
  const kept = new Set(stored);
  return asTyped.size !== kept.size || [...asTyped].some((token) => !kept.has(token));
}

export interface SplitStatusSummary {
  enabled: boolean;
  rules: number;
  pending: boolean;
  unavailableReason: string;
  cidrsDropped: boolean;
}

/** Reads the optional `splitTunnel` block of a /status reply; null when an older service leaves it out. */
export function splitStatusOf(status: unknown): SplitStatusSummary | null {
  if (typeof status !== "object" || status === null) return null;
  const block = (status as { splitTunnel?: unknown }).splitTunnel;
  if (typeof block !== "object" || block === null) return null;
  const value = block as Record<string, unknown>;
  const count = (key: string): number => (typeof value[key] === "number" && (value[key] as number) > 0 ? (value[key] as number) : 0);
  return {
    enabled: value.enabled === true,
    rules: count("appCount") + count("cidrCount"),
    pending: value.pending === true,
    unavailableReason: typeof value.unavailableReason === "string" ? value.unavailableReason : "",
    cidrsDropped: value.cidrsDropped === true
  };
}

export function splitChipVisible(connected: boolean, status: unknown): boolean {
  const split = splitStatusOf(status);
  return connected && split !== null && split.enabled && split.rules > 0;
}
