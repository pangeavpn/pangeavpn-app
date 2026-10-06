import {
  firstWorking,
  mergeAdvertised,
  promoteEntry,
  restoreCached,
  seedCached,
  type CredKind
} from "./hubCredList.ts";

/** Control-plane Shadowsocks credentials, out of pangeaApiClient so the
 *  cache rules are testable without electron. */
export interface HubShadowsocksCreds {
  remoteHost: string;
  remotePort: number;
  method: string;
  password: string;
}

/** Shipped control-plane nodes, so an install that has never reached the hub
 *  still has this route in. Each listener relays only to the hub on 443. */
export const DEFAULT_HUB_SHADOWSOCKS: readonly Readonly<HubShadowsocksCreds>[] = [
  {
    remoteHost: "192.248.175.17",
    remotePort: 8489,
    method: "2022-blake3-aes-128-gcm",
    password: "tUnJ/XLnK31LxBHhimZP5g=="
  },
  {
    remoteHost: "64.176.205.92",
    remotePort: 8489,
    method: "2022-blake3-aes-128-gcm",
    password: "RsMy+zj1BTQVj+jTa8ZPfA=="
  },
  {
    remoteHost: "136.244.108.254",
    remotePort: 8489,
    method: "2022-blake3-aes-128-gcm",
    password: "MIASyWggp3VO21RKPCB5cA=="
  },
  {
    remoteHost: "136.244.66.46",
    remotePort: 8489,
    method: "2022-blake3-aes-128-gcm",
    password: "YySUjWAvg9bDSTEYA0WdEw=="
  },
  {
    remoteHost: "208.76.222.40",
    remotePort: 8489,
    method: "2022-blake3-aes-128-gcm",
    password: "NulRaTBF6QqLx1dRaM1F3w=="
  }
];

// sing-shadowsocks2's shadowaead and shadowaead_2022 families; the legacy
// shadowstream ciphers are unauthenticated, so the daemon rejects them too.
const SUPPORTED_METHODS = new Set([
  "aes-128-gcm",
  "aes-192-gcm",
  "aes-256-gcm",
  "chacha20-ietf-poly1305",
  "xchacha20-ietf-poly1305",
  "2022-blake3-aes-128-gcm",
  "2022-blake3-aes-256-gcm",
  "2022-blake3-chacha20-poly1305"
]);

export function isHubShadowsocksCreds(value: unknown): value is HubShadowsocksCreds {
  const c = value as Partial<HubShadowsocksCreds> | null;
  return (
    !!c &&
    typeof c.remoteHost === "string" &&
    c.remoteHost.trim().length > 0 &&
    typeof c.remotePort === "number" &&
    Number.isInteger(c.remotePort) &&
    c.remotePort > 0 &&
    c.remotePort <= 65535 &&
    typeof c.method === "string" &&
    SUPPORTED_METHODS.has(c.method.trim()) &&
    typeof c.password === "string" &&
    c.password.trim().length > 0
  );
}

/** Trims the strings a validated candidate carries, so padding or a stray
 *  newline from the hub never lands in the cache or reaches proxy.start(). */
function normalizeHubShadowsocksCreds(candidate: HubShadowsocksCreds): HubShadowsocksCreds {
  return {
    remoteHost: candidate.remoteHost.trim(),
    remotePort: candidate.remotePort,
    method: candidate.method.trim(),
    password: candidate.password.trim()
  };
}

export function sameHubShadowsocks(a: HubShadowsocksCreds, b: HubShadowsocksCreds): boolean {
  return (
    a.remoteHost === b.remoteHost &&
    a.remotePort === b.remotePort &&
    a.method === b.method &&
    a.password === b.password
  );
}

const SHADOWSOCKS: CredKind<HubShadowsocksCreds> = {
  isValid: isHubShadowsocksCreds,
  normalize: normalizeHubShadowsocksCreds,
  same: sameHubShadowsocks
};

export const mergeAdvertisedCreds = (current: readonly HubShadowsocksCreds[], advertised: unknown[]) =>
  mergeAdvertised(SHADOWSOCKS, current, advertised);

export const promoteCreds = promoteEntry<HubShadowsocksCreds>;

export const firstWorkingCreds = <T>(
  list: readonly HubShadowsocksCreds[],
  attempt: (creds: HubShadowsocksCreds, index: number) => Promise<T | null>,
  onError?: (err: unknown, index: number) => void
) => firstWorking(list, attempt, onError);

export const restoreCachedCreds = (stored: unknown) => restoreCached(SHADOWSOCKS, stored);

export const seedCachedCreds = (stored: unknown) => seedCached(SHADOWSOCKS, stored, DEFAULT_HUB_SHADOWSOCKS);
