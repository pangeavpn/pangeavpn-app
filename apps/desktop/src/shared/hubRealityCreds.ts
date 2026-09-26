import { normalizeFrontedEndpoint } from "./frontedEndpoints.ts";
import { restoreCached, seedCached, type CredKind } from "./hubCredList.ts";
import { isIPv4Literal } from "./ipLiteral.ts";

/** A node's control-plane REALITY user, which the node pins to the hub on 443. */
export interface HubRealityCreds {
  remoteHost: string;
  remotePort: number;
  uuid: string;
  /** X25519, base64url without padding. */
  publicKey: string;
  shortId: string;
  /** The cover SNI the node's REALITY inbound answers to. */
  serverName: string;
}

/** Shipped nodes, so an install that has never reached the hub still has this
 *  route in. Serves the same purpose as DEFAULT_HUB_SHADOWSOCKS. */
export const DEFAULT_HUB_REALITY: readonly Readonly<HubRealityCreds>[] = [
  {
    remoteHost: "95.179.239.1",
    remotePort: 443,
    uuid: "cf550715-b9c8-4a58-a610-dc5cc73e36f4",
    publicKey: "8rifnTuJS517L1ysYdVaSvCntor5nkC3dn1XGqWMYlg",
    shortId: "355adc938875db2a",
    serverName: "swdist.apple.com"
  }
];

const UUID = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
const PUBLIC_KEY = /^[A-Za-z0-9_-]{43}$/;
const SHORT_ID = /^(?:[0-9a-f]{2}){1,8}$/i;

// An IP literal only: a name needs DNS before this path can help, and the
// kill switch will only permit an address.
export function isHubRealityCreds(value: unknown): value is HubRealityCreds {
  const c = value as Partial<HubRealityCreds> | null;
  return (
    !!c &&
    typeof c.remoteHost === "string" &&
    isIPv4Literal(c.remoteHost.trim()) &&
    typeof c.remotePort === "number" &&
    Number.isInteger(c.remotePort) &&
    c.remotePort > 0 &&
    c.remotePort <= 65535 &&
    typeof c.uuid === "string" &&
    UUID.test(c.uuid.trim()) &&
    typeof c.publicKey === "string" &&
    PUBLIC_KEY.test(c.publicKey.trim()) &&
    typeof c.shortId === "string" &&
    SHORT_ID.test(c.shortId.trim()) &&
    normalizeFrontedEndpoint(c.serverName) !== null
  );
}

function normalizeHubReality(candidate: HubRealityCreds): HubRealityCreds {
  return {
    remoteHost: candidate.remoteHost.trim(),
    remotePort: candidate.remotePort,
    uuid: candidate.uuid.trim().toLowerCase(),
    publicKey: candidate.publicKey.trim(),
    shortId: candidate.shortId.trim().toLowerCase(),
    serverName: normalizeFrontedEndpoint(candidate.serverName) ?? ""
  };
}

export function sameHubReality(a: HubRealityCreds, b: HubRealityCreds): boolean {
  return (
    a.remoteHost === b.remoteHost &&
    a.remotePort === b.remotePort &&
    a.uuid === b.uuid &&
    a.publicKey === b.publicKey &&
    a.shortId === b.shortId &&
    a.serverName === b.serverName
  );
}

export const REALITY_CREDS: CredKind<HubRealityCreds> = {
  isValid: isHubRealityCreds,
  normalize: normalizeHubReality,
  same: sameHubReality
};

export const restoreRealityCreds = (stored: unknown) => restoreCached(REALITY_CREDS, stored);

export const seedRealityCreds = (stored: unknown) => seedCached(REALITY_CREDS, stored, DEFAULT_HUB_REALITY);
