/** Ways the app may reach the hub. ensureHub tries each enabled one in order. */
export type HubMethod = "directIp" | "shadowsocks" | "fronted" | "normal";

export interface HubMethods {
  /** Cached hub IP, and DoH-resolved IP with no SNI. Survives a Lockdown lock. */
  directIp: boolean;
  /** Hub traffic through the daemon's Shadowsocks proxy. */
  shadowsocks: boolean;
  /** The envelope relayed by an edge worker on shared CDN address space. */
  fronted: boolean;
  /** Plain HTTPS to the hub domain — a normal DNS lookup and a cleartext SNI. */
  normal: boolean;
}

// Attempt order. directIp is first because it needs no lookup; fronted comes
// after our own paths because it hands a third party the timing of our traffic
// (never its content — see secureChannel), but before normal, which is the only
// one that puts the hub's name on the wire in cleartext.
export const HUB_METHOD_ORDER: readonly HubMethod[] = [
  "directIp",
  "shadowsocks",
  "fronted",
  "normal"
];

export const DEFAULT_HUB_METHODS: HubMethods = {
  directIp: true,
  shadowsocks: true,
  fronted: true,
  normal: false
};

/**
 * Bumped when a method's default changes in a way existing installs should
 * inherit. Everything the old default wrote to disk looks identical to a
 * deliberate choice, so without this an install would stay frozen on a value
 * the user never actually picked. normalizeHubMethods re-applies the changed
 * defaults once for anything stored below this; from then on the stored value
 * wins. Persisted inside the hubMethods object as `rev`.
 */
export const HUB_METHODS_REV = 1;

/** Methods whose default flipped on at HUB_METHODS_REV 1. */
const REV_1_DEFAULTS: readonly HubMethod[] = ["shadowsocks", "fronted"];

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
      shadowsocks: source.shadowsocks === true,
      fronted: source.fronted === true,
      normal: source.normal === true
    };
    if (typeof source.rev !== "number" || source.rev < HUB_METHODS_REV) {
      for (const method of REV_1_DEFAULTS) {
        methods[method] = DEFAULT_HUB_METHODS[method];
      }
    }
  } else {
    // Migration: directIpEnabled defaulted true, and directIpOnly defaulted
    // true meaning "never touch the domain", so normal is its inverse. A file
    // this old predates both newer methods, which take their current default.
    methods = {
      directIp: source.directIpEnabled !== false,
      shadowsocks: DEFAULT_HUB_METHODS.shadowsocks,
      fronted: DEFAULT_HUB_METHODS.fronted,
      normal: source.directIpOnly === false
    };
  }

  if (enabledHubMethods(methods).length === 0) {
    return { ...methods, directIp: true };
  }
  return methods;
}

/** The shape written to settings.json: the switches plus the rev that says
 *  which default changes this file has already seen. */
export function persistableHubMethods(methods: HubMethods): Record<string, unknown> {
  return { ...methods, rev: HUB_METHODS_REV };
}
