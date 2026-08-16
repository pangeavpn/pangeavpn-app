import type { SubscriptionInfo } from "./ipc";

/** Validation for the subscription cached to settings.json, kept out of
 *  pangeaApiClient so the rules are testable without electron. */

const STATUSES = new Set([
  "trialing",
  "active",
  "past_due",
  "canceled",
  "unpaid",
  "incomplete",
  "none"
]);

export interface CachedSubscription {
  subscription: SubscriptionInfo;
  /** Epoch ms the hub last answered. Used to age the cache out. */
  cachedAt: number;
}

export function isSubscriptionInfo(value: unknown): value is SubscriptionInfo {
  const s = value as Partial<SubscriptionInfo> | null;
  if (!s || typeof s !== "object" || Array.isArray(s)) return false;
  if (typeof s.status !== "string" || !STATUSES.has(s.status)) return false;
  if (typeof s.renews !== "boolean") return false;
  if (s.expiresAt !== null && typeof s.expiresAt !== "string") return false;
  if (s.entitled !== undefined && typeof s.entitled !== "boolean") return false;
  return true;
}

/** The stored blob, or null when it is missing, malformed or from the future
 *  (a clock that jumped would otherwise pin a stale answer indefinitely). */
export function restoreCachedSubscription(stored: unknown): CachedSubscription | null {
  const blob = stored as Partial<CachedSubscription> | null;
  if (!blob || typeof blob !== "object" || Array.isArray(blob)) return null;
  if (typeof blob.cachedAt !== "number" || !Number.isFinite(blob.cachedAt)) return null;
  if (blob.cachedAt > Date.now() + 86_400_000) return null;
  if (!isSubscriptionInfo(blob.subscription)) return null;
  return { subscription: blob.subscription, cachedAt: blob.cachedAt };
}

/** An unreachable hub is not evidence that a paid account stopped being paid,
 *  so only a date already past revokes a cached entitlement. */
export function cachedEntitlement(cached: CachedSubscription, nowMs: number): boolean {
  const { subscription } = cached;
  if (subscription.entitled === false) return false;
  if (subscription.expiresAt) {
    const expiry = Date.parse(subscription.expiresAt);
    if (Number.isFinite(expiry) && expiry <= nowMs) return false;
  }
  return subscription.entitled !== undefined ? subscription.entitled : subscription.status !== "none";
}
