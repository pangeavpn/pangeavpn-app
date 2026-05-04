import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse
} from "@pangeavpn/shared-types";

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
    this.connectTimeoutMs = 45000;
    this.disconnectTimeoutMs = 45000;
  }

  async getStatus(): Promise<StatusResponse> {
    return this.request<StatusResponse>("GET", "/status");
  }

  async connect(profileId: string, opts?: { allowLAN?: boolean }): Promise<OkResponse> {
    const body: Record<string, unknown> = { profileId };
    if (opts?.allowLAN) {
      body.allowLAN = true;
    }
    return this.request<OkResponse>("POST", "/connect", body, this.connectTimeoutMs);
  }

  async disconnect(): Promise<OkResponse> {
    return this.request<OkResponse>("POST", "/disconnect", undefined, this.disconnectTimeoutMs);
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

  private async request<T>(method: string, route: string, body?: unknown, timeoutMs?: number): Promise<T> {
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

      let response: Response;
      try {
        response = await fetch(`${this.baseUrl}${route}`, {
          method,
          headers: {
            "Content-Type": "application/json",
            Authorization: `Bearer ${token}`
          },
          body: body === undefined ? undefined : JSON.stringify(body),
          signal: controller.signal
        });
      } catch (error) {
        if (error instanceof Error && error.name === "AbortError") {
          throw new Error(`daemon request timeout (${method} ${route})`);
        }
        throw error;
      } finally {
        clearTimeout(timer);
      }

      if (response.status === 401) {
        continue;
      }

      if (!response.ok) {
        const text = await response.text();
        throw new Error(`daemon request failed (${response.status}): ${text}`);
      }

      return (await response.json()) as T;
    }

    throw new Error("daemon unauthorized (token mismatch)");
  }
}
