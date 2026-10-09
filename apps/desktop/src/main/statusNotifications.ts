// Decides which OS notification a tray-status transition deserves, if any.
// Pure so the transition table is testable without Electron.

export type StatusNotificationKind = "connected" | "reconnecting" | "restored" | "disconnected" | "blocking";

export interface StatusSnapshot {
  state: string;
  reconnecting: boolean;
  killSwitchActive?: boolean;
}

// ERROR covers both a dropped session mid-retry and an unreachable daemon;
// reconnecting covers the daemon rebuilding through CONNECTING.
function troubled(s: StatusSnapshot): boolean {
  return s.state === "ERROR" || s.reconnecting;
}

// The daemon is fail-closed with no tunnel up: to a user who can't see why,
// this is indistinguishable from broken internet, so it must be announced.
function blocking(s: StatusSnapshot | null): boolean {
  return s !== null && troubled(s) && s.killSwitchActive === true;
}

export function statusNotificationKind(prev: StatusSnapshot | null, next: StatusSnapshot): StatusNotificationKind | null {
  if (next.state === "CONNECTED" && !next.reconnecting) {
    if (!prev) return null;
    if (prev.state === "CONNECTED" && !prev.reconnecting) return null;
    return troubled(prev) ? "restored" : "connected";
  }
  // First sight of an already-blocking daemon (app start into crash recovery):
  // the only chance to say the kill switch, not the ISP, is holding traffic.
  if (!prev) {
    return blocking(next) ? "blocking" : null;
  }
  if (troubled(next) && prev.state === "CONNECTED" && !troubled(prev)) {
    return "reconnecting";
  }
  // Only a session the user actually had is worth announcing the end of; a
  // failed or cancelled connect never notifies.
  if (next.state === "DISCONNECTED" && (prev.state === "CONNECTED" || prev.state === "DISCONNECTING")) {
    return "disconnected";
  }
  return null;
}
