import type { LoginErrorCode } from "../shared/ipc.ts";

// Matched on `name` and `status` rather than instanceof: keeps this pure and
// testable without dragging electron in through pangeaApiClient.
interface ErrorLike {
  name?: unknown;
  message?: unknown;
  status?: unknown;
}

const STATUS_CARRIERS = new Set(["LoginRejectedError", "AuthError"]);

// Every way the platform spells "the bytes never got there". A hub that
// answered has a status, which is checked first.
const TRANSPORT_FAILURE =
  /(fetch failed|ENOTFOUND|ECONNREFUSED|ECONNRESET|ETIMEDOUT|ENETUNREACH|EHOSTUNREACH|EAI_AGAIN|certificate|CERT_|SSL|socket hang up|Hub transport unavailable)/i;

function asErrorLike(err: unknown): ErrorLike | null {
  if (err instanceof Error) return err as ErrorLike;
  return null;
}

function statusOf(err: ErrorLike): number | null {
  if (!STATUS_CARRIERS.has(String(err.name))) return null;
  return typeof err.status === "number" ? err.status : null;
}

function fromStatus(status: number): LoginErrorCode {
  if (status === 429) return "RATE_LIMITED";
  if (status >= 500) return "SERVER_ERROR";
  if (status >= 400) return "INVALID_ACCOUNT_NUMBER";
  return "UNKNOWN";
}

/**
 * Turns whatever the sign-in path threw into one code the UI can explain.
 * Unrecognised failures stay UNKNOWN — guessing here is how an expired
 * subscription ended up telling people their account number was wrong.
 */
export function classifyLoginError(err: unknown): LoginErrorCode {
  const error = asErrorLike(err);
  if (!error) return "UNKNOWN";

  const name = String(error.name ?? "");
  if (name === "SubscriptionExpiredError") return "SUBSCRIPTION_EXPIRED";
  if (name === "HubUnreachableError") return "HUB_UNREACHABLE";

  const status = statusOf(error);
  if (status !== null) return fromStatus(status);

  const message = typeof error.message === "string" ? error.message : "";
  if (name === "AbortError" || /timeout|timed out|aborted/i.test(message)) return "TIMEOUT";
  if (TRANSPORT_FAILURE.test(message)) return "HUB_UNREACHABLE";
  return "UNKNOWN";
}
