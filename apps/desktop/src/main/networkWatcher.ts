import { networkInterfaces } from "node:os";

type Listener = (signature: string) => void;

const POLL_INTERVAL_MS = 3000;

// Expands an IPv6 address (which may use "::" shorthand) into its 8 groups.
function expandIPv6Groups(address: string): string[] {
  const withoutZone = address.split("%")[0];
  const [head, tail] = withoutZone.split("::");
  const headParts = head ? head.split(":") : [];
  const tailParts = tail ? tail.split(":") : [];
  const missing = 8 - headParts.length - tailParts.length;
  const zeros = withoutZone.includes("::") ? Array(Math.max(missing, 0)).fill("0") : [];
  return [...headParts, ...zeros, ...tailParts];
}

// Masks to the /64 prefix so SLAAC privacy-address rotation (which only
// changes the low 64 bits) doesn't look like a network change.
function ipv6NetworkPrefix(address: string): string {
  return expandIPv6Groups(address).slice(0, 4).join(":");
}

function isLinkLocalOrMulticast(addr: { family: string; address: string }): boolean {
  const lower = addr.address.toLowerCase();
  if (addr.family === "IPv6") return lower.startsWith("fe80") || lower.startsWith("ff");
  return lower.startsWith("169.254.");
}

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
      if (isLinkLocalOrMulticast(addr)) continue;
      const token = addr.family === "IPv6" ? ipv6NetworkPrefix(addr.address) : addr.address;
      parts.push(`${name}:${token}`);
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
    lastSignature = sig;
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
