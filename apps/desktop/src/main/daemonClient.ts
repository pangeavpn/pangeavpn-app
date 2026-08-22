import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse
} from "@pangeavpn/shared-types";

export class TransportExhaustedError extends Error {
  constructor() {
    super("All configured transports failed");
    this.name = "TransportExhaustedError";
  }
}

export class DaemonClient {
  private readonly baseUrl: string;
  private readonly tokenProvider: () => Promise<string | string[]>;
  private readonly defaultRequestTimeoutMs: number;
  private readonly connectTimeoutMs: number;
  private readonly disconnectTimeoutMs: number;

  constructor(baseUrl: string, tokenProvider: () => Promise<string | string[]>) {
    this.baseUrl = baseUrl;
    this.tokenProvider = tokenProvider;
    this.defaultRequestTimeoutMs = 5000;
    // Auto mode can spend 10s proving each configured transport end-to-end.
    this.connectTimeoutMs = 120000;
    this.disconnectTimeoutMs = 45000;
  }

  async getStatus(): Promise<StatusResponse> {
    return this.request<StatusResponse>("GET", "/status");
  }

  async connect(
    profileId: string,
    opts?: {
      allowLAN?: boolean;
      lockdown?: boolean;
      preferredTransport?: "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard";
    },
    signal?: AbortSignal
  ): Promise<OkResponse> {
    const body: Record<string, unknown> = { profileId };
    if (opts?.allowLAN) {
      body.allowLAN = true;
    }
    if (opts?.lockdown) {
      body.lockdown = true;
    }
    if (opts?.preferredTransport) {
      body.preferredTransport = opts.preferredTransport;
    }
    return this.request<OkResponse>("POST", "/connect", body, this.connectTimeoutMs, signal);
  }

  /** Starts the hub Shadowsocks proxy; resolves to its loopback port and CONNECT auth. */
  async startSsProxy(profile: {
    remoteHost: string;
    remotePort: number;
    method: string;
    password: string;
  }): Promise<{ port: number; proxyUsername: string; proxyPassword: string }> {
    const res = await this.request<{
      ok: boolean;
      port?: number;
      proxyUsername?: string;
      proxyPassword?: string;
      error?: string;
    }>("POST", "/ssproxy/start", profile);
    if (!res.ok || !res.port) {
      throw new Error(res.error || "shadowsocks proxy failed to start");
    }
    // A daemon older than the proxy credentials answers with the port alone,
    // and its inbound wants no Proxy-Authorization.
    return {
      port: res.port,
      proxyUsername: res.proxyUsername ?? "",
      proxyPassword: res.proxyPassword ?? ""
    };
  }

  async stopSsProxy(): Promise<OkResponse> {
    return this.request<OkResponse>("POST", "/ssproxy/stop");
  }

  async disconnect(opts?: { keepKillSwitch?: boolean }, signal?: AbortSignal): Promise<OkResponse> {
    const body = opts?.keepKillSwitch ? { keepKillSwitch: true } : undefined;
    return this.request<OkResponse>("POST", "/disconnect", body, this.disconnectTimeoutMs, signal);
  }

  // Forgets the daemon's per-network last-good-transport cache. Called at
  // startup so a new app session never inherits the last one's cascade order.
  async clearTransportMemory(): Promise<OkResponse> {
    return this.request<OkResponse>("POST", "/transport-memory/clear");
  }

  async clearKillSwitch(): Promise<OkResponse> {
    // Kill-switch ops hold opMu and touch WFP; allow more than the 5s default
    // so a busy daemon isn't falsely reported unreachable.
    return this.request<OkResponse>("POST", "/killswitch/clear", undefined, 15000);
  }

  /**
   * Ask the daemon to let these IPs through an engaged kill switch. Used before
   * provisioning under Lockdown: the lock blocks everything, including the hub
   * the app must reach to get a profile. IP literals only — the lock blocks DNS
   * too, so a hostname could never be resolved behind it. An empty list lets
   * the daemon fall back to the hub IP stored with the last profile.
   */
  async permitHosts(hosts: string[]): Promise<OkResponse> {
    return this.request<OkResponse>("POST", "/killswitch/permit", { hosts }, 15000);
  }

  async engageKillSwitch(opts?: { profileId?: string; allowLAN?: boolean }): Promise<OkResponse> {
    const body: Record<string, unknown> = {};
    if (opts?.profileId) body.profileId = opts.profileId;
    if (opts?.allowLAN) body.allowLAN = true;
    return this.request<OkResponse>(
      "POST",
      "/killswitch/engage",
      Object.keys(body).length > 0 ? body : undefined,
      15000
    );
  }

  async switch(
    profileId: string,
    opts?: {
      allowLAN?: boolean;
      lockdown?: boolean;
      preferredTransport?: "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard";
    },
    signal?: AbortSignal
  ): Promise<OkResponse> {
    const body: Record<string, unknown> = { profileId };
    if (opts?.allowLAN) {
      body.allowLAN = true;
    }
    if (opts?.lockdown) {
      body.lockdown = true;
    }
    if (opts?.preferredTransport) {
      body.preferredTransport = opts.preferredTransport;
    }
    return this.request<OkResponse>("POST", "/switch", body, this.connectTimeoutMs, signal);
  }

  async getLogs(since?: number): Promise<LogEntry[]> {
    const query = typeof since === "number" ? `?since=${since}` : "";
    return this.request<LogEntry[]>("GET", `/logs${query}`);
  }

  async getConfig(): Promise<ConfigResponse> {
    return this.request<ConfigResponse>("GET", "/config");
  }

  async setConfig(profiles: Profile[]): Promise<OkResponse> {
    return this.request<OkResponse>("POST", "/config", { profiles });
  }

  async ping(): Promise<boolean> {
    const controller = new AbortController();
    const timer = setTimeout(() => controller.abort(), 1000);
    try {
      const response = await fetch(`${this.baseUrl}/ping`, {
        method: "GET",
        signal: controller.signal
      });
      return response.ok;
    } catch {
      return false;
    } finally {
      clearTimeout(timer);
    }
  }

  private async request<T>(
    method: string,
    route: string,
    body?: unknown,
    timeoutMs?: number,
    signal?: AbortSignal
  ): Promise<T> {
    const rawTokens = await this.tokenProvider();
    const tokens = (Array.isArray(rawTokens) ? rawTokens : [rawTokens])
      .map((token) => token.trim())
      .filter((token, index, values) => token.length > 0 && values.indexOf(token) === index);

    if (tokens.length === 0) {
      throw new Error("daemon token not found");
    }

    for (const token of tokens) {
      const controller = new AbortController();
      const timer = setTimeout(() => controller.abort(), timeoutMs ?? this.defaultRequestTimeoutMs);
      const forwardAbort = () => controller.abort();
      signal?.addEventListener("abort", forwardAbort);

      // Timer stays armed through the whole response read, not just fetch()
      // itself — a daemon that stalls mid-body must still be interruptible.
      try {
        const response = await fetch(`${this.baseUrl}${route}`, {
          method,
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`
          },
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: controller.signal
        });

        if (response.status === 401) {
          // Drain the body so the socket is released before the next candidate
          // token restarts fetch, instead of holding it open until GC.
          await response.text().catch(() => undefined);
          continue;
        }

        if (!response.ok) {
          const text = await response.text();
          try {
            const payload = JSON.parse(text) as { error?: unknown };
            if (payload.error === "transport_exhausted") {
              throw new TransportExhaustedError();
            }
          } catch (error) {
            if (error instanceof TransportExhaustedError) throw error;
          }
          throw new Error(`daemon request failed (${response.status}): ${text}`);
        }

        return (await response.json()) as T;
      } catch (error) {
        if (error instanceof Error && error.name === "AbortError") {
          if (signal?.aborted) {
            throw new Error(`daemon request cancelled (${method} ${route})`);
          }
          throw new Error(`daemon request timeout (${method} ${route})`);
        }
        throw error;
      } finally {
        clearTimeout(timer);
        signal?.removeEventListener("abort", forwardAbort);
      }
    }

    throw new Error("daemon unauthorized (token mismatch)");
  }
}
