import { createHash } from "node:crypto";

/** How long a hub-registered WireGuard peer is reused before re-registering. */
export const PROFILE_TTL_MS = 12 * 60 * 60 * 1000;

/** Upper bound on retained peers, so hopping regions can't grow the daemon
 *  config without limit. */
export const MAX_CACHED_PROFILES = 6;

const MANAGED_PREFIX = "auto-";

export interface ProfileRecord {
  serverId: string;
  provisionedAt: number;
  /** Inputs baked into the profile. A change here invalidates the peer even
   *  while it is still inside its TTL. */
  fingerprint: string;
}

export type ProfileRecords = Readonly<Record<string, ProfileRecord>>;

export interface FingerprintInput {
  wireguardMtu: number;
  customDns: readonly string[];
  server: unknown;
}

export function profileFingerprint(input: FingerprintInput): string {
  const payload = JSON.stringify({
    mtu: input.wireguardMtu,
    dns: [...input.customDns],
    server: input.server ?? null
  });
  return createHash("sha256").update(payload).digest("hex").slice(0, 32);
}

export function parseProfileRecords(stored: unknown): ProfileRecords {
  if (typeof stored !== "object" || stored === null || Array.isArray(stored)) {
    return {};
  }
  const records: Record<string, ProfileRecord> = {};
  for (const [profileId, value] of Object.entries(stored as Record<string, unknown>)) {
    const record = parseRecord(value);
    if (record && profileId.startsWith(MANAGED_PREFIX)) {
      records[profileId] = record;
    }
  }
  return records;
}

function parseRecord(value: unknown): ProfileRecord | null {
  if (typeof value !== "object" || value === null) return null;
  const { serverId, provisionedAt, fingerprint } = value as Record<string, unknown>;
  if (typeof serverId !== "string" || serverId === "") return null;
  if (typeof provisionedAt !== "number" || !Number.isFinite(provisionedAt)) return null;
  if (typeof fingerprint !== "string" || fingerprint === "") return null;
  return { serverId, provisionedAt, fingerprint };
}

export function isReusable(
  record: ProfileRecord | undefined,
  fingerprint: string,
  now: number
): boolean {
  if (!record) return false;
  if (record.fingerprint !== fingerprint) return false;
  const age = now - record.provisionedAt;
  // A record stamped in the future is a clock change, not a fresh peer.
  return age >= 0 && age < PROFILE_TTL_MS;
}

export function recordProvision(
  records: ProfileRecords,
  profileId: string,
  record: ProfileRecord
): ProfileRecords {
  return retainNewest({ ...records, [profileId]: record });
}

export function forgetProfile(records: ProfileRecords, profileId: string): ProfileRecords {
  const { [profileId]: _dropped, ...rest } = records;
  return rest;
}

export function dropExpired(records: ProfileRecords, now: number): ProfileRecords {
  return Object.fromEntries(
    Object.entries(records).filter(([, record]) => isReusable(record, record.fingerprint, now))
  );
}

/** Forgets records whose profile the daemon no longer holds. */
export function retainOnly(records: ProfileRecords, profileIds: readonly string[]): ProfileRecords {
  const live = new Set(profileIds);
  return Object.fromEntries(Object.entries(records).filter(([id]) => live.has(id)));
}

function retainNewest(records: ProfileRecords): ProfileRecords {
  const entries = Object.entries(records);
  if (entries.length <= MAX_CACHED_PROFILES) return records;
  return Object.fromEntries(
    entries
      .sort(([, a], [, b]) => b.provisionedAt - a.provisionedAt)
      .slice(0, MAX_CACHED_PROFILES)
  );
}

/**
 * The profile set to hand the daemon after a successful connect: the winner,
 * every profile the app does not manage, and the managed peers still worth
 * reusing. Anything else is a spent peer and is dropped.
 */
export function commitProfileSet<T extends { id: string }>(
  profiles: readonly T[],
  winner: T,
  reusableIds: readonly string[]
): T[] {
  const keep = new Set(reusableIds);
  return [
    ...profiles.filter(
      (profile) =>
        profile.id !== winner.id &&
        (!profile.id.startsWith(MANAGED_PREFIX) || keep.has(profile.id))
    ),
    winner
  ];
}
