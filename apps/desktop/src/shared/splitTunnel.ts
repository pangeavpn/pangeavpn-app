export type SplitTunnelAppErrorCode =
  | "notAbsolute"
  | "tooLong"
  | "nul"
  | "unsupportedForm"
  | "systemProcess"
  | "tooBroad"
  | "ownImage"
  | "tooMany"
  | "duplicate";

export type SplitTunnelCidrErrorCode = "notIPv4" | "prefixTooShort" | "tooMany" | "tooManyRoutes";

/** "invalid" stands in for a code this build doesn't know (a newer daemon). */
export type SplitTunnelErrorCode = SplitTunnelAppErrorCode | SplitTunnelCidrErrorCode | "invalid";

/** GET /split-tunnel. `apps` keep the case they were picked in. */
export interface SplitTunnelConfig {
  enabled: boolean;
  apps: string[];
  cidrs: string[];
  appsSupported: boolean;
  /** "" or a daemon code: classifierFailed, egressFailed, permitFailed, stackFailed, strictReversePath. */
  unavailableReason: string;
  active: boolean;
  /** Saved, but not yet applied to the live tunnel. */
  pending: boolean;
  /** The ranges stay in the tunnel on this connection: the server's routes can't fit them. */
  cidrsDropped: boolean;
}

/** `index` points into the list that was sent; `value` is that entry (the token as typed, for ranges). */
export interface SplitTunnelInvalid {
  field: "apps" | "cidrs";
  index: number;
  code: SplitTunnelErrorCode;
  value?: string;
}

export type SplitTunnelResult =
  | { ok: true; config: SplitTunnelConfig }
  | { ok: false; invalid: SplitTunnelInvalid[] };

/** POST /split-tunnel body; the daemon rejects any other field. */
export interface SplitTunnelWriteBody {
  enabled: boolean;
  apps: string[];
  cidrs: string[];
  protect?: string[];
}

export type SplitTunnelAppKind = "file" | "dir" | "bundle";

/** `rule` is what the daemon stores ("" when unsupported); `id` is unique per reply and is the icon key. */
export interface SplitTunnelAppEntry {
  id: string;
  name: string;
  rule: string;
  exe: string;
  kind: SplitTunnelAppKind;
  missing?: boolean;
  warning?: "inherits" | "mayNotWork";
  unsupported?: "flatpak" | "appimage" | "script";
}

/** `icon` is a data: URL, or "" when there is none to show. */
export interface SplitTunnelIcon {
  key: string;
  icon: string;
}

export type SplitTunnelBrowseResult =
  | { ok: true; entry: SplitTunnelAppEntry }
  | { ok: false; reason: "ownImage" | "notAnApp" };

const KNOWN_CODES: ReadonlySet<string> = new Set<SplitTunnelErrorCode>([
  "notAbsolute",
  "tooLong",
  "nul",
  "unsupportedForm",
  "systemProcess",
  "tooBroad",
  "ownImage",
  "tooMany",
  "duplicate",
  "notIPv4",
  "prefixTooShort",
  "tooManyRoutes",
  "invalid"
]);

export const DEFAULT_SPLIT_TUNNEL_CONFIG: SplitTunnelConfig = {
  enabled: false,
  apps: [],
  cidrs: [],
  appsSupported: false,
  unavailableReason: "",
  active: false,
  pending: false,
  cidrsDropped: false
};

function stringList(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  return value.filter((item): item is string => typeof item === "string");
}

/** Reads a daemon reply defensively; fields it lacks come from `fallback`. */
export function normalizeSplitTunnelConfig(
  raw: unknown,
  fallback: SplitTunnelConfig = DEFAULT_SPLIT_TUNNEL_CONFIG
): SplitTunnelConfig {
  const value = (typeof raw === "object" && raw !== null ? raw : {}) as Record<string, unknown>;
  const bool = (key: keyof SplitTunnelConfig): boolean =>
    typeof value[key] === "boolean" ? (value[key] as boolean) : (fallback[key] as boolean);
  return {
    enabled: bool("enabled"),
    apps: stringList(value.apps) ?? [...fallback.apps],
    cidrs: stringList(value.cidrs) ?? [...fallback.cidrs],
    appsSupported: bool("appsSupported"),
    unavailableReason:
      typeof value.unavailableReason === "string" ? value.unavailableReason : fallback.unavailableReason,
    active: bool("active"),
    pending: bool("pending"),
    // The daemon omits it when false, so a reply without it must not inherit a stale true.
    cidrsDropped: value.cidrsDropped === true
  };
}

/** The `invalid` list of a 400 `invalid_split_tunnel` body, or null for any other body. */
export function parseSplitTunnelInvalid(body: string): SplitTunnelInvalid[] | null {
  let payload: unknown;
  try {
    payload = JSON.parse(body);
  } catch {
    return null;
  }
  if (typeof payload !== "object" || payload === null) return null;
  const { error, invalid } = payload as { error?: unknown; invalid?: unknown };
  if (error !== "invalid_split_tunnel" || !Array.isArray(invalid)) return null;
  const out: SplitTunnelInvalid[] = [];
  for (const item of invalid) {
    if (typeof item !== "object" || item === null) continue;
    const { field, index, code } = item as { field?: unknown; index?: unknown; code?: unknown };
    if (field !== "apps" && field !== "cidrs") continue;
    if (typeof index !== "number" || !Number.isInteger(index) || index < 0) continue;
    out.push({
      field,
      index,
      code: typeof code === "string" && KNOWN_CODES.has(code) ? (code as SplitTunnelErrorCode) : "invalid"
    });
  }
  return out;
}

export interface ParsedCidrs {
  /** Valid tokens in order, bare addresses as /32. Not deduplicated, so a daemon index lines up with `tokens`. */
  cidrs: string[];
  tokens: string[];
  invalid: SplitTunnelInvalid[];
}

const CIDR_TOKEN = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?:\/(\d{1,2}))?$/;

// Leading zeros are refused because Go's netip parser refuses them too.
function cidrOf(token: string): string | null {
  const match = CIDR_TOKEN.exec(token);
  if (!match) return null;
  const octets = match.slice(1, 5);
  if (!octets.every((octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255))) return null;
  const prefix = match[5];
  if (prefix !== undefined && ((prefix.length > 1 && prefix.startsWith("0")) || Number(prefix) > 32)) return null;
  return `${octets.join(".")}/${prefix ?? "32"}`;
}

/** Splits the ranges field on commas, semicolons and whitespace. Prefix policy, overlap and limits are the daemon's call. */
export function parseCidrText(text: string): ParsedCidrs {
  const tokens = text.split(/[\s,;]+/).filter((token) => token.length > 0);
  const cidrs: string[] = [];
  const invalid: SplitTunnelInvalid[] = [];
  tokens.forEach((token, index) => {
    const cidr = cidrOf(token);
    if (cidr) cidrs.push(cidr);
    else invalid.push({ field: "cidrs", index, code: "notIPv4", value: token });
  });
  return { cidrs, tokens, invalid };
}

/** Comparison key for a stored rule: Windows and macOS paths are case-insensitive. */
export function splitRuleKey(rule: string, platform: string): string {
  if (platform === "win32") return rule.replace(/\//g, "\\").toLowerCase();
  if (platform === "darwin") return rule.replace(/\.app\/+$/i, ".app").toLowerCase();
  return rule;
}

export function sameSplitRule(a: string, b: string, platform: string): boolean {
  return splitRuleKey(a, platform) === splitRuleKey(b, platform);
}

export function withAppChange(apps: readonly string[], rule: string, excluded: boolean, platform: string): string[] {
  const key = splitRuleKey(rule, platform);
  if (excluded) {
    return apps.some((app) => splitRuleKey(app, platform) === key) ? [...apps] : [...apps, rule];
  }
  return apps.filter((app) => splitRuleKey(app, platform) !== key);
}

/** This app's image, whose process tree must never bypass; on macOS the bundle, so the Electron helpers are covered. */
export function protectRulesFor(execPath: string, platform: string): string[] {
  if (!execPath) return [];
  if (platform === "darwin") {
    const at = execPath.indexOf(".app/");
    return [at >= 0 ? execPath.slice(0, at + 4) : execPath];
  }
  return [execPath];
}

export class SplitTunnelUnsupportedError extends Error {
  constructor() {
    super("split tunnelling is not supported by this PangeaVPN service");
    this.name = "SplitTunnelUnsupportedError";
  }
}

export interface SplitTunnelBackend {
  get(): Promise<SplitTunnelConfig | null>;
  /** `current` fills the fields a POST reply leaves out. */
  set(body: SplitTunnelWriteBody, current: SplitTunnelConfig): Promise<SplitTunnelResult | null>;
}

export interface SplitTunnelWriter {
  setEnabled(enabled: boolean): Promise<SplitTunnelResult>;
  setApp(rule: string, excluded: boolean): Promise<SplitTunnelResult>;
  setCidrs(text: string): Promise<SplitTunnelResult>;
}

type Draft = Pick<SplitTunnelConfig, "enabled" | "apps" | "cidrs">;

/** The one writer: each change re-reads the daemon's config and posts it back in
 *  order, so overlapping edits can't undo each other and a failed read writes nothing. */
export function createSplitTunnelWriter(
  backend: SplitTunnelBackend,
  options: { platform: string; protect: () => string[] }
): SplitTunnelWriter {
  let chain: Promise<unknown> = Promise.resolve();

  function serial<T>(op: () => Promise<T>): Promise<T> {
    const run = chain.catch(() => {}).then(op);
    chain = run.catch(() => {});
    return run;
  }

  async function update(change: (current: SplitTunnelConfig) => Draft, cidrTokens?: string[]): Promise<SplitTunnelResult> {
    const current = await backend.get();
    if (!current) throw new SplitTunnelUnsupportedError();
    const next = change(current);
    const body: SplitTunnelWriteBody = {
      enabled: next.enabled,
      apps: next.apps,
      cidrs: next.cidrs,
      protect: options.protect()
    };
    const reply = await backend.set(body, current);
    if (!reply) throw new SplitTunnelUnsupportedError();
    if (reply.ok) return reply;
    return {
      ok: false,
      invalid: reply.invalid.map((item) => {
        const value = (item.field === "apps" ? body.apps : (cidrTokens ?? body.cidrs))[item.index];
        return value === undefined ? item : { ...item, value };
      })
    };
  }

  return {
    setEnabled: (enabled) =>
      serial(() => update((current) => ({ enabled: enabled === true, apps: current.apps, cidrs: current.cidrs }))),
    setApp: (rule, excluded) =>
      serial(() =>
        update((current) => ({
          enabled: current.enabled,
          apps: withAppChange(current.apps, rule, excluded, options.platform),
          cidrs: current.cidrs
        }))
      ),
    setCidrs: (text) => {
      const parsed = parseCidrText(text);
      if (parsed.invalid.length > 0) return Promise.resolve({ ok: false, invalid: parsed.invalid });
      return serial(() =>
        update((current) => ({ enabled: current.enabled, apps: current.apps, cidrs: parsed.cidrs }), parsed.tokens)
      );
    }
  };
}
