// Misreading a lapse as a bad identity clears the keypair, and the next sign-in
// registers as a NEW device — burning one of the account's four slots.

/** Hub codes meaning the account may not connect but is otherwise fine. */
const LAPSE_CODES = [
  // Still active/trialing but past its dated expiry — prepaid crypto and guest trials.
  "SUBSCRIPTION_EXPIRED",
  // No active/trialing row at all — where a lapsed Stripe plan actually lands.
  "NO_ACTIVE_SUBSCRIPTION"
] as const;

export type HubFailureVerdict = "expired" | "signOut" | "passthrough";

export interface HubFailure {
  status: number;
  body: string;
}

/** Just the field that matters here, so callers can pass a full SubscriptionInfo. */
export interface EntitlementView {
  entitled?: boolean;
}

export function isSubscriptionLapseBody(body: string): boolean {
  return LAPSE_CODES.some((code) => body.includes(code));
}

/** @param subscription the hub's verdict, or null if it could not be asked */
export function classifyHubFailure(
  failure: HubFailure,
  subscription: EntitlementView | null
): HubFailureVerdict {
  if (failure.status !== 401 && failure.status !== 403) return "passthrough";
  if (isSubscriptionLapseBody(failure.body)) return "expired";
  // Backstop for a hub code we do not recognise yet. A missing `entitled` means
  // an older hub, which fails open so a paying customer is never locked out.
  if (subscription?.entitled === false) return "expired";
  return "signOut";
}
