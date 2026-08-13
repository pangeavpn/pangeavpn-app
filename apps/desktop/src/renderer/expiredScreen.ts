// Waits for the tunnel to be down: the screen replaces the stage the disconnect
// button lives on, and time can run out mid-session.
export function shouldShowExpiredScreen(entitled: boolean | null, tunnelState: string): boolean {
  if (entitled !== false) return false;
  return tunnelState !== "CONNECTED" && tunnelState !== "CONNECTING" && tunnelState !== "DISCONNECTING";
}
