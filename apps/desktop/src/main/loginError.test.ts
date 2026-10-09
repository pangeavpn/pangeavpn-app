import assert from "node:assert/strict";
import test from "node:test";
import { classifyLoginError } from "./loginError.ts";

function withStatus(name: string, message: string, status: number): Error {
  const err = new Error(message);
  err.name = name;
  (err as Error & { status: number }).status = status;
  return err;
}

function named(name: string, message = ""): Error {
  const err = new Error(message);
  err.name = name;
  return err;
}

test("an expired subscription is never reported as a bad account number", () => {
  assert.equal(
    classifyLoginError(named("SubscriptionExpiredError", "subscription has expired")),
    "SUBSCRIPTION_EXPIRED"
  );
});

test("a cascade that gave up is a reachability failure, not a rejection", () => {
  assert.equal(
    classifyLoginError(named("HubUnreachableError", "Hub unreachable; retrying in 20s")),
    "HUB_UNREACHABLE"
  );
});

test("the hub refusing the credential maps to a bad account number", () => {
  for (const status of [400, 401, 403, 404]) {
    assert.equal(
      classifyLoginError(withStatus("LoginRejectedError", "refused", status)),
      "INVALID_ACCOUNT_NUMBER",
      `status ${status}`
    );
  }
});

test("429 is throttling, not a bad account number", () => {
  assert.equal(classifyLoginError(withStatus("LoginRejectedError", "slow down", 429)), "RATE_LIMITED");
});

test("a 5xx blames our servers, not the user's input", () => {
  for (const status of [500, 502, 503]) {
    assert.equal(
      classifyLoginError(withStatus("LoginRejectedError", "boom", status)),
      "SERVER_ERROR",
      `status ${status}`
    );
  }
});

test("AuthError carries a status too and classifies the same way", () => {
  assert.equal(classifyLoginError(withStatus("AuthError", "unauthorized", 401)), "INVALID_ACCOUNT_NUMBER");
});

test("a timeout is named as one so the user knows to retry", () => {
  assert.equal(classifyLoginError(new Error("Token login request timeout")), "TIMEOUT");
  assert.equal(classifyLoginError(named("AbortError", "The operation was aborted")), "TIMEOUT");
});

test("transport failures are reachability, whatever the platform calls them", () => {
  const messages = [
    "fetch failed",
    "getaddrinfo ENOTFOUND api.pangeavpn.org",
    "connect ECONNREFUSED 10.0.0.1:443",
    "connect ETIMEDOUT",
    "read ECONNRESET",
    "network is unreachable ENETUNREACH",
    "unable to verify the first certificate",
    "Hub transport unavailable: the normal (cleartext domain) method is switched off"
  ];
  for (const message of messages) {
    assert.equal(classifyLoginError(new Error(message)), "HUB_UNREACHABLE", message);
  }
});

test("a status beats a message that merely looks like a network error", () => {
  assert.equal(
    classifyLoginError(withStatus("LoginRejectedError", "fetch failed upstream", 401)),
    "INVALID_ACCOUNT_NUMBER"
  );
});

test("a filesystem errno is reported as a local storage failure", () => {
  for (const code of ["EACCES", "EPERM", "EROFS", "ENOSPC", "EDQUOT", "EIO", "EBUSY"]) {
    const err = new Error("write failed") as Error & { code: string };
    err.code = code;
    assert.equal(classifyLoginError(err), "LOCAL_STORAGE_FAILED", code);
  }
});

test("a network errno is reachability, never local storage", () => {
  for (const code of ["ECONNREFUSED", "ENOTFOUND", "ETIMEDOUT", "ECONNRESET", "ENETUNREACH", "EHOSTUNREACH", "EAI_AGAIN"]) {
    const err = new Error("connect failed") as Error & { code: string };
    err.code = code;
    assert.notEqual(classifyLoginError(err), "LOCAL_STORAGE_FAILED", code);
  }
});

test("a status still wins over a filesystem errno", () => {
  const err = withStatus("LoginRejectedError", "refused", 401) as Error & { code: string };
  err.code = "EACCES";
  assert.equal(classifyLoginError(err), "INVALID_ACCOUNT_NUMBER");
});

test("a plain object carrying an errno is still UNKNOWN", () => {
  assert.equal(classifyLoginError({ code: "ENOSPC" }), "UNKNOWN");
});

test("anything unrecognised stays UNKNOWN rather than guessing", () => {
  assert.equal(classifyLoginError(new Error("something else entirely")), "UNKNOWN");
  assert.equal(classifyLoginError(null), "UNKNOWN");
  assert.equal(classifyLoginError("a thrown string"), "UNKNOWN");
  assert.equal(classifyLoginError({ status: 401 }), "UNKNOWN");
});

test("a non-numeric status is ignored rather than trusted", () => {
  const err = new Error("weird");
  (err as Error & { status: unknown }).status = "401";
  assert.equal(classifyLoginError(err), "UNKNOWN");
});
