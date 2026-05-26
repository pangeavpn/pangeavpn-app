import { networkInterfaces } from "node:os";

type Listener = (signature: string) => void;

const POLL_INTERVAL_MS = 3000;

function computeSignature(): string {
  const ifaces = networkInterfaces();
  const parts: string[] = [];
  for (const name of Object.keys(ifaces).sort()) {
    const addrs = ifaces[name];
    if (!addrs) continue;
    for (const addr of addrs) {
      if (addr.internal) continue;
      if (addr.family !== "IPv4" && addr.family !== "IPv6") continue;
      // Skip tunnel-like interfaces so VPN bring-up/tear-down doesn't itself
      // count as a network change (would cause reconnect loops).
      const lower = name.toLowerCase();
      if (lower.startsWith("tun") || lower.startsWith("utun") || lower.startsWith("wg") || lower.startsWith("pangea")) {
        continue;
      }
      parts.push(`${name}:${addr.address}`);
    }
  }
  return parts.join("|");
}

let timer: NodeJS.Timeout | null = null;
let lastSignature = "";
const listeners = new Set<Listener>();

export function startNetworkWatcher(): void {
  if (timer) return;
  lastSignature = computeSignature();
  timer = setInterval(() => {
    const sig = computeSignature();
    if (sig === lastSignature) return;
    const prev = lastSignature;
    lastSignature = sig;
    if (prev === "") return; // ignore the very first transition out of "no interfaces"
    for (const fn of listeners) {
      try {
        fn(sig);
      } catch (err) {
        console.warn("network watcher listener failed", err);
      }
    }
  }, POLL_INTERVAL_MS);
  if (typeof timer.unref === "function") timer.unref();
}

export function stopNetworkWatcher(): void {
  if (!timer) return;
  clearInterval(timer);
  timer = null;
}

export function onNetworkChange(fn: Listener): () => void {
  listeners.add(fn);
  return () => listeners.delete(fn);
}
