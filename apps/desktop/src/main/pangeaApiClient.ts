import { generateKeyPairSync } from "node:crypto";
import https from "node:https";
import { URL } from "node:url";
import { net } from "electron";
import type { Profile } from "@pangeavpn/shared-types";
import type { ServerInfo } from "../shared/ipc";
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

/**
 * HTTPS fetch using node:https with rejectUnauthorized: false.
 * Electron's net.fetch uses Chromium's stack which always validates certs
 * and cannot be bypassed. Since the secure channel is our trust anchor,
 * we use Node's https for all hub requests.
 */
function fetchInsecure(
  url: string,
  options: {
    method?: string;
    headers?: Record<string, string>;
    body?: string;
    timeoutMs?: number;
    signal?: AbortSignal;
  } = {}
): Promise<Response> {
  const parsed = new URL(url);
  const deadline = options.timeoutMs ?? 15000;
  return new Promise<Response>((resolve, reject) => {
    if (options.signal?.aborted) {
      return reject(new DOMException("The operation was aborted.", "AbortError"));
    }

    let settled = false;
    const timer = setTimeout(() => {
      if (!settled) {
        settled = true;
        req.destroy();
        reject(new Error("Request timeout"));
      }
    }, deadline);

    const onAbort = () => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        req.destroy();
        reject(new DOMException("The operation was aborted.", "AbortError"));
      }
    };
    options.signal?.addEventListener("abort", onAbort, { once: true });

    const req = https.request(
      {
        hostname: parsed.hostname,
        port: parsed.port || 443,
        path: parsed.pathname + parsed.search,
        method: options.method ?? "GET",
        headers: options.headers ?? {},
        rejectUnauthorized: false
      },
      (res) => {
        const chunks: Buffer[] = [];
        res.on("data", (chunk: Buffer) => chunks.push(chunk));
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

    req.on("error", (err) => {
      if (!settled) {
        settled = true;
        clearTimeout(timer);
        options.signal?.removeEventListener("abort", onAbort);
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

/** Calculate AllowedIPs CIDRs that cover 0.0.0.0/0 minus a single excluded IP */
function allowedIPsExcluding(excludeIP: string): string[] {
  const parts = excludeIP.split(".").map(Number);
  const ip = ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0;

  const ranges: string[] = [];
  let base = 0;

  for (let prefixLen = 1; prefixLen <= 32; prefixLen++) {
    const bit = 32 - prefixLen;
    const mask = (1 << bit) >>> 0;
    const ipBitSet = (ip & mask) !== 0;

    // Sibling subnet: the half that does NOT contain the excluded IP
    const sibling = ipBitSet ? base : (base | mask) >>> 0;
    const a = (sibling >>> 24) & 0xff;
    const b = (sibling >>> 16) & 0xff;
    const c = (sibling >>> 8) & 0xff;
    const d = sibling & 0xff;
    ranges.push(`${a}.${b}.${c}.${d}/${prefixLen}`);

    if (ipBitSet) {
      base = (base | mask) >>> 0;
    }
  }

  return ranges;
}

function buildWireGuardConfig(
  privateKey: string,
  assignedIP: string,
  dns: string,
  serverPubkey: string,
  cloakLocalPort: number,
  cloakRemoteHost: string
): string {
  const allowedIPs = allowedIPsExcluding(cloakRemoteHost).join(", ");

  return [
    "[Interface]",
    `PrivateKey = ${privateKey}`,
    `Address = ${assignedIP}/32`,
    `DNS = ${dns}`,
    "MTU = 1280",
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
}

export class PangeaApiClient {
  private readonly timeoutMs: number;
  private cachedServers: ServerInfo[] = [];
  private licenseKey: string | null = null;
  private dohEnabled = true;
  private directIpEnabled = true;
  private directIpOnly = false;
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
      if (await this.tryHealthDirect()) {
        console.log(`[HubURL] Direct domain works`);
        this.dohResolvedIp = null;
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
        try {
          const response = await fetchDohResolved(resolvedIp, HUB_HOSTNAME, "/health", { timeoutMs: 5000 });
          if (response.ok) {
            console.log(`[HubURL] DoH works`);
            this.dohResolvedIp = resolvedIp;
            this.hubReady = true;
            return;
          }
          console.log(`[HubURL] DoH health returned ${response.status}`);
        } catch (err) {
          console.log(`[HubURL] DoH failed: ${err instanceof Error ? err.message : err}`);
        }
      }
    }

    console.log(`[HubURL] All strategies failed, falling back to direct domain`);
    this.dohResolvedIp = null;
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

  private async tryHealthDirect(): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 3000);
    try {
      const response = await net.fetch(`${HUB_API_BASE}/health`, {
        signal: controller.signal,
      });
      return response.ok;
    } catch {
      return false;
    } finally {
      clearTimeout(timer);
    }
  }

  async isReachable(): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 3000);
    try {
      const response = await net.fetch(`${HUB_API_BASE}/health`, { signal: controller.signal });
      return response.ok;
    } catch {
      return false;
    } finally {
      clearTimeout(timer);
    }
  }

  async tokenLogin(vpnAccessToken: string): Promise<TokenLoginResponse> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), this.timeoutMs);

    try {
      const response = await this.hubFetch("/api/client/token-login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ vpnAccessToken: vpnAccessToken.trim() }),
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
    const data = await this.hubRequest<ServerInfo[]>("GET", "/api/client/regions");
    this.cachedServers = data;
    return data;
  }

  async registerDevice(identityPubkey: string): Promise<DeviceRegisterResponse> {
    return this.hubRequest<DeviceRegisterResponse>("POST", "/api/device/register", {
      licenseKey: this.licenseKey,
      identityPubkey
    });
  }

  async deregisterDevice(identityPubkey: string): Promise<void> {
    await this.hubRequest<unknown>("POST", "/api/device/deregister", {
      licenseKey: this.licenseKey,
      identityPubkey
    });
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

    const cloakLocalPort = 51820;
    const configText = buildWireGuardConfig(
      keyPair.privateKey,
      reg.assignedIP,
      reg.dns,
      reg.serverPubkey,
      cloakLocalPort,
      server.cloak.remoteHost
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
        password: ""
      },
      wireguard: {
        configText,
        tunnelName: "pangeavpn",
        dns: dnsServers
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
