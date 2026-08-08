/** Ways the app may reach the hub. ensureHub tries each enabled one in order. */
export type HubMethod = "directIp" | "shadowsocks" | "normal";

export interface HubMethods {
  /** Cached hub IP, and DoH-resolved IP with no SNI. Survives a Lockdown lock. */
  directIp: boolean;
  /** Hub traffic through the daemon's Shadowsocks proxy. */
  shadowsocks: boolean;
  /** Plain HTTPS to the hub domain — a normal DNS lookup and a cleartext SNI. */
  normal: boolean;
}

// Attempt order. directIp is first because it needs no lookup; normal is last
// because it is the only one that puts the hub's name on the wire in cleartext.
export const HUB_METHOD_ORDER: readonly HubMethod[] = ["directIp", "shadowsocks", "normal"];

export const DEFAULT_HUB_METHODS: HubMethods = {
  directIp: true,
  shadowsocks: false,
  normal: false
};

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
  if (
    typeof source.directIp === "boolean" ||
    typeof source.shadowsocks === "boolean" ||
    typeof source.normal === "boolean"
  ) {
    methods = {
      directIp: source.directIp === true,
      shadowsocks: source.shadowsocks === true,
      normal: source.normal === true
    };
  } else {
    // Migration: directIpEnabled defaulted true, and directIpOnly defaulted
    // true meaning "never touch the domain", so normal is its inverse.
    methods = {
      directIp: source.directIpEnabled !== false,
      shadowsocks: false,
      normal: source.directIpOnly === false
    };
  }

  if (enabledHubMethods(methods).length === 0) {
    return { ...methods, directIp: true };
  }
  return methods;
}
