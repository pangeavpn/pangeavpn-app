import { generateKeyPairSync } from "node:crypto";
import https from "node:https";
import { net } from "electron";
import type { Profile } from "@pangeavpn/shared-types";
import type { ServerInfo, SubscriptionInfo } from "../shared/ipc";
import { normalizeCustomDns, resolveWireGuardDns } from "../shared/dns";
import { MTU_DEFAULT, normalizeMtu, normalizeMtuOrDefault } from "../shared/mtu";
import { resolveNaiveEndpoint } from "../shared/naiveEndpoint";
import { parseNodeWireGuardEndpoint } from "../shared/wireguardEndpoint";
import { buildShadowsocksProfile } from "../shared/shadowsocksProfile";
import {
  DEFAULT_HUB_METHODS,
  type HubMethod,
  type HubMethods,
  type HubMethodTestResult,
  type HubStatus
} from "../shared/hubMethods";
import {
  firstWorkingCreds,
  mergeAdvertisedCreds,
  promoteCreds,
  restoreCachedCreds,
  type HubShadowsocksCreds
} from "../shared/hubShadowsocksCreds";
import {
  mergeFrontedEndpoints,
  promoteFrontedEndpoint,
  seedFrontedEndpoints
} from "../shared/frontedEndpoints";
import { HUB_HOSTNAME, normalHubHosts } from "../shared/hubHosts";
import { isCachedServer, restoreCachedServers } from "../shared/cachedServers";
import { isIPv4Literal } from "../shared/ipLiteral";
import { DEAD_DROP_KEYS } from "./deadDropKeys";
import { deadDropDue, fetchDeadDropPayload } from "./deadDrop";
import {
  cachedEntitlement,
  restoreCachedSubscription,
  type CachedSubscription
} from "../shared/cachedSubscription";
import { encryptRequest, decryptResponse, type EncryptedResponse } from "./secureChannel";
import { sanitizeLog } from "./logSanitize";
import { DOH_TLS_OPTIONS, fetchViaConnectProxy } from "./hubTransport";

export class AuthError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "AuthError";
    this.status = status;
  }
}

/**
 * The account is fine, the subscription ran out.
 *
 * Deliberately NOT an AuthError: the handlers treat those as "this identity is
 * no longer valid" and call auth.logout(), which clears the saved token AND the
 * identity keypair. A new keypair registers as a NEW device, so treating an
 * expiry that way would burn one of the account's device slots every time a
 * prepaid customer lapsed and topped up — and crypto accounts lapse by design.
 * The user stays signed in and simply cannot connect until they pay.
 */
export class SubscriptionExpiredError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "SubscriptionExpiredError";
  }
}

/**
 * The user pressed Stop while this request was in flight. Not a failure — the
 * caller unwinds to an idle UI with no error toast.
 */
export class ConnectCancelledError extends Error {
  constructor() {
    super("Connect cancelled");
    this.name = "ConnectCancelledError";
  }
}

/** Every path to the hub failed. Thrown fast during the cooldown that follows,
 *  instead of making each caller re-run the whole probe cascade. */
export class HubUnreachableError extends Error {
  constructor(retryInMs: number) {
    super(`Hub unreachable; retrying in ${Math.ceil(retryInMs / 1000)}s`);
    this.name = "HubUnreachableError";
  }
}

// How long a fully failed cascade is believed before it is attempted again.
const HUB_RETRY_COOLDOWN_MS = 20000;
// Consecutive hubFetch failures tolerated on a resolved path before it is
// invalidated and the connection cascade is forced to re-run.
const HUB_FAILURE_LIMIT = 2;

/** Settles as soon as `signal` aborts, however long `work` still has to run. */
function raceAbort<T>(work: Promise<T>, signal?: AbortSignal): Promise<T> {
  if (!signal) return work;
  if (signal.aborted) return Promise.reject(new ConnectCancelledError());
  return new Promise<T>((resolve, reject) => {
    const onAbort = (): void => reject(new ConnectCancelledError());
    signal.addEventListener("abort", onAbort, { once: true });
    work.then(resolve, reject).finally(() => signal.removeEventListener("abort", onAbort));
  });
}


// DoH providers — all accessed by IP to avoid SNI-based blocking
const DOH_PROVIDERS = [
  { url: "https://1.1.1.1/dns-query", accept: "application/dns-json" },           // Cloudflare
  { url: "https://8.8.8.8/resolve", accept: "application/dns-json" },              // Google
  { url: "https://9.9.9.9:5053/dns-query", accept: "application/dns-json" },       // Quad9
  { url: "https://94.140.14.14/dns-query", accept: "application/dns-json" },       // AdGuard
];

interface BootstrapResponse {
  vpnAccessToken: string;
  servers: ServerInfo[];
  /** Edge relays the hub currently runs. Absent on hubs that predate them. */
  frontedEndpoints?: string[];
}

interface TokenLoginResponse {
  vpnAccessToken: string;
  user: { email: string; name: string };
  servers: ServerInfo[];
  frontedEndpoints?: string[];
}

interface RegisterResponse {
  serverPubkey: string;
  /** The node's own WireGuard listener, host:port. */
  serverEndpoint: string;
  assignedIP: string;
  dns: string;
  existingConfig?: boolean;
}

interface DohAnswer {
  type: number;
  data: string;
}

interface DohResponse {
  Answer?: DohAnswer[];
}

// DNS record types this client trusts as a resolved address, not a further
// name to chase (A, AAAA) — a CNAME in the same array must never be used.
const DOH_ADDRESS_RECORD_TYPES = new Set([1, 28]);

/** Try a single DoH provider */
async function tryDoHProvider(providerUrl: string, accept: string, hostname: string): Promise<string | null> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 3000);
  try {
    const sep = providerUrl.includes("?") ? "&" : "?";
    const response = await fetch(`${providerUrl}${sep}name=${hostname}&type=A`, {
      headers: { accept },
      signal: controller.signal
    });
    if (!response.ok) {
      console.log(`[DoH] ${providerUrl} returned ${response.status}`);
      return null;
    }
    const data = (await response.json()) as DohResponse;
    const answers = data.Answer?.filter((a) => a.data && DOH_ADDRESS_RECORD_TYPES.has(a.type) && isIPv4Literal(a.data)) ?? [];
    if (answers.length > 0) {
      console.log(`[DoH] ${providerUrl} resolved ${hostname} → ${sanitizeLog(answers[0].data)}`);
      return answers[0].data;
    }
    console.log(`[DoH] ${providerUrl} returned no usable A/AAAA answers for ${hostname}`);
    return null;
  } catch (err) {
    console.log(`[DoH] ${providerUrl} failed: ${sanitizeLog(err)}`);
    return null;
  } finally {
    clearTimeout(timer);
  }
}

/** Resolve hostname via DNS-over-HTTPS, trying multiple providers */
async function resolveViaDoH(hostname: string): Promise<string | null> {
  console.log(`[DoH] Resolving ${hostname} via ${DOH_PROVIDERS.length} providers...`);
  for (const provider of DOH_PROVIDERS) {
    const ip = await tryDoHProvider(provider.url, provider.accept, hostname);
    if (ip) return ip;
  }
  console.log(`[DoH] All providers failed for ${hostname}`);
  return null;
}

/**
 * Deduplicated, non-empty entries, order preserved. Used for the node endpoint
 * addresses handed to the daemon, which come from the hub and can repeat when
 * several transports share the node's IP.
 */
function uniqueNonEmpty(values: (string | undefined | null)[]): string[] {
  const out: string[] = [];
  for (const value of values) {
    const trimmed = value?.trim();
    if (trimmed && !out.includes(trimmed)) out.push(trimmed);
  }
  return out;
}

// Bounds how much an unauthenticated-until-decrypted peer can stream into the
// main process before this client gives up on it.
const MAX_RESPONSE_BYTES = 8 * 1024 * 1024;

// Empty SNI and no cert check are deliberate: every call posts a sealed
// /v1/secure envelope, so TLS is carrier only. Do not "harden" this.
function fetchDohResolved(
  ip: string,
  hostname: string,
  requestPath: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
    timeoutMs?: number;
    signal?: AbortSignal;
  }
): Promise<Response> {
  const deadline = options.timeoutMs ?? 15000;
  return new Promise<Response>((resolve, reject) => {
    let settled = false;
    const fail = (err: Error): void => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", onAbort);
      req.destroy();
      reject(err);
    };
    const timer = setTimeout(() => fail(new Error("Request timeout")), deadline);
    const onAbort = (): void => fail(new Error("Request aborted"));
    if (options.signal) {
      if (options.signal.aborted) {
        // req isn't defined yet; queue the abort for right after construction.
        queueMicrotask(() => fail(new Error("Request aborted")));
      } else {
        options.signal.addEventListener("abort", onAbort, { once: true });
      }
    }

    const req = https.request(
      {
        hostname: ip,
        port: 443,
        path: requestPath,
        method: options.method ?? "GET",
        headers: {
          ...options.headers,
          Host: hostname
        },
        ...DOH_TLS_OPTIONS
      },
      (res) => {
        const chunks: Buffer[] = [];
        let received = 0;
        res.on("data", (chunk: Buffer) => {
          received += chunk.length;
          if (received > MAX_RESPONSE_BYTES) {
            fail(new Error("Response too large"));
            return;
          }
          chunks.push(chunk);
        });
        res.on("end", () => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
          options.signal?.removeEventListener("abort", onAbort);
          const body = Buffer.concat(chunks).toString("utf8");
          const headers = new Headers();
          for (const [key, value] of Object.entries(res.headers)) {
            if (value) {
              headers.set(key, Array.isArray(value) ? value.join(", ") : value);
            }
          }
          resolve(new Response(body, { status: res.statusCode ?? 500, headers }));
        });
      }
    );

    req.on("error", fail);

    if (options.body) {
      req.write(options.body);
    }
    req.end();
  });
}

/**
 * POST the envelope to an edge relay, which forwards it to the hub.
 *
 * Unlike fetchDohResolved this validates the certificate normally: the relay is
 * reached by its own name on shared CDN address space, so its certificate is a
 * real one for that name and there is no reason to accept anything less. That
 * the relay itself is untrusted costs nothing — it carries a sealed envelope
 * (see secureChannel), so it forwards ciphertext it cannot read and cannot
 * forge a reply to.
 */
function fetchFronted(
  host: string,
  requestPath: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
    timeoutMs?: number;
    signal?: AbortSignal;
  }
): Promise<Response> {
  if (options.signal?.aborted) {
    return Promise.reject(new ConnectCancelledError());
  }
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs ?? 15000);
  const onExternalAbort = () => controller.abort();
  options.signal?.addEventListener("abort", onExternalAbort, { once: true });

  return net
    .fetch(`https://${host}${requestPath}`, {
      method: options.method ?? "GET",
      headers: options.headers,
      body: options.body,
      signal: controller.signal
    })
    .finally(() => {
      clearTimeout(timer);
      options.signal?.removeEventListener("abort", onExternalAbort);
    });
}

function generateWireGuardKeyPair(): { privateKey: string; publicKey: string } {
  const { publicKey, privateKey } = generateKeyPairSync("x25519");
  const privDer = privateKey.export({ type: "pkcs8", format: "der" }) as Buffer;
  const pubDer = publicKey.export({ type: "spki", format: "der" }) as Buffer;
  return {
    privateKey: privDer.subarray(16).toString("base64"),
    publicKey: pubDer.subarray(12).toString("base64")
  };
}

interface CidrBlock {
  /** Network address as a 32-bit unsigned integer. */
  base: number;
  /** Prefix length, 0-32. */
  prefixLen: number;
}

function ipToInt(ip: string): number {
  const parts = ip.split(".").map(Number);
  return ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0;
}

function intToIp(n: number): string {
  return [(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff].join(".");
}

/** True if `ip` (as a 32-bit uint) falls inside `block`. */
function cidrContains(block: CidrBlock, ip: number): boolean {
  if (block.prefixLen === 0) return true;
  const maskBits = 32 - block.prefixLen;
  const mask = (0xffffffff << maskBits) >>> 0;
  return (ip & mask) >>> 0 === (block.base & mask) >>> 0;
}

/**
 * Split a CIDR block into the set of sibling sub-blocks that together cover
 * `block` minus the single host `ip` (which must lie within `block`).
 *
 * Same bit-halving technique as the original single-IP implementation, but
 * generalized to start from an arbitrary block instead of hardcoding
 * 0.0.0.0/0 — this is what lets `allowedIPsExcludingAll` recurse into an
 * already-split range when a second excluded IP lands inside it.
 */
function splitBlockExcluding(block: CidrBlock, ip: number): CidrBlock[] {
  const ranges: CidrBlock[] = [];
  let base = block.base;

  for (let prefixLen = block.prefixLen + 1; prefixLen <= 32; prefixLen++) {
    const bit = 32 - prefixLen;
    const mask = (1 << bit) >>> 0;
    const ipBitSet = (ip & mask) !== 0;

    // Sibling subnet: the half that does NOT contain the excluded IP
    const sibling = ipBitSet ? base : (base | mask) >>> 0;
    ranges.push({ base: sibling, prefixLen });

    if (ipBitSet) {
      base = (base | mask) >>> 0;
    }
  }

  return ranges;
}

/**
 * Calculate AllowedIPs CIDRs that cover 0.0.0.0/0 minus every IP in
 * `excludeIPs`. Each transport (Cloak always; NaiveProxy and Hysteria2 when
 * configured) makes its own outbound connection to its own remote host
 * *outside* the WireGuard tunnel (the tunnel's Endpoint is always
 * 127.0.0.1:<local transport port>) — every one of those remote hosts must
 * be excluded from AllowedIPs, or the OS routing table would try to route
 * the transport's own connection attempt back through the tunnel it's still
 * establishing (a fatal routing loop).
 *
 * Approach: start with the whole address space as a single range
 * (0.0.0.0/0) and, for each IP to exclude, subtract it from every range
 * currently in the list — a range that doesn't contain the IP passes
 * through unchanged; a range that does gets replaced by its sibling
 * sub-blocks via splitBlockExcluding. Duplicate excluded IPs are handled
 * safely: the second occurrence finds its /32 already isolated and
 * splitBlockExcluding returns no further sub-blocks for it (a /32 block has
 * no room to halve further), which is exactly "no-op, stays excluded".
 */
function allowedIPsExcludingAll(excludeIPs: string[]): string[] {
  let blocks: CidrBlock[] = [{ base: 0, prefixLen: 0 }];

  for (const excludeIP of excludeIPs) {
    const ip = ipToInt(excludeIP);
    const nextBlocks: CidrBlock[] = [];
    for (const block of blocks) {
      if (cidrContains(block, ip)) {
        nextBlocks.push(...splitBlockExcluding(block, ip));
      } else {
        nextBlocks.push(block);
      }
    }
    blocks = nextBlocks;
  }

  return blocks.map((b) => `${intToIp(b.base)}/${b.prefixLen}`);
}

function buildWireGuardConfig(
  privateKey: string,
  assignedIP: string,
  dns: string,
  serverPubkey: string,
  cloakLocalPort: number,
  excludeIPs: string[],
  mtu: number
): string {
  const allowedIPs = allowedIPsExcludingAll(excludeIPs).join(", ");

  return [
    "[Interface]",
    `PrivateKey = ${privateKey}`,
    `Address = ${assignedIP}/32`,
    `DNS = ${dns}`,
    `MTU = ${normalizeMtuOrDefault(mtu)}`,
    "",
    "[Peer]",
    `PublicKey = ${serverPubkey}`,
    `Endpoint = 127.0.0.1:${cloakLocalPort}`,
    `AllowedIPs = ${allowedIPs}`,
    "PersistentKeepalive = 25"
  ].join("\n");
}

interface DeviceRegisterResponse {
  deviceId: string;
  assignedIp: string;
  friendlyName?: string | null;
}

export interface DeviceInfo {
  id: string;
  /** Absent on hubs older than the rename feature. */
  identityPubkey?: string | null;
  friendlyName: string | null;
  createdAt: string;
  status: string;
}

/**
 * The daemon-side Shadowsocks proxy ensureHub routes the secure envelope
 * through. Injected so the client keeps no daemon dependency of its own.
 */
/** One fully-specified way to reach the hub, holding no instance state, so a
 *  probe can run against a path the client is not currently using. */
type HubProbePath =
  | { kind: "ip"; ip: string }
  | { kind: "proxy"; port: number; username?: string; password?: string }
  | { kind: "fronted"; host: string }
  | { kind: "domain"; host: string };

/** A test result before the method name and elapsed time are stamped on. */
type HubMethodOutcome = Omit<HubMethodTestResult, "method" | "ms">;

export interface ShadowsocksHubProxy {
  /** Resolves to the proxy's loopback port and CONNECT auth, or null when unavailable. */
  start(creds: HubShadowsocksCreds): Promise<{ port: number; proxyUsername: string; proxyPassword: string } | null>;
  stop(): Promise<void>;
}

export class PangeaApiClient {
  private readonly timeoutMs: number;
  // The last node list the hub gave us, restored from settings.json at startup.
  // Persisted because a client that cannot reach the hub still needs somewhere
  // to connect: without this a cold start behind a block has no server to name,
  // so the picker is empty and the tray has no retry plan to build.
  private cachedServers: ServerInfo[] = [];
  private onServers: ((servers: ServerInfo[]) => void) | null = null;
  private licenseKey: string | null = null;
  private dohEnabled = true;
  // Which paths to the hub are permitted, in ensureHub's attempt order.
  private hubMethods: HubMethods = { ...DEFAULT_HUB_METHODS };
  private normalHost: string = HUB_HOSTNAME;
  private wireguardMtu = MTU_DEFAULT;
  private customDnsServers: string[] | null = null;
  identityPubkey: string | null = null;

  // When DoH resolves an IP, we store it here and use fetchDohResolved()
  // to connect directly with no SNI (invisible to DPI).
  private dohResolvedIp: string | null = null;
  private hubReady = false;

  // Last hub IP that worked, restored from settings at startup. It is the only
  // path to the hub while a Lockdown kill switch is engaged: the lock permits
  // this IP (the daemon is told to) but blocks DNS and DoH, so without it the
  // client could never re-learn where the hub is and provisioning would fail
  // with EACCES. Survives resetHubResolution() — it is a cache, not a result.
  private cachedHubIp: string | null = null;
  private onHubIpResolved: ((ip: string) => void) | null = null;

  // Loopback port of the daemon's Shadowsocks hub proxy while it is carrying
  // our traffic; null when that method is off or not currently in use.
  private ssProxyPort: number | null = null;
  private ssProxyUsername: string | null = null;
  private ssProxyPassword: string | null = null;
  private shadowsocksHubProxy: ShadowsocksHubProxy | null = null;
  // Control-plane credentials from the last good /api/client/regions, restored
  // from settings.json at startup — a cold install behind a block has none.
  // Every node the hub named, not just one: a node whose key has rotated past
  // this cache is useless, and the others are the only way back to the hub.
  private hubShadowsocks: HubShadowsocksCreds[] = [];
  private onHubShadowsocks: ((creds: HubShadowsocksCreds[]) => void) | null = null;

  // Edge relays that will forward the envelope, restored from settings.json and
  // refreshed from whatever the hub advertises. Empty until one is configured,
  // which makes the fronted method a no-op ensureHub skips over — the same way
  // the shadowsocks method sits inert until credentials are cached.
  private frontedEndpoints: string[] = seedFrontedEndpoints(null);
  private onFrontedEndpoints: ((endpoints: string[]) => void) | null = null;

  // Signed public file of last resort: addresses to try when every carrier has
  // failed. Inbound only — it re-seeds this cache, it never carries a request.
  private deadDropEnabled = true;
  private deadDropSeq = 0;
  private deadDropLastAttemptMs = 0;
  private onDeadDropState: ((state: { seq: number; lastAttemptMs: number }) => void) | null = null;
  // The relay currently carrying our traffic; null when the method is off or
  // not the path in use.
  private frontedHost: string | null = null;

  // Stands between an unreachable hub and a paying user being told they have
  // no subscription. Restored from settings.json at startup.
  private cachedSubscription: CachedSubscription | null = null;
  private onSubscription: ((cached: CachedSubscription | null) => void) | null = null;

  // The method whose probe last won, and the address it won on — diagnostics
  // for the settings pane, never an input to any decision.
  private activeHubMethod: HubMethod | null = null;
  private activeHubDetail: string | null = null;
  private onHubStatus: ((status: HubStatus) => void) | null = null;
  // One test at a time: the shadowsocks probe borrows the daemon's single
  // proxy, so two overlapping tests would fight over it.
  private hubTestInFlight = false;

  // One shared cascade, so concurrent callers don't each pay ensureHub's full
  // probe sequence against a hub that is down.
  private hubResolution: Promise<void> | null = null;
  private hubResolutionFailedAtMs = 0;
  // Consecutive hubFetch failures on the currently-resolved path. Once this
  // hits HUB_FAILURE_LIMIT the path is presumed stale (IP rotation, moved
  // network) and hubReady is cleared so ensureHub re-runs the cascade.
  private hubFailureStreak = 0;

  constructor() {
    this.timeoutMs = 15000;
  }

  setDohEnabled(enabled: boolean): void {
    this.dohEnabled = enabled;
    this.resetHubResolution();
  }

  isDohEnabled(): boolean {
    return this.dohEnabled;
  }

  /**
   * Replaces which hub-connection methods ensureHub may attempt. The
   * at-least-one-enabled invariant is enforced by the caller (see
   * shared/hubMethods.ts); this only stores what it is given.
   */
  setHubMethods(methods: HubMethods): void {
    this.hubMethods = { ...methods };
    this.resetHubResolution();
  }

  getHubMethods(): HubMethods {
    return { ...this.hubMethods };
  }

  /** The switches plus which method is carrying hub traffic right now. */
  getHubStatus(): HubStatus {
    return {
      methods: { ...this.hubMethods },
      active: this.activeHubMethod,
      detail: this.activeHubDetail
    };
  }

  /** Called whenever the path in use changes, so the UI can follow it. */
  onHubStatusChanged(fn: ((status: HubStatus) => void) | null): void {
    this.onHubStatus = fn;
  }

  private setActiveHubMethod(method: HubMethod | null, detail: string | null): void {
    if (this.activeHubMethod === method && this.activeHubDetail === detail) return;
    this.activeHubMethod = method;
    this.activeHubDetail = detail;
    this.onHubStatus?.(this.getHubStatus());
  }

  /**
   * Supplies the Shadowsocks hub proxy. Until one is wired in, the shadowsocks
   * method is a no-op that ensureHub skips over.
   */
  setShadowsocksHubProxy(proxy: ShadowsocksHubProxy | null): void {
    this.shadowsocksHubProxy = proxy;
    this.resetHubResolution();
  }

  /** The proxy port currently carrying hub traffic, for diagnostics. */
  getShadowsocksProxyPort(): number | null {
    return this.ssProxyPort;
  }

  /** Restores cached control-plane credentials at startup. */
  setCachedHubShadowsocks(stored: unknown): void {
    this.hubShadowsocks = restoreCachedCreds(stored);
  }

  getCachedHubShadowsocks(): HubShadowsocksCreds[] {
    return this.hubShadowsocks;
  }

  /** Called when fresh credentials arrive, so the caller can persist them. */
  onHubShadowsocksResolved(fn: (creds: HubShadowsocksCreds[]) => void): void {
    this.onHubShadowsocks = fn;
  }

  /** Restores the cached node list at startup. */
  setCachedServers(stored: unknown): void {
    this.cachedServers = restoreCachedServers(stored);
  }

  getCachedServers(): ServerInfo[] {
    return [...this.cachedServers];
  }

  /** Called when a fresh node list arrives, so the caller can persist it. */
  onServersResolved(fn: (servers: ServerInfo[]) => void): void {
    this.onServers = fn;
  }

  /** Restores cached edge relays at startup. */
  setCachedFrontedEndpoints(stored: unknown): void {
    this.frontedEndpoints = seedFrontedEndpoints(stored);
    this.resetHubResolution();
  }

  getCachedFrontedEndpoints(): string[] {
    return [...this.frontedEndpoints];
  }

  /** Called when a fresh relay list arrives, so the caller can persist it. */
  onFrontedEndpointsResolved(fn: (endpoints: string[]) => void): void {
    this.onFrontedEndpoints = fn;
  }

  /** Restore the dead-drop switch and replay guard from settings at startup. */
  setDeadDropEnabled(enabled: boolean): void {
    this.deadDropEnabled = enabled;
  }

  getDeadDropEnabled(): boolean {
    return this.deadDropEnabled;
  }

  setDeadDropState(seq: unknown, lastAttemptMs: unknown): void {
    this.deadDropSeq = typeof seq === "number" && Number.isSafeInteger(seq) && seq >= 0 ? seq : 0;
    this.deadDropLastAttemptMs =
      typeof lastAttemptMs === "number" && Number.isFinite(lastAttemptMs) && lastAttemptMs >= 0
        ? lastAttemptMs
        : 0;
  }

  onDeadDropStateChanged(fn: (state: { seq: number; lastAttemptMs: number }) => void): void {
    this.onDeadDropState = fn;
  }

  /** The relay currently carrying hub traffic, for diagnostics. */
  getFrontedHost(): string | null {
    return this.frontedHost;
  }

  /**
   * Returns the value actually stored, which differs from the requested one when
   * that was rejected. Invalid input leaves the current value alone rather than
   * snapping back to the default — a typo shouldn't discard a deliberate choice.
   */
  setWireguardMtu(mtu: unknown): number {
    const normalized = normalizeMtu(mtu);
    if (normalized !== null) this.wireguardMtu = normalized;
    return this.wireguardMtu;
  }

  getWireguardMtu(): number {
    return this.wireguardMtu;
  }

  setCustomDns(value: unknown): string[] {
    const normalized = normalizeCustomDns(value);
    if (normalized === null) {
      throw new TypeError("Custom DNS must contain only IPv4 addresses");
    }
    this.customDnsServers = normalized.length > 0 ? normalized : null;
    return this.getCustomDns();
  }

  getCustomDns(): string[] {
    return this.customDnsServers ? [...this.customDnsServers] : [];
  }

  /** Seed the last known good hub IP (from settings) at startup. */
  setCachedHubIp(ip: unknown): void {
    this.cachedHubIp = typeof ip === "string" && isIPv4Literal(ip.trim()) ? ip.trim() : null;
  }

  /**
   * The hub's IP on the current path, or the last known good one. The daemon
   * needs it to punch a control-plane hole in an engaged Lockdown lock.
   */
  getHubIp(): string | null {
    return this.dohResolvedIp ?? this.cachedHubIp;
  }

  /** Called whenever a new hub IP proves to work, so it can be persisted. */
  onHubIp(listener: (ip: string) => void): void {
    this.onHubIpResolved = listener;
  }

  private rememberHubIp(ip: string): void {
    if (this.cachedHubIp === ip) return;
    this.cachedHubIp = ip;
    this.onHubIpResolved?.(ip);
  }

  setLicenseKey(key: string): void {
    this.licenseKey = key.trim();
  }

  /** Rejects a hub response that reports success but omits a usable token,
   *  rather than silently downgrading every later call to unauthenticated. */
  private acceptLicenseKey(vpnAccessToken: unknown, context: string): string {
    if (typeof vpnAccessToken !== "string" || vpnAccessToken.trim().length === 0) {
      throw new Error(`${context} returned no usable access token`);
    }
    this.licenseKey = vpnAccessToken;
    return vpnAccessToken;
  }

  getLicenseKey(): string | null {
    return this.licenseKey;
  }

  private resetHubResolution(): void {
    this.dohResolvedIp = null;
    this.frontedHost = null;
    this.hubReady = false;
    this.setActiveHubMethod(null, null);
    // Every caller of this is a deliberate change of plan, so the cooldown from
    // the last failure must not hold the new one back.
    this.hubResolutionFailedAtMs = 0;
    // Drop the proxy too: the next ensureHub re-decides whether to use it, and
    // leaving it running would keep a listener open for a path we abandoned.
    if (this.ssProxyPort !== null) {
      this.ssProxyPort = null;
      this.ssProxyUsername = null;
      this.ssProxyPassword = null;
      this.shadowsocksHubProxy?.stop().catch(() => {
        // best-effort teardown
      });
    }
  }

  /**
   * Verify that /v1/secure is reachable and decryptable on the currently
   * selected transport path (direct domain when nothing else is selected, and
   * otherwise the Shadowsocks proxy, an edge relay, or a DoH-resolved IP).
   *
   * Uses /api/client/regions as the inner probe route because the hub's
   * /v1/secure handler enforces an ALLOWED_ROUTES whitelist; unauthenticated
   * routes like /health are rejected with 403 before the crypto roundtrip
   * completes. An unauthenticated call returns inner status 401 inside a
   * successfully-decrypted envelope, which is all the probe needs.
   */
  private async trySecureProbeCurrentPath(): Promise<boolean> {
    return this.probeSecurePath(this.currentProbePath());
  }

  /** The path the next hub request would take, as an explicit value. */
  private currentProbePath(): HubProbePath {
    if (this.ssProxyPort) {
      return {
        kind: "proxy",
        port: this.ssProxyPort,
        username: this.ssProxyUsername ?? undefined,
        password: this.ssProxyPassword ?? undefined
      };
    }
    if (this.frontedHost) return { kind: "fronted", host: this.frontedHost };
    if (this.dohResolvedIp) return { kind: "ip", ip: this.dohResolvedIp };
    return { kind: "domain", host: this.normalHost };
  }

  private async probeSecurePath(path: HubProbePath): Promise<boolean> {
    const { envelope, aesKey } = encryptRequest("GET", "/api/client/regions", {}, undefined);
    const envelopeJson = JSON.stringify(envelope);

    try {
      let rawResponse: Response;
      if (path.kind === "proxy") {
        rawResponse = await fetchViaConnectProxy(path.port, HUB_HOSTNAME, HUB_HOSTNAME, "/v1/secure", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: envelopeJson,
          timeoutMs: 8000,
          proxyUsername: path.username,
          proxyPassword: path.password
        });
      } else if (path.kind === "fronted") {
        rawResponse = await fetchFronted(path.host, "/v1/secure", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: envelopeJson,
          timeoutMs: 8000
        });
      } else if (path.kind === "ip") {
        rawResponse = await fetchDohResolved(path.ip, HUB_HOSTNAME, "/v1/secure", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: envelopeJson,
          timeoutMs: 5000
        });
      } else {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 5000);
        try {
          rawResponse = await net.fetch(`https://${path.host}/v1/secure`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: envelopeJson,
            signal: controller.signal,
          });
        } finally {
          clearTimeout(timer);
        }
      }

      if (!rawResponse.ok) {
        return false;
      }

      const responseText = await rawResponse.text();
      if (responseText.trimStart().startsWith("<")) {
        return false;
      }

      const encryptedResponse = JSON.parse(responseText) as EncryptedResponse;
      decryptResponse(aesKey, encryptedResponse);
      return true;
    } catch (err) {
      console.log(`[HubURL] Secure probe failed: ${sanitizeLog(err)}`);
      return false;
    }
  }

  /**
   * Ensure we have a working connection strategy to the hub API. Each step
   * belongs to a hub method the user can switch off; at least one is always
   * enabled (see shared/hubMethods.ts). Order, skipping disabled methods:
   *
   *   directIp     1. Last known good IP — no lookup at all, so it is the only
   *                   path that survives an engaged Lockdown lock.
   *                2. DoH-resolved IP, connected with no SNI.
   *   shadowsocks  3. Through the daemon's Shadowsocks proxy.
   *   fronted      4. Through an edge relay on shared CDN address space.
   *   normal       5. Plain HTTPS to the hub domain.
   *
   * Steps 1-3 all terminate on address space we own, so one enumeration sweep
   * that blackholes it takes out all three at once. Step 4 is the answer to
   * exactly that: its address is the CDN's, shared with enough of the web that
   * blocking it costs the censor more than it costs us.
   *
   * "normal" is last on purpose: it is the only step that puts the hub's name
   * on the wire in cleartext, so anything else that works should win first.
   */
  private async ensureHub(): Promise<void> {
    if (this.hubReady) {
      return;
    }
    // Join the cascade already running instead of starting a second one.
    if (this.hubResolution) {
      return this.hubResolution;
    }
    const sinceFailure = Date.now() - this.hubResolutionFailedAtMs;
    if (this.hubResolutionFailedAtMs > 0 && sinceFailure < HUB_RETRY_COOLDOWN_MS) {
      throw new HubUnreachableError(HUB_RETRY_COOLDOWN_MS - sinceFailure);
    }

    this.hubResolution = this.resolveHubPath()
      .then((confirmed) => {
        // An unconfirmed path still gets this call's real request, but it opens
        // the cooldown so the next caller fails fast instead of hanging again.
        this.hubResolutionFailedAtMs = confirmed ? 0 : Date.now();
      })
      .catch((err: unknown) => {
        this.hubResolutionFailedAtMs = Date.now();
        throw err;
      })
      .finally(() => {
        this.hubResolution = null;
      });
    return this.hubResolution;
  }

  private async resolveHubPath(reseeded = false): Promise<boolean> {
    console.log(`[HubURL] Finding working API connection...`);

    // 1. Last known good IP — no lookup, so it works under a lockdown lock.
    if (this.hubMethods.directIp && this.cachedHubIp) {
      console.log(`[HubURL] Trying cached hub IP ${this.cachedHubIp}`);
      this.dohResolvedIp = this.cachedHubIp;
      if (await this.trySecureProbeCurrentPath()) {
        console.log(`[HubURL] Cached hub IP works`);
        this.setActiveHubMethod("directIp", this.cachedHubIp);
        this.hubReady = true;
        return true;
      }
      console.log(`[HubURL] Cached hub IP failed`);
      this.dohResolvedIp = null;
    }

    // 2. DoH — resolve via encrypted DNS, connect to the IP with no SNI.
    if (this.hubMethods.directIp && this.dohEnabled) {
      const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
      if (resolvedIp) {
        console.log(`[HubURL] Trying DoH-resolved IP ${resolvedIp}`);
        this.dohResolvedIp = resolvedIp;
        if (await this.trySecureProbeCurrentPath()) {
          console.log(`[HubURL] DoH secure probe works`);
          this.rememberHubIp(resolvedIp);
          this.setActiveHubMethod("directIp", resolvedIp);
          this.hubReady = true;
          return true;
        }
        console.log(`[HubURL] DoH secure probe failed`);
        this.dohResolvedIp = null;
      }
    }

    // 3. Shadowsocks — the daemon proxies the secure envelope for us.
    if (this.hubMethods.shadowsocks) {
      this.dohResolvedIp = null;
      if (await this.tryShadowsocksHubPath()) {
        console.log(`[HubURL] Shadowsocks hub path works`);
        this.setActiveHubMethod("shadowsocks", this.hubShadowsocks[0]?.remoteHost ?? null);
        this.hubReady = true;
        return true;
      }
    }

    // 4. Edge relay — the CDN forwards the envelope to the hub for us.
    if (this.hubMethods.fronted) {
      this.dohResolvedIp = null;
      if (await this.tryFrontedPath()) {
        console.log(`[HubURL] Fronted relay works`);
        this.setActiveHubMethod("fronted", this.frontedHost);
        this.hubReady = true;
        return true;
      }
    }

    // 5. Plain HTTPS to the domain. Last because it is the only step whose
    //    SNI names the hub in cleartext.
    if (this.hubMethods.normal) {
      this.dohResolvedIp = null;
      for (const host of normalHubHosts()) {
        console.log(`[HubURL] Trying direct domain: https://${host}`);
        this.normalHost = host;
        if (await this.trySecureProbeCurrentPath()) {
          console.log(`[HubURL] Direct domain secure probe works`);
          this.setActiveHubMethod("normal", host);
          this.hubReady = true;
          return true;
        }
        console.log(`[HubURL] Direct domain failed: ${host}`);
      }
      // Nothing answered; do not strand later requests on the mirror.
      this.normalHost = HUB_HOSTNAME;
    }

    this.dohResolvedIp = null;

    // Every carrier is out of addresses that work. The dead drop is the only
    // remaining source of new ones, so spend it before giving up.
    if (!reseeded && (await this.reseedFromDeadDrop())) {
      return this.resolveHubPath(true);
    }

    if (!this.hubMethods.normal) {
      // Fail closed: falling back to the domain here would leak the SNI the
      // user switched that method off to avoid.
      throw new Error(
        "Hub unreachable: every enabled connection method failed, and the normal (cleartext domain) method is switched off"
      );
    }

    // The probe is stricter than a real request (shorter timeout), so give the
    // domain one genuine attempt — but do not record the path as confirmed.
    console.log(`[HubURL] All strategies failed, falling back to direct domain`);
    return false;
  }

  /**
   * Pulls the signed public bootstrap file and folds whatever it names into the
   * caches the ladder reads. Returns whether anything actually changed, so a
   * blob we already had does not buy a pointless second pass.
   *
   * Never throws: a dead drop that cannot be reached leaves the client exactly
   * where it already was.
   */
  private async reseedFromDeadDrop(): Promise<boolean> {
    if (!this.deadDropEnabled) return false;
    const now = Date.now();
    if (!deadDropDue(this.deadDropLastAttemptMs, now)) {
      console.log(`[HubURL] Dead drop rate limited, skipping`);
      return false;
    }
    this.deadDropLastAttemptMs = now;
    this.onDeadDropState?.({ seq: this.deadDropSeq, lastAttemptMs: now });

    let result: Awaited<ReturnType<typeof fetchDeadDropPayload>> = null;
    try {
      result = await fetchDeadDropPayload({
        keys: DEAD_DROP_KEYS,
        minSeq: this.deadDropSeq,
        nowMs: now
      });
    } catch (err) {
      console.warn(`[HubURL] Dead drop fetch failed:`, sanitizeLog(err));
      return false;
    }
    if (!result) {
      console.log(`[HubURL] Dead drop had nothing usable`);
      return false;
    }

    console.log(`[HubURL] Dead drop seq ${result.payload.seq} from ${result.url}`);
    this.deadDropSeq = result.payload.seq;
    this.onDeadDropState?.({ seq: this.deadDropSeq, lastAttemptMs: now });

    const before = this.frontedEndpoints;
    this.rememberFrontedEndpoints(result.payload.frontedEndpoints);
    let changed = this.frontedEndpoints !== before;

    // Only one hub IP is cached at a time, so take the first the blob names and
    // leave the rest for the next pass if this one does not work out.
    const [hubIp] = result.payload.hubIps;
    if (hubIp && hubIp !== this.cachedHubIp) {
      this.rememberHubIp(hubIp);
      changed = true;
    }
    return changed;
  }

  /**
   * Attempt the hub through the daemon's Shadowsocks proxy. Returns false —
   * never throws — so ensureHub can fall through to the remaining methods.
   */
  private async tryShadowsocksHubPath(): Promise<boolean> {
    if (!this.shadowsocksHubProxy || this.hubShadowsocks.length === 0) {
      console.log(`[HubURL] Shadowsocks hub path unavailable (no proxy or no cached credentials)`);
      return false;
    }
    const proxy = this.shadowsocksHubProxy;
    const won = await firstWorkingCreds(
      this.hubShadowsocks,
      async (creds) => {
        const started = await proxy.start(creds);
        if (!started) return null;
        this.ssProxyPort = started.port;
        this.ssProxyUsername = started.proxyUsername;
        this.ssProxyPassword = started.proxyPassword;
        if (await this.trySecureProbeCurrentPath()) return started.port;
        this.ssProxyPort = null;
        this.ssProxyUsername = null;
        this.ssProxyPassword = null;
        await proxy.stop();
        return null;
      },
      (err, index) => {
        console.warn(`[HubURL] Shadowsocks hub node ${index + 1} failed:`, sanitizeLog(err));
        this.ssProxyPort = null;
        this.ssProxyUsername = null;
        this.ssProxyPassword = null;
      }
    );
    if (!won) return false;
    this.promoteHubShadowsocks(won.index);
    return true;
  }

  /**
   * Attempt the hub through each cached edge relay in turn. Returns false —
   * never throws — so ensureHub can fall through to the remaining methods.
   */
  private async tryFrontedPath(): Promise<boolean> {
    if (this.frontedEndpoints.length === 0) {
      console.log(`[HubURL] Fronted path unavailable (no relay configured)`);
      return false;
    }
    for (const [index, host] of this.frontedEndpoints.entries()) {
      this.frontedHost = host;
      try {
        if (await this.trySecureProbeCurrentPath()) {
          this.promoteFronted(index);
          return true;
        }
      } catch (err) {
        console.warn(`[HubURL] Fronted relay ${index + 1} failed:`, sanitizeLog(err));
      }
    }
    this.frontedHost = null;
    return false;
  }

  private promoteFronted(index: number): void {
    const promoted = promoteFrontedEndpoint(this.frontedEndpoints, index);
    if (!promoted) return;
    this.frontedEndpoints = promoted;
    this.onFrontedEndpoints?.(this.frontedEndpoints);
  }

  private promoteHubShadowsocks(index: number): void {
    const promoted = promoteCreds(this.hubShadowsocks, index);
    if (!promoted) return;
    this.hubShadowsocks = promoted;
    this.onHubShadowsocks?.(this.hubShadowsocks);
  }

  /** Probes one method on its own, leaving the path in use untouched. Never
   *  throws — a failure is a result the settings pane renders. */
  async testHubMethod(method: HubMethod): Promise<HubMethodTestResult> {
    if (this.hubTestInFlight) {
      return { method, ok: false, unavailable: "busy", ms: 0 };
    }
    this.hubTestInFlight = true;
    const startedAt = Date.now();
    try {
      const outcome = await this.runHubMethodTest(method);
      return { ...outcome, method, ms: Date.now() - startedAt };
    } catch (err) {
      console.warn(`[HubTest] ${method} failed:`, sanitizeLog(err));
      return { method, ok: false, ms: Date.now() - startedAt };
    } finally {
      this.hubTestInFlight = false;
    }
  }

  private async runHubMethodTest(method: HubMethod): Promise<HubMethodOutcome> {
    switch (method) {
      case "directIp":
        return this.testDirectIpPath();
      case "shadowsocks":
        return this.testShadowsocksPath();
      case "fronted":
        return this.testFrontedPath();
      case "normal": {
        for (const host of normalHubHosts()) {
          if (await this.probeSecurePath({ kind: "domain", host })) {
            return { ok: true, detail: host };
          }
        }
        return { ok: false };
      }
    }
  }

  private async testDirectIpPath(): Promise<HubMethodOutcome> {
    const candidates = this.cachedHubIp ? [this.cachedHubIp] : [];
    if (this.dohEnabled) {
      const resolved = await resolveViaDoH(HUB_HOSTNAME);
      if (resolved && !candidates.includes(resolved)) candidates.push(resolved);
    }
    if (candidates.length === 0) {
      return { ok: false, unavailable: "noAddress" };
    }
    for (const ip of candidates) {
      if (await this.probeSecurePath({ kind: "ip", ip })) return { ok: true, detail: ip };
    }
    return { ok: false };
  }

  private async testFrontedPath(): Promise<HubMethodOutcome> {
    if (this.frontedEndpoints.length === 0) {
      return { ok: false, unavailable: "noRelay" };
    }
    for (const host of this.frontedEndpoints) {
      if (await this.probeSecurePath({ kind: "fronted", host })) return { ok: true, detail: host };
    }
    return { ok: false };
  }

  private async testShadowsocksPath(): Promise<HubMethodOutcome> {
    if (!this.shadowsocksHubProxy || this.hubShadowsocks.length === 0) {
      return { ok: false, unavailable: "noCredentials" };
    }
    // Borrow the running proxy rather than restarting the one carrying traffic.
    if (this.ssProxyPort !== null) {
      const ok = await this.probeSecurePath(this.currentProbePath());
      return ok ? { ok, detail: this.hubShadowsocks[0]?.remoteHost } : { ok };
    }
    const proxy = this.shadowsocksHubProxy;
    for (const creds of this.hubShadowsocks) {
      let started: { port: number; proxyUsername: string; proxyPassword: string } | null = null;
      try {
        started = await proxy.start(creds);
      } catch (err) {
        console.warn(`[HubTest] Shadowsocks proxy start failed:`, sanitizeLog(err));
      }
      if (!started) continue;
      const ok = await this.probeSecurePath({
        kind: "proxy",
        port: started.port,
        username: started.proxyUsername,
        password: started.proxyPassword
      });
      await proxy.stop();
      if (ok) return { ok: true, detail: creds.remoteHost };
    }
    return { ok: false };
  }

  /**
   * Unified fetch that encrypts all requests through the secure channel.
   * Uses net.fetch (trusts system cert store) for direct domain, or
   * fetchDohResolved for DoH-resolved IP connections.
   */
  private async hubFetch(
    path: string,
    options: {
      method?: string;
      headers?: Record<string, string>;
      body?: string;
      signal?: AbortSignal;
      /** The caller's own cancel, undiluted by the request timeout composed
       *  into `signal` — a slow cascade is not a cancelled one. */
      cancelSignal?: AbortSignal;
    }
  ): Promise<Response> {
    // Race, don't just await: the cascade is shared with other callers, so this
    // one abandons it rather than cancelling it out from under them.
    await raceAbort(this.ensureHub(), options.cancelSignal);

    try {
      const response = await this.sendHubFetch(path, options);
      this.hubFailureStreak = 0;
      this.hubResolutionFailedAtMs = 0;
      return response;
    } catch (err) {
      if (err instanceof ConnectCancelledError) throw err;
      this.hubFailureStreak++;
      if (this.hubFailureStreak >= HUB_FAILURE_LIMIT) {
        console.log("[hubFetch] Path failed repeatedly, forcing hub re-resolution");
        this.hubFailureStreak = 0;
        this.hubReady = false;
        this.dohResolvedIp = null;
        this.frontedHost = null;
        if (this.ssProxyPort !== null) {
          this.ssProxyPort = null;
          this.ssProxyUsername = null;
          this.ssProxyPassword = null;
          this.shadowsocksHubProxy?.stop().catch(() => {
            // best-effort teardown
          });
        }
      }
      throw err;
    }
  }

  private async sendHubFetch(
    path: string,
    options: {
      method?: string;
      headers?: Record<string, string>;
      body?: string;
      signal?: AbortSignal;
      cancelSignal?: AbortSignal;
    }
  ): Promise<Response> {
    const method = options.method ?? "GET";
    const headers = options.headers ?? {};
    const bodyObj = options.body ? JSON.parse(options.body) : undefined;

    // Encrypt the inner request
    const { envelope, aesKey } = encryptRequest(method, path, headers, bodyObj);
    const envelopeJson = JSON.stringify(envelope);

    // Send encrypted envelope to /v1/secure
    let rawResponse: Response;
    if (this.ssProxyPort) {
      // CONNECT names the hub by hostname: the node resolves it, so a client
      // with no cached IP still gets through.
      rawResponse = await fetchViaConnectProxy(this.ssProxyPort, HUB_HOSTNAME, HUB_HOSTNAME, "/v1/secure", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: envelopeJson,
        timeoutMs: this.timeoutMs,
        signal: options.signal,
        proxyUsername: this.ssProxyUsername ?? undefined,
        proxyPassword: this.ssProxyPassword ?? undefined
      });
    } else if (this.frontedHost) {
      rawResponse = await fetchFronted(this.frontedHost, "/v1/secure", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: envelopeJson,
        timeoutMs: this.timeoutMs,
        signal: options.signal
      });
    } else if (this.dohResolvedIp) {
      rawResponse = await fetchDohResolved(this.dohResolvedIp, HUB_HOSTNAME, "/v1/secure", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: envelopeJson,
        timeoutMs: this.timeoutMs,
        signal: options.signal
      });
    } else if (!this.hubMethods.normal) {
      // Defensive: ensureHub() should already have thrown when the cleartext
      // domain method is off and nothing else resolved a path.
      throw new Error("Hub transport unavailable: the normal (cleartext domain) method is switched off");
    } else {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeoutMs);
      const onExternalAbort = () => controller.abort();
      if (options.signal) {
        if (options.signal.aborted) controller.abort();
        else options.signal.addEventListener("abort", onExternalAbort, { once: true });
      }
      try {
        rawResponse = await net.fetch(`https://${this.normalHost}/v1/secure`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: envelopeJson,
          signal: controller.signal,
        });
      } catch (err: unknown) {
        // TLS cert error (MITM proxy / corporate WiFi) — fall back to DoH + direct IP
        const msg = err instanceof Error ? err.message : "";
        if (msg.includes("CERT") || msg.includes("certificate") || msg.includes("SSL")) {
          console.log("[hubFetch] TLS cert error, falling back to DoH + direct IP");
          if (this.hubMethods.directIp) {
            const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
            if (resolvedIp) {
              this.dohResolvedIp = resolvedIp;
              this.rememberHubIp(resolvedIp);
              this.setActiveHubMethod("directIp", resolvedIp);
              rawResponse = await fetchDohResolved(resolvedIp, HUB_HOSTNAME, "/v1/secure", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: envelopeJson,
                timeoutMs: this.timeoutMs,
              });
            } else {
              throw err;
            }
          } else {
            throw err;
          }
        } else {
          throw err;
        }
      } finally {
        clearTimeout(timer);
        options.signal?.removeEventListener("abort", onExternalAbort);
      }
    }

    const responseText = await rawResponse.text();

    if (!rawResponse.ok) {
      throw new Error(`Secure channel error (${rawResponse.status}): ${responseText}`);
    }

    // If response looks like HTML, the request is being intercepted (DPI / captive portal).
    // Fall back to DoH + direct IP to bypass interception.
    if (responseText.trimStart().startsWith("<")) {
      if (!this.dohResolvedIp && this.dohEnabled && this.hubMethods.directIp) {
        console.log("[hubFetch] Response intercepted (HTML), falling back to DoH");
        const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
        if (resolvedIp) {
          this.dohResolvedIp = resolvedIp;
          this.setActiveHubMethod("directIp", resolvedIp);
          // A relay or proxy that answers with an error page is being
          // intercepted just as surely as the domain would be; drop both so
          // the branch order in this method reaches the DoH path just resolved.
          this.frontedHost = null;
          if (this.ssProxyPort !== null) {
            this.ssProxyPort = null;
            this.ssProxyUsername = null;
            this.ssProxyPassword = null;
            this.shadowsocksHubProxy?.stop().catch(() => {
              // best-effort teardown
            });
          }
          const retryResponse = await fetchDohResolved(resolvedIp, HUB_HOSTNAME, "/v1/secure", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: envelopeJson,
            timeoutMs: this.timeoutMs
          });
          const retryText = await retryResponse.text();
          if (!retryResponse.ok) {
            throw new Error(`Secure channel error via DoH (${retryResponse.status}): ${retryText}`);
          }
          if (retryText.trimStart().startsWith("<")) {
            throw new Error("Response intercepted even via DoH");
          }
          this.rememberHubIp(resolvedIp);
          const retryEncrypted = JSON.parse(retryText) as EncryptedResponse;
          const retryInner = decryptResponse(aesKey, retryEncrypted);
          return new Response(JSON.stringify(retryInner.body), {
            status: retryInner.status,
            headers: { "Content-Type": "application/json" },
          });
        }
      }
      throw new Error("Response intercepted (HTML returned instead of JSON)");
    }

    // Decrypt the response
    const encryptedResponse = JSON.parse(responseText) as EncryptedResponse;
    const inner = decryptResponse(aesKey, encryptedResponse);

    return new Response(JSON.stringify(inner.body), {
      status: inner.status,
      headers: { "Content-Type": "application/json" },
    });
  }

  async tokenLogin(vpnAccessToken: string): Promise<TokenLoginResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const response = await this.hubFetch("/api/client/token-login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          vpnAccessToken: vpnAccessToken.trim(),
          ...(this.identityPubkey ? { identityPubkey: this.identityPubkey } : {})
        }),
        signal: controller.signal
      });

      if (!response.ok) {
        const text = await response.text();
        if (text.includes("SUBSCRIPTION_EXPIRED")) {
          throw new SubscriptionExpiredError(
            "This account's subscription has expired. Top up or resubscribe, then sign in again."
          );
        }
        throw new Error(`Token login failed (${response.status}): ${text}`);
      }

      const data = (await response.json()) as TokenLoginResponse;
      this.acceptLicenseKey(data.vpnAccessToken, "Token login");
      this.rememberServers(data.servers);
      this.rememberHubShadowsocks(data.servers);
      this.rememberFrontedEndpoints(data.frontedEndpoints);
      return data;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        throw new Error("Token login request timeout");
      }
      throw error;
    } finally {
      clearTimeout(timer);
    }
  }

  async bootstrap(auth0AccessToken: string): Promise<BootstrapResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const response = await this.hubFetch("/api/client/bootstrap", {
        method: "GET",
        headers: { Authorization: `Bearer ${auth0AccessToken}` },
        signal: controller.signal
      });

      if (!response.ok) {
        const text = await response.text();
        throw new Error(`Bootstrap failed (${response.status}): ${text}`);
      }

      const data = (await response.json()) as BootstrapResponse;
      this.acceptLicenseKey(data.vpnAccessToken, "Bootstrap");
      this.rememberServers(data.servers);
      this.rememberHubShadowsocks(data.servers);
      this.rememberFrontedEndpoints(data.frontedEndpoints);
      return data;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        throw new Error("Bootstrap request timeout");
      }
      throw error;
    } finally {
      clearTimeout(timer);
    }
  }

  /**
   * The node list, falling back to the cached one when the hub cannot be
   * reached at all. Without that fallback a blocked hub leaves the client with
   * nowhere to connect even though it knows perfectly well where the nodes are.
   *
   * An AuthError or an expired subscription still propagates: those are the hub
   * answering, and answering "no". Serving a stale list over the top of a real
   * answer would show a signed-out user a working server picker.
   */
  async getServers(): Promise<ServerInfo[]> {
    if (!this.licenseKey) throw new Error("Not authenticated");
    // Send identityPubkey so the hub returns this device's own cloakUid
    // and re-pushes it to every region. Older hubs ignore the param and
    // fall back to their LIMIT-1 path, so this is safe to send always.
    const route = this.identityPubkey
      ? `/api/client/regions?identityPubkey=${encodeURIComponent(this.identityPubkey)}`
      : "/api/client/regions";
    try {
      const data = await this.hubRequest<ServerInfo[]>("GET", route);
      this.rememberServers(data);
      this.rememberHubShadowsocks(data);
      this.rememberFrontedEndpoints(data.find((s) => s.frontedEndpoints)?.frontedEndpoints);
      return data;
    } catch (err) {
      if (err instanceof AuthError || err instanceof SubscriptionExpiredError) throw err;
      if (err instanceof ConnectCancelledError) throw err;
      if (this.cachedServers.length === 0) throw err;
      console.warn(
        `[getServers] hub unreachable, using ${this.cachedServers.length} cached node(s):`,
        sanitizeLog(err)
      );
      return [...this.cachedServers];
    }
  }

  async registerDevice(identityPubkey: string, friendlyName?: string): Promise<DeviceRegisterResponse> {
    return this.hubRequest<DeviceRegisterResponse>("POST", "/api/device/register", {
      licenseKey: this.licenseKey,
      identityPubkey,
      ...(friendlyName ? { friendlyName } : {})
    });
  }

  async deregisterDevice(identityPubkey: string): Promise<void> {
    await this.hubRequest<unknown>("POST", "/api/device/deregister", {
      licenseKey: this.licenseKey,
      identityPubkey
    });
  }

  async listDevices(): Promise<DeviceInfo[]> {
    const data = await this.hubRequest<{ devices: DeviceInfo[] }>("GET", "/api/device/list");
    return data.devices;
  }

  async removeDevice(deviceId: string): Promise<void> {
    await this.hubRequest<unknown>("POST", "/api/device/remove", { deviceId });
  }

  async renameDevice(deviceId: string, friendlyName: string): Promise<void> {
    await this.hubRequest<unknown>("POST", "/api/device/rename", { deviceId, friendlyName });
  }

  /** Drops the post-failure cooldown. Pressing Connect is the user asking for
   *  the cascade to be tried again, not to be told it failed a minute ago. */
  retryHubNow(): void {
    this.hubResolutionFailedAtMs = 0;
  }

  /** Restores the cached subscription at startup. */
  setCachedSubscription(stored: unknown): void {
    this.cachedSubscription = restoreCachedSubscription(stored);
  }

  getCachedSubscription(): CachedSubscription | null {
    return this.cachedSubscription;
  }

  /** Called when a fresh answer arrives, so the caller can persist it. */
  onSubscriptionResolved(fn: (cached: CachedSubscription | null) => void): void {
    this.onSubscription = fn;
  }

  /** Falls back to the last answer the hub gave: unreachable must not read as
   *  "no subscription" to a user who is paying for one. */
  async getSubscription(): Promise<SubscriptionInfo | null> {
    try {
      const data = await this.hubRequest<{ subscription: SubscriptionInfo | null }>("GET", "/api/client/subscription");
      const subscription = data.subscription ?? null;
      this.rememberSubscription(subscription);
      return subscription;
    } catch (err) {
      // A definitive hub "no" must revoke, not fall through to the cache.
      if (err instanceof AuthError || err instanceof SubscriptionExpiredError) throw err;
      console.warn("[getSubscription]", sanitizeLog(err));
      const cached = this.cachedSubscription;
      if (!cached) return null;
      // Serve the cache with entitlement re-derived from the stored expiry, so
      // a plan that has since lapsed does not read as still valid.
      return { ...cached.subscription, entitled: cachedEntitlement(cached, Date.now()) };
    }
  }

  /** A null answer is cached too: the hub said this account has no plan. */
  private rememberSubscription(subscription: SubscriptionInfo | null): void {
    if (!subscription) {
      this.cachedSubscription = null;
      this.onSubscription?.(null);
      return;
    }
    const cached: CachedSubscription = { subscription, cachedAt: Date.now() };
    this.cachedSubscription = cached;
    this.onSubscription?.(cached);
  }


  /**
   * Records the first control-plane Shadowsocks block the hub advertised, so a
   * later run that cannot reach the hub any other way still has credentials.
   */
  /**
   * Records the node list the hub just gave us. An empty list is ignored rather
   * than cached: it is far more likely a truncated response or a hub mid-deploy
   * than an instruction to forget every server the user could still reach.
   */
  private rememberServers(servers: ServerInfo[]): void {
    if (!Array.isArray(servers) || servers.length === 0) return;
    // Same shape check the disk cache enforces, so a malformed hub entry is
    // dropped here instead of reaching provision() as a bad profile.
    const valid = servers.filter(isCachedServer);
    if (valid.length === 0) return;
    this.cachedServers = valid;
    this.onServers?.(valid);
  }

  private rememberFrontedEndpoints(advertised: unknown): void {
    const merged = mergeFrontedEndpoints(this.frontedEndpoints, advertised);
    if (!merged) return;
    this.frontedEndpoints = merged;
    this.onFrontedEndpoints?.(this.frontedEndpoints);
  }

  private rememberHubShadowsocks(servers: ServerInfo[]): void {
    if (!Array.isArray(servers)) return;
    const merged = mergeAdvertisedCreds(
      this.hubShadowsocks,
      servers.map((s) => s.controlPlaneShadowsocks)
    );
    if (!merged) return;
    this.hubShadowsocks = merged;
    this.onHubShadowsocks?.(this.hubShadowsocks);
  }

  async provision(serverId: string, signal?: AbortSignal): Promise<Profile> {
    if (!this.licenseKey) throw new Error("Not authenticated");

    const server = this.cachedServers.find((s) => s.id === serverId);
    if (!server) throw new Error(`Unknown server: ${serverId}`);

    // Ephemeral WG keypair — generated fresh per connection, never stored
    const keyPair = generateWireGuardKeyPair();

    const reg = await this.hubRequest<RegisterResponse>(
      "POST",
      "/api/register",
      {
        licenseKey: this.licenseKey,
        identityPubkey: this.identityPubkey,
        wgPubkey: keyPair.publicKey,
        region: serverId
      },
      signal
    );

    if (!reg.serverPubkey || !reg.assignedIP || !reg.dns) {
      throw new AuthError(
        "Server returned an incomplete response. Your device may have been removed.",
        403
      );
    }

    const { servers: dnsServers, configValue: dnsText } = resolveWireGuardDns(
      reg.dns,
      this.customDnsServers
    );

    // Transport endpoints must be excluded from AllowedIPs (or the dial routes
    // into the tunnel it is establishing) and permitted through the kill switch.
    // All from the hub — we never resolve a node domain ourselves: that leaks it
    // to a third-party resolver and cannot work behind a Lockdown lock.
    //
    // IPv4 literals only: the AllowedIPs split does integer arithmetic on these,
    // so an unvalidated hub hostname produced garbage CIDRs. A domain-only node
    // still gets its bypass route from the daemon at WireGuard start.
    // The node's WireGuard listener needs the same treatment, because the direct
    // method dials it rather than a loopback bridge. Normally it is the node
    // address already on the list, and deduplicated away.
    const wireguardEndpoint = parseNodeWireGuardEndpoint(reg.serverEndpoint);
    const nodeIp = server.cloak.remoteHost;
    const excludeIPs = uniqueNonEmpty([
      nodeIp,
      wireguardEndpoint?.host,
      // resolveNaiveEndpoint dials remoteHost when remoteIp is absent, so
      // that address needs excluding too whenever it happens to be a literal.
      server.naive?.remoteIp ?? server.naive?.remoteHost,
      server.reality?.remoteIp,
      server.hysteria2?.remoteIp,
      server.shadowsocks?.remoteIp
    ]).filter(isIPv4Literal);
    // Snowflake is the exception and is left out: its broker is a third-party
    // Tor host, not one of our nodes, and its data-plane peer is a volunteer
    // proxy discovered per-session — neither address is ours to know up front.
    // It stays release-gated in the daemon (see snowflakeReleaseGated), and
    // ungating it needs its rendezvous addresses to come from the hub too.

    const cloakLocalPort = 51820;
    const configText = buildWireGuardConfig(
      keyPair.privateKey,
      reg.assignedIP,
      dnsText,
      reg.serverPubkey,
      cloakLocalPort,
      excludeIPs,
      this.wireguardMtu
    );

    return {
      id: `auto-${serverId}`,
      name: `${server.name} (auto)`,
      cloak: {
        localPort: 51820,
        remoteHost: server.cloak.remoteHost,
        remotePort: 443,
        uid: server.cloak.uid,
        publicKey: server.cloak.publicKey,
        encryptionMethod: "plain",
        password: "",
        ...(server.cloak.serverName ? { serverName: server.cloak.serverName } : {})
      },
      // Naive dials the node, not its domain — the engine validates against
      // serverName and maps it onto remoteHost. See resolveNaiveEndpoint.
      ...(server.naive ? {
        naive: {
          localPort: 0,
          ...resolveNaiveEndpoint(server.naive, nodeIp),
          remotePort: server.naive.remotePort,
          username: server.naive.username,
          password: server.naive.password
        }
      } : {}),
      // Reality and Hysteria2 dial the node's IP rather than its domain, so no
      // lookup stands between them and the node — the difference between
      // working and not working behind a Lockdown lock. The TLS ClientHello is
      // unchanged: both carry their SNI in a field of their own, and where the
      // hub sends none the domain is passed through as the SNI it would have
      // been used for anyway.
      ...(server.reality ? {
        reality: {
          localPort: 0,
          remoteHost: server.reality.remoteIp ?? nodeIp,
          remotePort: server.reality.remotePort,
          uuid: server.reality.uuid,
          publicKey: server.reality.publicKey,
          shortId: server.reality.shortId,
          ...(server.reality.flow ? { flow: server.reality.flow } : {}),
          // Reality falls back to its own cover SNI when this is absent, so the
          // domain is only passed through when the hub named one.
          ...(server.reality.serverName ? { serverName: server.reality.serverName } : {})
        }
      } : {}),
      ...(server.hysteria2 ? {
        hysteria2: {
          localPort: 0,
          remoteHost: server.hysteria2.remoteIp ?? nodeIp,
          remotePort: server.hysteria2.remotePort,
          password: server.hysteria2.password,
          obfsPassword: server.hysteria2.obfsPassword,
          serverName: server.hysteria2.serverName ?? server.hysteria2.remoteHost,
          ...(server.hysteria2.pinSha256 ? { pinSha256: server.hysteria2.pinSha256 } : {})
        }
      } : {}),
      ...(server.shadowsocks ? {
        shadowsocks: buildShadowsocksProfile(server.shadowsocks, nodeIp)
      } : {}),
      ...(server.snowflake ? {
        snowflake: {
          localPort: 0,
          brokerURL: server.snowflake.brokerURL,
          bridgeFingerprint: server.snowflake.bridgeFingerprint,
          ...(server.snowflake.frontDomains ? { fronts: server.snowflake.frontDomains } : {}),
          ...(server.snowflake.ampCacheURL ? { ampCacheUrl: server.snowflake.ampCacheURL } : {}),
          ...(server.snowflake.iceServers ? { iceServers: server.snowflake.iceServers } : {})
        }
      } : {}),
      // What the daemon permits through the kill switch without a lookup.
      transportEndpointIPs: excludeIPs,
      wireguard: {
        configText,
        tunnelName: "pangeavpn",
        dns: dnsServers,
        // Where the tunnel goes when the user picks plain WireGuard; configText
        // itself always points at the loopback transport bridge.
        ...(wireguardEndpoint ? { directEndpoint: wireguardEndpoint.endpoint } : {}),
        // The hub's IP, so the daemon keeps it reachable: outside the tunnel for
        // routing, and through the kill switch so re-provisioning during a
        // switch — and provisioning under a Lockdown lock — still works. Falls
        // back to the last known good IP when this session reached the hub by
        // domain instead of resolving it.
        bypassHosts: (() => {
          const hubIp = this.getHubIp();
          return hubIp ? [hubIp] : [];
        })()
      }
    };
  }

  clearCache(): void {
    this.licenseKey = null;
    this.cachedServers = [];
    // The node list is account-scoped, so it goes when the account does — and
    // it must leave disk too, or the next user of this machine gets a picker
    // full of the last one's servers. The hub IP, relays and control-plane
    // credentials deliberately survive: they are how a signed-out client
    // reaches the hub to sign in again.
    this.onServers?.([]);
    // Account-scoped like the node list: the next user of this machine must not
    // inherit the last one's renewal date.
    this.cachedSubscription = null;
    this.onSubscription?.(null);
    this.resetHubResolution();
    this.identityPubkey = null;
  }

  /**
   * @param externalSignal Aborts this request when the caller's work is
   *   cancelled — e.g. the user pressing Stop mid-connect. Composed with the
   *   timeout controller so either can abort, and requests without one behave
   *   exactly as before.
   */
  private async hubRequest<T>(
    method: string,
    route: string,
    body?: unknown,
    externalSignal?: AbortSignal
  ): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);
    const abortFromCaller = () => controller.abort();
    if (externalSignal) {
      if (externalSignal.aborted) controller.abort();
      else externalSignal.addEventListener("abort", abortFromCaller, { once: true });
    }

    try {
      const headers: Record<string, string> = {
        "Content-Type": "application/json"
      };
      if (this.licenseKey) {
        headers["X-License-Key"] = this.licenseKey;
      }

      const response = await this.hubFetch(route, {
        method,
        headers,
        body: body ? JSON.stringify(body) : undefined,
        signal: controller.signal,
        ...(externalSignal ? { cancelSignal: externalSignal } : {})
      });

      if (!response.ok) {
        const text = await response.text();
        // Check this before the auth branch: an expired subscription is a 403,
        // but it must not log the user out (see SubscriptionExpiredError).
        if (text.includes("SUBSCRIPTION_EXPIRED")) {
          throw new SubscriptionExpiredError(
            "Your subscription has expired. Top up or resubscribe to reconnect."
          );
        }
        if (response.status === 401 || response.status === 403 || text.includes("DEVICE_NOT_REGISTERED")) {
          throw new AuthError(`Hub API auth error (${response.status}): ${text}`, response.status);
        }
        throw new Error(`Hub API error (${response.status}): ${text}`);
      }

      const text = await response.text();
      return (text ? JSON.parse(text) : undefined) as T;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        // Distinguish "the user stopped this" from "the hub is slow": the
        // caller turns a cancellation into an idle UI, not an error toast.
        if (externalSignal?.aborted) {
          throw new ConnectCancelledError();
        }
        throw new Error(`Hub API timeout (${method} ${route})`);
      }
      throw error;
    } finally {
      clearTimeout(timer);
      externalSignal?.removeEventListener("abort", abortFromCaller);
    }
  }
}
