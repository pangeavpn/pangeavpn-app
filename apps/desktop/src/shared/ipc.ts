import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse
} from "@pangeavpn/shared-types";

export const IPC_CHANNELS = {
  getStatus: "daemon:getStatus",
  connect: "daemon:connect",
  disconnect: "daemon:disconnect",
  getLogs: "daemon:getLogs",
  getConfig: "daemon:getConfig",
  setConfig: "daemon:setConfig",
  getAppVersion: "app:getAppVersion",
  authLogin: "auth:login",
  authLogout: "auth:logout",
  authGetState: "auth:getState",
  getServers: "pangea:getServers",
  provisionAndConnect: "pangea:provisionAndConnect",
  cancelConnect: "pangea:cancelConnect",
  provisionAndSwitch: "pangea:provisionAndSwitch",
  setDoh: "pangea:setDoh",
  getDoh: "pangea:getDoh",
  setDirectIp: "pangea:setDirectIp",
  getDirectIp: "pangea:getDirectIp",
  setDirectIpOnly: "pangea:setDirectIpOnly",
  getDirectIpOnly: "pangea:getDirectIpOnly",
  setAllowLan: "pangea:setAllowLan",
  getAllowLan: "pangea:getAllowLan",
  setWireguardMtu: "settings:setWireguardMtu",
  getWireguardMtu: "settings:getWireguardMtu",
  setCustomDns: "settings:setCustomDns",
  getCustomDns: "settings:getCustomDns",
  setPreferredTransport: "settings:setPreferredTransport",
  getPreferredTransport: "settings:getPreferredTransport",
  setLaunchAtStartup: "settings:setLaunchAtStartup",
  getLaunchAtStartup: "settings:getLaunchAtStartup",
  setAlwaysConnected: "settings:setAlwaysConnected",
  getAlwaysConnected: "settings:getAlwaysConnected",
  getLastServer: "settings:getLastServer",
  clearLastServer: "settings:clearLastServer",
  getLocale: "settings:getLocale",
  setLocale: "settings:setLocale",
  getIsPackaged: "app:getIsPackaged",
  getCachedServers: "pangea:getCachedServers",
  cacheServers: "pangea:cacheServers",
  listDevices: "pangea:listDevices",
  removeDevice: "pangea:removeDevice",
  getSubscription: "pangea:getSubscription",
  checkForUpdates: "app:checkForUpdates",
  downloadAppUpdate: "app:downloadAppUpdate",
  installUpdate: "app:installUpdate",
  updateAvailable: "app:updateAvailable",
  updateNotAvailable: "app:updateNotAvailable",
  updateError: "app:updateError",
  updateDownloadProgress: "app:updateDownloadProgress",
  updateDownloaded: "app:updateDownloaded"
} as const;

export interface DaemonApi {
  getStatus: () => Promise<StatusResponse>;
  connect: (profileId: string) => Promise<OkResponse>;
  disconnect: () => Promise<OkResponse>;
  getLogs: (since?: number) => Promise<LogEntry[]>;
  getConfig: () => Promise<ConfigResponse>;
  setConfig: (profiles: Profile[]) => Promise<OkResponse>;
  getAppVersion: () => Promise<string>;
}

export interface AuthUser {
  email: string;
  name: string;
}

export interface AuthState {
  authenticated: boolean;
  user: AuthUser | null;
  error?: string;
  friendlyName?: string | null;
}

export interface ServerInfo {
  id: string;
  name: string;
  region: string;
  country: string;
  /** Current server load 0–100 (composite CPU/memory). Absent/null when unknown or from an older hub. */
  load?: number | null;
  cloak: {
    remoteHost: string;
    uid: string;
    publicKey: string;
    // Optional cover SNI advertised by the hub (daemon defaults to www.microsoft.com when absent).
    serverName?: string;
  };
  /**
   * NaiveProxy fallback connection info, present only when the hub node has
   * NaiveProxy configured. Static per-node config (unlike cloak's per-device
   * `uid`) — see NaiveProfileSchema in @pangeavpn/shared-types for the full
   * daemon-facing shape; this omits `localPort`, which is daemon-assigned.
   */
  naive?: {
    remoteHost: string;
    /**
     * The endpoint's address, when the hub names one for this transport
     * specifically. Absent means it terminates on the node `cloak.remoteHost`
     * already names. Either way the client never resolves `remoteHost` itself:
     * that would leak our node domains to a third-party resolver, and it cannot
     * work behind an engaged Lockdown lock, which blocks DNS.
     */
    remoteIp?: string;
    remotePort: number;
    username: string;
    password: string;
    // Cover SNI presented during the TLS handshake (naive's --proxy host).
    serverName?: string;
  };
  /**
   * VLESS+REALITY connection info, present only when the hub node has
   * reality configured. Static per-node config, same shape as naive above —
   * see RealityProfileSchema in @pangeavpn/shared-types for the full
   * daemon-facing shape; this omits `localPort`/`targetPort`, which are
   * daemon-assigned/defaulted.
   */
  reality?: {
    remoteHost: string;
    /** Per-transport endpoint address; see naive.remoteIp above. */
    remoteIp?: string;
    remotePort: number;
    uuid: string;
    publicKey: string;
    shortId: string;
    flow?: string;
    // REALITY SNI / camouflage target hostname.
    serverName?: string;
  };
  /**
   * Hysteria2 (QUIC + Salamander obfuscation) connection info, present
   * only when the hub node has Hysteria2 configured. Static per-node
   * config, same as NaiveProxy — see Hysteria2ProfileSchema in
   * @pangeavpn/shared-types for the full daemon-facing shape; this omits
   * `localPort`, which is daemon-assigned.
   */
  hysteria2?: {
    remoteHost: string;
    /** Per-transport endpoint address; see naive.remoteIp above. */
    remoteIp?: string;
    remotePort: number;
    password: string;
    obfsPassword: string;
    serverName?: string;
    // Base64 SPKI SHA-256 pin for the node's self-signed cert; the daemon
    // verifies against this instead of a CA chain.
    pinSha256?: string;
  };
  /**
   * Tor Snowflake (WebRTC rendezvous) connection info, present only when the
   * hub node has Snowflake configured. Static per-node config, same as
   * Hysteria2 — see SnowflakeProfileSchema in @pangeavpn/shared-types for
   * the full daemon-facing shape; this omits `localPort`, which is
   * daemon-assigned. Unlike the other transports there is no single
   * `remoteHost`: rendezvous happens against `brokerURL` (optionally via
   * `frontDomains` or `ampCacheURL`), and the actual data-plane peer is a
   * volunteer proxy discovered dynamically per-session.
   */
  snowflake?: {
    brokerURL: string;
    bridgeFingerprint: string;
    frontDomains?: string[];
    ampCacheURL?: string;
    iceServers?: string[];
  };
}

export interface DeviceInfo {
  id: string;
  friendlyName: string | null;
  createdAt: string;
  status: string;
}

/**
 * Result of a connect attempt. `error: "cancelled"` means the user stopped it —
 * the caller should return to idle, not report a failure. Kept separate from
 * the daemon's bare OkResponse, which the zod schema pins to `{ ok }` alone.
 */
export interface ConnectResult {
  ok: boolean;
  error?: string;
  serverId?: string;
}

export interface SubscriptionInfo {
  status: "trialing" | "active" | "past_due" | "canceled" | "unpaid" | "incomplete" | "none";
  /**
   * May this account connect right now? Computed by the hub with the same rule
   * its register routes enforce — never re-derive it from `status`, which stays
   * "active" forever on prepaid (crypto/guest) plans even after they lapse.
   * Older hubs omit it; treat a missing value as entitled so the app doesn't
   * lock out a paying customer talking to one.
   */
  entitled?: boolean;
  /** True only for auto-renewing Stripe subs not set to cancel. Crypto/guest (prepaid) plans are always false. */
  renews: boolean;
  /** End of the current period — the renewal date if it renews, otherwise the expiry date. ISO string or null. */
  expiresAt: string | null;
}

export interface PangeaApi {
  login: (vpnToken: string) => Promise<AuthState>;
  logout: () => Promise<void>;
  getAuthState: () => Promise<AuthState>;
  getServers: () => Promise<ServerInfo[]>;
  provisionAndConnect: (serverIds: string[]) => Promise<ConnectResult>;
  /** Stop the in-flight connect attempt. No-op when nothing is connecting. */
  cancelConnect: () => Promise<void>;
  provisionAndSwitch: (serverIds: string[]) => Promise<ConnectResult>;
  setDoh: (enabled: boolean) => Promise<void>;
  getDoh: () => Promise<boolean>;
  setDirectIp: (enabled: boolean) => Promise<void>;
  getDirectIp: () => Promise<boolean>;
  setDirectIpOnly: (enabled: boolean) => Promise<void>;
  getDirectIpOnly: () => Promise<boolean>;
  setAllowLan: (enabled: boolean) => Promise<void>;
  getAllowLan: () => Promise<boolean>;
  /** Resolves to the MTU actually stored — differs from `mtu` when it was rejected. */
  setWireguardMtu: (mtu: number) => Promise<number>;
  getWireguardMtu: () => Promise<number>;
  /** Empty restores the DNS servers supplied by the VPN server. */
  setCustomDns: (value: string) => Promise<string[]>;
  getCustomDns: () => Promise<string[]>;
  setPreferredTransport: (value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "snowflake") => Promise<void>;
  getPreferredTransport: () => Promise<"auto" | "cloak" | "naive" | "reality" | "hysteria2" | "snowflake">;
  setLaunchAtStartup: (enabled: boolean) => Promise<void>;
  getLaunchAtStartup: () => Promise<boolean>;
  setAlwaysConnected: (enabled: boolean) => Promise<void>;
  getAlwaysConnected: () => Promise<boolean>;
  getLastServer: () => Promise<{ lastServerId: string | null; lastProfileId: string | null }>;
  clearLastServer: () => Promise<void>;
  /** Stored language preference: a locale code, or "system" when unset. */
  getLocale: () => Promise<string>;
  setLocale: (locale: string) => Promise<void>;
  getIsPackaged: () => Promise<boolean>;
  getCachedServers: () => Promise<ServerInfo[]>;
  cacheServers: (servers: ServerInfo[]) => Promise<void>;
  listDevices: () => Promise<DeviceInfo[]>;
  removeDevice: (deviceId: string) => Promise<void>;
  getSubscription: () => Promise<SubscriptionInfo | null>;
}
