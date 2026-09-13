import type { LoginErrorCode } from "../shared/ipc.js";
import type { MessageKey } from "./i18n/messages.js";
import type { Translate } from "./hubStatusText.js";

const LOGIN_ERROR_KEYS: Record<LoginErrorCode, MessageKey> = {
  INVALID_ACCOUNT_NUMBER: "login.error.invalidAccountNumber",
  SUBSCRIPTION_EXPIRED: "login.error.subscriptionExpired",
  DEVICE_LIMIT_REACHED: "login.error.deviceLimitReached",
  RATE_LIMITED: "login.error.rateLimited",
  SERVER_ERROR: "login.error.serverError",
  HUB_UNREACHABLE: "login.error.hubUnreachable",
  TIMEOUT: "login.error.timeout",
  REGISTRATION_FAILED: "login.error.registrationFailed",
  UNKNOWN: "login.error.unknown"
};

/** Says why sign-in failed. An unmapped code falls back rather than showing the
 *  raw code — a main process newer than this bundle must not leak one. */
export function loginErrorText(code: LoginErrorCode | undefined, t: Translate): string {
  const key = code ? LOGIN_ERROR_KEYS[code] : undefined;
  return t(key ?? "login.error.unknown");
}
