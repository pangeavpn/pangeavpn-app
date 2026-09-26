/** Ways the app may reach the hub. ensureHub tries each enabled one in order. */
export type HubMethod = "directIp" | "reality" | "shadowsocks" | "fronted" | "normal";

export interface HubMethods {
  /** Cached hub IP, and DoH-resolved IP with no SNI. Survives a Lockdown lock. */
  directIp: boolean;
  /** Hub traffic through the daemon's REALITY proxy, to a user pinned to the hub. */
  reality: boolean;
  /** Hub traffic through the daemon's Shadowsocks proxy. */
  shadowsocks: boolean;
  /** The envelope relayed by an edge worker on shared CDN address space. */
  fronted: boolean;
  /** Plain HTTPS to the hub domain — a normal DNS lookup and a cleartext SNI. */
  normal: boolean;
}

// Attempt order: directIp needs no lookup, REALITY passes for TLS where SS-2022
// is flagged as random, fronted only leaks timing, normal names the hub in cleartext.
export const HUB_METHOD_ORDER: readonly HubMethod[] = [
  "directIp",
  "reality",
  "shadowsocks",
  "fronted",
  "normal"
];

export const DEFAULT_HUB_METHODS: HubMethods = {
  directIp: true,
  reality: true,
  shadowsocks: true,
  fronted: true,
  normal: false
};

/** Bumped when a method's default changes; normalizeHubMethods re-applies the
 *  new default once for anything stored below this. Persisted as `rev`. */
export const HUB_METHODS_REV = 2;

/** Methods whose default flipped on at each rev. */
const REV_DEFAULTS: ReadonlyArray<{ rev: number; methods: readonly HubMethod[] }> = [
  { rev: 1, methods: ["shadowsocks", "fronted"] },
  { rev: 2, methods: ["reality"] }
];

export function isHubMethod(value: unknown): value is HubMethod {
  return typeof value === "string" && (HUB_METHOD_ORDER as readonly string[]).includes(value);
}

export function enabledHubMethods(methods: HubMethods): HubMethod[] {
  return HUB_METHOD_ORDER.filter((method) => methods[method]);
}

/** Applies one switch. Refuses to disable the last one, so the caller can say
 *  why nothing moved rather than silently correcting it. */
export function applyHubMethod(
  current: HubMethods,
  method: HubMethod,
  enabled: boolean
): { methods: HubMethods; applied: boolean } {
  if (current[method] === enabled) {
    return { methods: current, applied: true };
  }
  if (!enabled && enabledHubMethods(current).length === 1) {
    return { methods: current, applied: false };
  }
  return { methods: { ...current, [method]: enabled }, applied: true };
}

/** Reads the persisted shape, migrating the old directIpEnabled/directIpOnly
 *  pair. Always returns at least one enabled method. */
export function normalizeHubMethods(raw: unknown): HubMethods {
  const source = (raw ?? {}) as Record<string, unknown>;

  let methods: HubMethods;
  if (HUB_METHOD_ORDER.some((method) => typeof source[method] === "boolean")) {
    methods = {
      directIp: source.directIp === true,
      reality: source.reality === true,
      shadowsocks: source.shadowsocks === true,
      fronted: source.fronted === true,
      normal: source.normal === true
    };
    const storedRev = typeof source.rev === "number" ? source.rev : 0;
    for (const { rev, methods: flipped } of REV_DEFAULTS) {
      if (storedRev >= rev) continue;
      for (const method of flipped) methods[method] = DEFAULT_HUB_METHODS[method];
    }
  } else {
    // Migration: directIpOnly defaulted true ("never touch the domain"), so
    // normal is its inverse; a file this old predates the two newer methods.
    methods = {
      directIp: source.directIpEnabled !== false,
      reality: DEFAULT_HUB_METHODS.reality,
      shadowsocks: DEFAULT_HUB_METHODS.shadowsocks,
      fronted: DEFAULT_HUB_METHODS.fronted,
      normal: source.directIpOnly === false
    };
  }

  // directIp alone cannot produce a request without a cached hub IP or DoH, so
  // an all-off rescue restores the full default set rather than just one method.
  if (enabledHubMethods(methods).length === 0) {
    return { ...DEFAULT_HUB_METHODS };
  }
  return methods;
}

/** The shape written to settings.json: the switches plus the rev that says
 *  which default changes this file has already seen. */
export function persistableHubMethods(methods: HubMethods): Record<string, unknown> {
  return { ...methods, rev: HUB_METHODS_REV };
}

/** Why a method could not even be attempted, so the UI can say more than "failed". */
export type HubMethodUnavailable = "noAddress" | "noCredentials" | "noRelay" | "busy";

export interface HubMethodTestResult {
  method: HubMethod;
  ok: boolean;
  /** The address, relay host, or node the probe reached. */
  detail?: string;
  /** Set instead of a failure when the method had nothing to try. */
  unavailable?: HubMethodUnavailable;
  /** Round trip of the probe, in milliseconds. */
  ms: number;
}

/** Which method is carrying hub traffic right now, alongside the switches. */
export interface HubStatus {
  methods: HubMethods;
  active: HubMethod | null;
  detail: string | null;
}
