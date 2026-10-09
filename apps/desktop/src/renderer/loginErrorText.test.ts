import assert from "node:assert/strict";
import test from "node:test";
import { loginErrorText } from "./loginErrorText.ts";
import type { Translate } from "./hubStatusText.ts";
import type { LoginErrorCode } from "../shared/ipc.ts";

// Echoes the key so the assertions pin the mapping, not the English copy.
const t: Translate = (key) => key;

test("every code has its own message", () => {
  const codes: LoginErrorCode[] = [
    "INVALID_ACCOUNT_NUMBER",
    "SUBSCRIPTION_EXPIRED",
    "DEVICE_LIMIT_REACHED",
    "RATE_LIMITED",
    "SERVER_ERROR",
    "HUB_UNREACHABLE",
    "TIMEOUT",
    "REGISTRATION_FAILED",
    "LOCAL_STORAGE_FAILED",
    "UNKNOWN"
  ];
  const keys = codes.map((code) => loginErrorText(code, t));
  assert.equal(new Set(keys).size, codes.length, "codes must not share a message");
});

test("an expired subscription does not say the account number is wrong", () => {
  assert.equal(loginErrorText("SUBSCRIPTION_EXPIRED", t), "login.error.subscriptionExpired");
  assert.notEqual(loginErrorText("SUBSCRIPTION_EXPIRED", t), loginErrorText("INVALID_ACCOUNT_NUMBER", t));
});

test("a reachability failure does not blame the account number either", () => {
  assert.equal(loginErrorText("HUB_UNREACHABLE", t), "login.error.hubUnreachable");
});

test("a missing or unknown code falls back instead of showing a raw code", () => {
  assert.equal(loginErrorText(undefined, t), "login.error.unknown");
  assert.equal(loginErrorText("NOT_A_CODE" as LoginErrorCode, t), "login.error.unknown");
});
