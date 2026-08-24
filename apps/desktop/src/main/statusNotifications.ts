// Decides which OS notification a tray-status transition deserves, if any.
// Pure so the transition table is testable without Electron.

export type StatusNotificationKind = "connected" | "reconnecting" | "restored" | "disconnected";

export interface StatusSnapshot {
  state: string;
  reconnecting: boolean;
}

// ERROR covers both a dropped session mid-retry and an unreachable daemon;
// reconnecting covers the daemon rebuilding through CONNECTING.
function troubled(s: StatusSnapshot): boolean {
  return s.state === "ERROR" || s.reconnecting;
}

export function statusNotificationKind(prev: StatusSnapshot, next: StatusSnapshot): StatusNotificationKind | null {
  if (next.state === "CONNECTED" && !next.reconnecting) {
    if (prev.state === "CONNECTED" && !prev.reconnecting) return null;
    return troubled(prev) ? "restored" : "connected";
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
