import { generateKeyPairSync } from "node:crypto";
import https from "node:https";
import { URL } from "node:url";
import { net } from "electron";
import type { Profile } from "@pangeavpn/shared-types";
import type { ServerInfo, SubscriptionInfo } from "../shared/ipc";
import { encryptRequest, decryptResponse, type EncryptedResponse } from "./secureChannel";

export class AuthError extends Error {
  status: number;
  constructor(message: string, status: number) {
    super(message);
    this.name = "AuthError";
    this.status = status;
  }
}

const HUB_HOSTNAME = "api.pangeavpn.org";
const HUB_API_BASE = `https://${HUB_HOSTNAME}`;

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
}

interface TokenLoginResponse {
  vpnAccessToken: string;
  user: { email: string; name: string };
  servers: ServerInfo[];
}

interface RegisterResponse {
  serverPubkey: string;
  serverEndpoint: string;
  assignedIP: string;
  dns: string;
  existingConfig?: boolean;
}

interface DohAnswer {
  data: string;
}

interface DohResponse {
  Answer?: DohAnswer[];
}

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
    const answers = data.Answer?.filter((a) => a.data) ?? [];
    if (answers.length > 0) {
      console.log(`[DoH] ${providerUrl} resolved ${hostname} → ${answers[0].data}`);
      return answers[0].data;
    }
    console.log(`[DoH] ${providerUrl} returned no answers for ${hostname}`);
    return null;
  } catch (err) {
    console.log(`[DoH] ${providerUrl} failed: ${err instanceof Error ? err.message : err}`);
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
 * Resolve a fallback transport's (NaiveProxy, Hysteria2) remote host to an
 * IP so it can be excluded from AllowedIPs (see allowedIPsExcludingAll).
 * Unlike Cloak, whose remoteHost from the hub is already a raw IP, these
 * transports' remoteHost is a real domain name (their TLS front needs one
 * to obtain a genuine ACME certificate — see hub/config/nodes.json in the
 * hub repo, e.g. naive-eu-west-1.pangeavpn.org), so it must be resolved
 * before it can be turned into a /32 exclusion.
 *
 * Reuses the same DoH infrastructure already used to resolve the hub's own
 * hostname (see resolveViaDoH above / ensureHub), rather than falling back
 * to plain system DNS, since this client otherwise avoids leaking DNS
 * queries in cleartext. Returns null (never throws) on resolution failure
 * so callers can degrade gracefully instead of failing provisioning.
 */
async function resolveTransportRemoteIp(host: string): Promise<string | null> {
  if (isIPv4Literal(host)) return host;
  return resolveViaDoH(host);
}

/**
 * Make an HTTPS request to a DoH-resolved IP with correct SNI for Cloudflare.
 * Uses node:https so we can set servername (SNI) independently from the IP.
 * Uses an external setTimeout for a reliable connection timeout (node:https
 * timeout option only fires after the socket connects).
 */
function fetchDohResolved(
  ip: string,
  hostname: string,
  requestPath: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
    timeoutMs?: number;
  }
): Promise<Response> {
  const deadline = options.timeoutMs ?? 15000;
  return new Promise<Response>((resolve, reject) => {
    let settled = false;
    const timer = setTimeout(() => {
      if (!settled) {
        settled = true;
        req.destroy();
        reject(new Error("Request timeout"));
      }
    }, deadline);

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
        servername: "",
        rejectUnauthorized: false
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
        res.on("end", () => {
          if (settled) return;
          settled = true;
          clearTimeout(timer);
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

    req.on("error", (err) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        reject(err);
      }
    });

    if (options.body) {
      req.write(options.body);
    }
    req.end();
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

/** Calculate AllowedIPs CIDRs that cover 0.0.0.0/0 minus a single excluded IP */
function allowedIPsExcluding(excludeIP: string): string[] {
  return allowedIPsExcludingAll([excludeIP]);
}

/** Matches a literal dotted-quad IPv4 address (not a hostname). */
const IPV4_LITERAL_PATTERN = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

function isIPv4Literal(host: string): boolean {
  const match = IPV4_LITERAL_PATTERN.exec(host);
  if (!match) return false;
  return match.slice(1, 5).every((octet) => Number(octet) <= 255);
}

function buildWireGuardConfig(
  privateKey: string,
  assignedIP: string,
  dns: string,
  serverPubkey: string,
  cloakLocalPort: number,
  excludeIPs: string[]
): string {
  const allowedIPs = allowedIPsExcludingAll(excludeIPs).join(", ");

  return [
    "[Interface]",
    `PrivateKey = ${privateKey}`,
    `Address = ${assignedIP}/32`,
    `DNS = ${dns}`,
    "MTU = 1380",
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
  friendlyName: string | null;
  createdAt: string;
  status: string;
}

export class PangeaApiClient {
  private readonly timeoutMs: number;
  private cachedServers: ServerInfo[] = [];
  private licenseKey: string | null = null;
  private dohEnabled = true;
  private directIpEnabled = true;
  private directIpOnly = true;
  identityPubkey: string | null = null;

  // When DoH resolves an IP, we store it here and use fetchDohResolved()
  // to connect directly with no SNI (invisible to DPI).
  private dohResolvedIp: string | null = null;
  private hubReady = false;

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

  setDirectIpEnabled(enabled: boolean): void {
    this.directIpEnabled = enabled;
    this.resetHubResolution();
  }

  isDirectIpEnabled(): boolean {
    return this.directIpEnabled;
  }

  setDirectIpOnly(enabled: boolean): void {
    this.directIpOnly = enabled;
    if (enabled) {
      this.dohEnabled = true;
      this.directIpEnabled = true;
    }
    this.resetHubResolution();
  }

  isDirectIpOnly(): boolean {
    return this.directIpOnly;
  }

  setLicenseKey(key: string): void {
    this.licenseKey = key.trim();
  }

  getLicenseKey(): string | null {
    return this.licenseKey;
  }

  private resetHubResolution(): void {
    this.dohResolvedIp = null;
    this.hubReady = false;
  }

  /**
   * Verify that /v1/secure is reachable and decryptable on the currently
   * selected transport path (direct domain when dohResolvedIp is null,
   * DoH-resolved direct IP otherwise).
   *
   * Uses /api/client/regions as the inner probe route because the hub's
   * /v1/secure handler enforces an ALLOWED_ROUTES whitelist; unauthenticated
   * routes like /health are rejected with 403 before the crypto roundtrip
   * completes. An unauthenticated call returns inner status 401 inside a
   * successfully-decrypted envelope, which is all the probe needs.
   */
  private async trySecureProbeCurrentPath(): Promise<boolean> {
    const { envelope, aesKey } = encryptRequest("GET", "/api/client/regions", {}, undefined);
    const envelopeJson = JSON.stringify(envelope);

    try {
      let rawResponse: Response;
      if (this.dohResolvedIp) {
        rawResponse = await fetchDohResolved(this.dohResolvedIp, HUB_HOSTNAME, "/v1/secure", {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: envelopeJson,
          timeoutMs: 5000
        });
      } else {
        const controller = new AbortController();
        const timer = setTimeout(() => controller.abort(), 5000);
        try {
          rawResponse = await net.fetch(`${HUB_API_BASE}/v1/secure`, {
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
    } catch {
      return false;
    }
  }

  /**
   * Ensure we have a working connection strategy to the hub API.
   * Tries in order:
   *   1. Direct domain (normal DNS works)
   *   2. DoH + no SNI (DNS blocked / DPI — resolve via encrypted DNS,
   *      connect to resolved IP with no SNI so DPI can't see the domain)
   */
  private async ensureHub(): Promise<void> {
    if (this.hubReady) {
      return;
    }

    console.log(`[HubURL] Finding working API connection...`);

    // 1. Try direct domain (normal DNS) — skip if direct IP only mode
    if (!this.directIpOnly) {
      console.log(`[HubURL] Trying direct domain: ${HUB_API_BASE}`);
      this.dohResolvedIp = null;
      if (await this.trySecureProbeCurrentPath()) {
        console.log(`[HubURL] Direct domain secure probe works`);
        this.hubReady = true;
        return;
      }
      console.log(`[HubURL] Direct domain failed`);
    } else {
      console.log(`[HubURL] Direct IP only mode — skipping domain check`);
    }

    // 2. Try DoH — resolve via encrypted DNS, connect with no SNI
    if (this.directIpEnabled && this.dohEnabled) {
      const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
      if (resolvedIp) {
        console.log(`[HubURL] Trying DoH-resolved IP ${resolvedIp}`);
        this.dohResolvedIp = resolvedIp;
        if (await this.trySecureProbeCurrentPath()) {
          console.log(`[HubURL] DoH secure probe works`);
          this.hubReady = true;
          return;
        }
        console.log(`[HubURL] DoH secure probe failed`);
        this.dohResolvedIp = null;
      }
    }

    // directIpOnly (default): fail closed rather than fall back to the domain (would leak SNI in cleartext).
    this.dohResolvedIp = null;
    if (this.directIpOnly) {
      throw new Error(
        "Hub unreachable: DNS-over-HTTPS resolution failed and direct-IP-only mode forbids a cleartext domain connection"
      );
    }

    console.log(`[HubURL] All strategies failed, falling back to direct domain`);
    this.hubReady = true;
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
    }
  ): Promise<Response> {
    await this.ensureHub();

    const method = options.method ?? "GET";
    const headers = options.headers ?? {};
    const bodyObj = options.body ? JSON.parse(options.body) : undefined;

    // Encrypt the inner request
    const { envelope, aesKey } = encryptRequest(method, path, headers, bodyObj);
    const envelopeJson = JSON.stringify(envelope);

    // Send encrypted envelope to /v1/secure
    let rawResponse: Response;
    if (this.dohResolvedIp) {
      rawResponse = await fetchDohResolved(this.dohResolvedIp, HUB_HOSTNAME, "/v1/secure", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: envelopeJson,
        timeoutMs: this.timeoutMs
      });
    } else if (this.directIpOnly) {
      // Defensive: ensureHub() should already have thrown in directIpOnly mode.
      throw new Error("Hub transport unavailable: direct-IP-only mode forbids a cleartext domain connection");
    } else {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), this.timeoutMs);
      try {
        rawResponse = await net.fetch(`${HUB_API_BASE}/v1/secure`, {
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
          if (this.directIpEnabled) {
            const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
            if (resolvedIp) {
              this.dohResolvedIp = resolvedIp;
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
      }
    }

    const responseText = await rawResponse.text();

    if (!rawResponse.ok) {
      throw new Error(`Secure channel error (${rawResponse.status}): ${responseText}`);
    }

    // If response looks like HTML, the request is being intercepted (DPI / captive portal).
    // Fall back to DoH + direct IP to bypass interception.
    if (responseText.trimStart().startsWith("<")) {
      if (!this.dohResolvedIp && this.dohEnabled && this.directIpEnabled) {
        console.log("[hubFetch] Response intercepted (HTML), falling back to DoH");
        const resolvedIp = await resolveViaDoH(HUB_HOSTNAME);
        if (resolvedIp) {
          this.dohResolvedIp = resolvedIp;
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
        throw new Error(`Token login failed (${response.status}): ${text}`);
      }

      const data = (await response.json()) as TokenLoginResponse;
      this.licenseKey = data.vpnAccessToken;
      this.cachedServers = data.servers;
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
      this.licenseKey = data.vpnAccessToken;
      this.cachedServers = data.servers;
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

  async getServers(): Promise<ServerInfo[]> {
    if (!this.licenseKey) throw new Error("Not authenticated");
    // Send identityPubkey so the hub returns this device's own cloakUid
    // and re-pushes it to every region. Older hubs ignore the param and
    // fall back to their LIMIT-1 path, so this is safe to send always.
    const route = this.identityPubkey
      ? `/api/client/regions?identityPubkey=${encodeURIComponent(this.identityPubkey)}`
      : "/api/client/regions";
    const data = await this.hubRequest<ServerInfo[]>("GET", route);
    this.cachedServers = data;
    return data;
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

  async getSubscription(): Promise<SubscriptionInfo | null> {
    try {
      const data = await this.hubRequest<{ subscription: SubscriptionInfo | null }>("GET", "/api/client/subscription");
      return data.subscription ?? null;
    } catch (err) {
      console.warn("[getSubscription]", err instanceof Error ? err.message : err);
      return null;
    }
  }

  async provision(serverId: string): Promise<Profile> {
    if (!this.licenseKey) throw new Error("Not authenticated");

    const server = this.cachedServers.find((s) => s.id === serverId);
    if (!server) throw new Error(`Unknown server: ${serverId}`);

    // Ephemeral WG keypair — generated fresh per connection, never stored
    const keyPair = generateWireGuardKeyPair();

    const reg = await this.hubRequest<RegisterResponse>("POST", "/api/register", {
      licenseKey: this.licenseKey,
      identityPubkey: this.identityPubkey,
      wgPubkey: keyPair.publicKey,
      region: serverId
    });

    if (!reg.serverPubkey || !reg.assignedIP || !reg.dns) {
      throw new AuthError(
        "Server returned an incomplete response. Your device may have been removed.",
        403
      );
    }

    const dnsServers = reg.dns
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);

    // Every transport that might actually dial out (Cloak always; NaiveProxy
    // and Hysteria2 when configured as a fallback/alternative) needs its
    // remote host excluded from AllowedIPs, or the OS would try to route
    // that connection attempt back through the tunnel it's still
    // establishing. Cloak's remoteHost is always a raw IP from the hub. The
    // others are real domain names (their TLS front needs a genuine ACME
    // cert), so they must be resolved first.
    const excludeIPs = [server.cloak.remoteHost];
    if (server.naive) {
      const naiveIp = await resolveTransportRemoteIp(server.naive.remoteHost);
      if (naiveIp) {
        excludeIPs.push(naiveIp);
      } else {
        // Fail open, not closed: NaiveProxy is a fallback-of-last-resort —
        // Cloak is expected to work in the overwhelming majority of
        // connections, and Cloak's own exclusion above is unaffected by
        // this failure. Losing the ability to fall back to NaiveProxy this
        // session is far preferable to blocking provisioning (and therefore
        // every connection, including plain Cloak ones) on a DoH lookup for
        // a host that's only needed if Cloak later fails.
        console.warn(
          `[provision] Could not resolve NaiveProxy remote host "${server.naive.remoteHost}" to an IP; ` +
          "AllowedIPs will not exclude it. If Cloak fails and the daemon falls back to NaiveProxy, " +
          "its own connection attempt could be routed back through the tunnel (routing loop)."
        );
      }
    }
    if (server.hysteria2) {
      const hysteria2Ip = await resolveTransportRemoteIp(server.hysteria2.remoteHost);
      if (hysteria2Ip) {
        excludeIPs.push(hysteria2Ip);
      } else {
        console.warn(
          `[provision] Could not resolve Hysteria2 remote host "${server.hysteria2.remoteHost}" to an IP; ` +
          "AllowedIPs will not exclude it. If selected as the active transport, its own connection " +
          "attempt could be routed back through the tunnel (routing loop)."
        );
      }
    }

    const cloakLocalPort = 51820;
    const configText = buildWireGuardConfig(
      keyPair.privateKey,
      reg.assignedIP,
      reg.dns,
      reg.serverPubkey,
      cloakLocalPort,
      excludeIPs
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
      ...(server.naive ? {
        naive: {
          localPort: 0,
          remoteHost: server.naive.remoteHost,
          remotePort: server.naive.remotePort,
          username: server.naive.username,
          password: server.naive.password,
          ...(server.naive.serverName ? { serverName: server.naive.serverName } : {})
        }
      } : {}),
      ...(server.hysteria2 ? {
        hysteria2: {
          localPort: 0,
          remoteHost: server.hysteria2.remoteHost,
          remotePort: server.hysteria2.remotePort,
          password: server.hysteria2.password,
          obfsPassword: server.hysteria2.obfsPassword,
          ...(server.hysteria2.serverName ? { serverName: server.hysteria2.serverName } : {})
        }
      } : {}),
      wireguard: {
        configText,
        tunnelName: "pangeavpn",
        dns: dnsServers,
        bypassHosts: this.dohResolvedIp ? [this.dohResolvedIp] : []
      }
    };
  }

  clearCache(): void {
    this.licenseKey = null;
    this.cachedServers = [];
    this.resetHubResolution();
    this.identityPubkey = null;
  }

  private async hubRequest<T>(method: string, route: string, body?: unknown): Promise<T> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

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
        signal: controller.signal
      });

      if (!response.ok) {
        const text = await response.text();
        if (response.status === 401 || response.status === 403 || text.includes("DEVICE_NOT_REGISTERED")) {
          throw new AuthError(`Hub API auth error (${response.status}): ${text}`, response.status);
        }
        throw new Error(`Hub API error (${response.status}): ${text}`);
      }

      return (await response.json()) as T;
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") {
        throw new Error(`Hub API timeout (${method} ${route})`);
      }
      throw error;
    } finally {
      clearTimeout(timer);
    }
  }
}
