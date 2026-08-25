import test from "node:test";
import assert from "node:assert/strict";
import { statusNotificationKind } from "./statusNotifications.ts";

const at = (state: string, reconnecting = false) => ({ state, reconnecting });

test("announces a fresh connection", () => {
  assert.equal(statusNotificationKind(at("CONNECTING"), at("CONNECTED")), "connected");
  assert.equal(statusNotificationKind(at("DISCONNECTED"), at("CONNECTED")), "connected");
});

test("stays quiet while nothing changes", () => {
  assert.equal(statusNotificationKind(at("CONNECTED"), at("CONNECTED")), null);
  assert.equal(statusNotificationKind(at("DISCONNECTED"), at("DISCONNECTED")), null);
  assert.equal(statusNotificationKind(at("ERROR"), at("ERROR")), null);
});

test("announces a lost connection once, not on every retry", () => {
  assert.equal(statusNotificationKind(at("CONNECTED"), at("ERROR")), "reconnecting");
  assert.equal(statusNotificationKind(at("CONNECTED"), at("CONNECTING", true)), "reconnecting");
  assert.equal(statusNotificationKind(at("ERROR"), at("CONNECTING", true)), null);
  assert.equal(statusNotificationKind(at("CONNECTING", true), at("ERROR")), null);
});

test("announces recovery as back online rather than a plain connect", () => {
  assert.equal(statusNotificationKind(at("ERROR"), at("CONNECTED")), "restored");
  assert.equal(statusNotificationKind(at("CONNECTING", true), at("CONNECTED")), "restored");
  assert.equal(statusNotificationKind(at("CONNECTED", true), at("CONNECTED")), "restored");
});

test("announces a deliberate disconnect", () => {
  assert.equal(statusNotificationKind(at("CONNECTED"), at("DISCONNECTED")), "disconnected");
  assert.equal(statusNotificationKind(at("DISCONNECTING"), at("DISCONNECTED")), "disconnected");
});

test("announces a blocking kill switch found on the first snapshot", () => {
  assert.equal(statusNotificationKind(null, { state: "ERROR", reconnecting: false, killSwitchActive: true }), "blocking");
  assert.equal(statusNotificationKind(null, { state: "CONNECTING", reconnecting: true, killSwitchActive: true }), "blocking");
});

test("stays quiet on a first snapshot that is not blocking traffic", () => {
  assert.equal(statusNotificationKind(null, at("DISCONNECTED")), null);
  assert.equal(statusNotificationKind(null, at("CONNECTED")), null);
  assert.equal(statusNotificationKind(null, at("ERROR")), null);
  assert.equal(statusNotificationKind(null, { state: "CONNECTED", reconnecting: false, killSwitchActive: true }), null);
});

test("never announces the end of a session the user never had", () => {
  assert.equal(statusNotificationKind(at("CONNECTING"), at("DISCONNECTED")), null);
  assert.equal(statusNotificationKind(at("ERROR"), at("DISCONNECTED")), null);
  assert.equal(statusNotificationKind(at("CONNECTING"), at("ERROR")), null);
});
