import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse
} from "@pangeavpn/shared-types";
import type {
  HubMethod,
  HubMethods,
  HubMethodTestResult,
  HubStatus
} from "./hubMethods";
import type { MultihopPrefs } from "./multihop";

export type {
  HubMethod,
  HubMethods,
  HubMethodTestResult,
  HubMethodUnavailable,
  HubStatus
} from "./hubMethods";

/** Result of setHubMethod: `applied` is false when the last method was kept on. */
export interface HubMethodResult {
  methods: HubMethods;
  applied: boolean;
}

export const IPC_CHANNELS = {
  getStatus: "daemon:getStatus",
  connect: "daemon:connect",
  disconnect: "daemon:disconnect",
  getLogs: "daemon:getLogs",
  getConfig: "daemon:getConfig",
  setConfig: "daemon:setConfig",
  restartDaemon: "daemon:restart",
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
  setHubMethod: "pangea:setHubMethod",
  getHubMethods: "pangea:getHubMethods",
  getHubStatus: "pangea:getHubStatus",
  testHubMethod: "pangea:testHubMethod",
  hubStatusChanged: "pangea:hubStatusChanged",
  setAllowLan: "pangea:setAllowLan",
  getAllowLan: "pangea:getAllowLan",
  setPostQuantum: "settings:setPostQuantum",
  getPostQuantum: "settings:getPostQuantum",
  setWireguardMtu: "settings:setWireguardMtu",
  getWireguardMtu: "settings:getWireguardMtu",
  setCustomDns: "settings:setCustomDns",
  getCustomDns: "settings:getCustomDns",
  setHubInTunnel: "settings:setHubInTunnel",
  getHubInTunnel: "settings:getHubInTunnel",
  setPreferredTransport: "settings:setPreferredTransport",
  getPreferredTransport: "settings:getPreferredTransport",
  setLaunchAtStartup: "settings:setLaunchAtStartup",
  getLaunchAtStartup: "settings:getLaunchAtStartup",
  setLockdown: "settings:setLockdown",
  getLockdown: "settings:getLockdown",
  setAutoConnect: "settings:setAutoConnect",
  setDeadDrop: "settings:setDeadDrop",
  getDeadDrop: "settings:getDeadDrop",
  getAutoConnect: "settings:getAutoConnect",
  setNotifications: "settings:setNotifications",
  getNotifications: "settings:getNotifications",
  getLastServer: "settings:getLastServer",
  clearLastServer: "settings:clearLastServer",
  setMultihop: "settings:setMultihop",
  getMultihop: "settings:getMultihop",
  getLocale: "settings:getLocale",
  setLocale: "settings:setLocale",
  getIsPackaged: "app:getIsPackaged",
  getCachedServers: "pangea:getCachedServers",
  cacheServers: "pangea:cacheServers",
  listDevices: "pangea:listDevices",
  removeDevice: "pangea:removeDevice",
  renameDevice: "pangea:renameDevice",
  getSubscription: "pangea:getSubscription",
  checkForUpdates: "app:checkForUpdates",
  downloadAppUpdate: "app:downloadAppUpdate",
  installUpdate: "app:installUpdate",
  updateAvailable: "app:updateAvailable",
  updateNotAvailable: "app:updateNotAvailable",
  updateError: "app:updateError",
  openExternal: "app:openExternal",
  openLogsFolder: "app:openLogsFolder",
  sendDiagnostics: "app:sendDiagnostics",
  authInvalidated: "auth:invalidated",
  rememberAccountNumber: "auth:rememberAccountNumber",
  getRememberedAccountNumber: "auth:getRememberedAccountNumber",
  getAccountNumber: "auth:getAccountNumber",
  clearRememberedAccountNumber: "auth:clearRememberedAccountNumber"
} as const;

export interface DaemonApi {
  getStatus: () => Promise<StatusResponse>;
  connect: (profileId: string) => Promise<OkResponse>;
  disconnect: () => Promise<OkResponse>;
  getLogs: (since?: number) => Promise<LogEntry[]>;
  getConfig: () => Promise<ConfigResponse>;
  setConfig: (profiles: Profile[]) => Promise<OkResponse>;
  restartDaemon: () => Promise<{ ok: boolean; error?: string }>;
  getAppVersion: () => Promise<string>;
}

export interface AuthUser {
  email: string;
  name: string;
}

/** Outcome of an anonymous diagnostics upload. */
export type DiagnosticsSendResult =
  | { ok: true; reportCode: string }
  | { ok: false; reason: "unreachable" | "rejected" };

/** Why a sign-in failed. A stable code, not prose: the renderer localises it,
 *  and only INVALID_ACCOUNT_NUMBER blames what the user typed. */
export type LoginErrorCode =
  | "INVALID_ACCOUNT_NUMBER"
  | "SUBSCRIPTION_EXPIRED"
  | "DEVICE_LIMIT_REACHED"
  | "RATE_LIMITED"
  | "SERVER_ERROR"
  | "HUB_UNREACHABLE"
  | "TIMEOUT"
  | "REGISTRATION_FAILED"
  | "LOCAL_STORAGE_FAILED"
  | "UNKNOWN";

export interface AuthState {
  authenticated: boolean;
  user: AuthUser | null;
  error?: LoginErrorCode;
  friendlyName?: string | null;
}

export interface ServerInfo {
  id: string;
  name: string;
  region: string;
  country: string;
  /** Current server load 0–100 (composite CPU/memory). Absent/null when unknown or from an older hub. */
  load?: number | null;
  /** Hub flag: this node relays multihop sessions and may be offered as an entry. */
  multihop?: boolean;
  cloak: {
    remoteHost: string;
    uid: string;
    publicKey: string;
    // Optional cover SNI advertised by the hub (daemon defaults to www.microsoft.com when absent).
    serverName?: string;
  };
  /** NaiveProxy fallback, present only when the hub node has it configured. See
   *  NaiveProfileSchema in @pangeavpn/shared-types; omits daemon-assigned `localPort`. */
  naive?: {
    remoteHost: string;
    /** Per-transport endpoint address, when the hub names one; the client never
     *  resolves `remoteHost` itself — that leaks node domains and breaks under Lockdown. */
    remoteIp?: string;
    remotePort: number;
    username: string;
    password: string;
    // Cover SNI presented during the TLS handshake (naive's --proxy host).
    serverName?: string;
  };
  /** VLESS+REALITY, present only when configured. See RealityProfileSchema in
   *  @pangeavpn/shared-types; omits daemon-assigned/defaulted `localPort`/`targetPort`. */
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
  /** Hysteria2 (QUIC + Salamander obfuscation), present only when configured.
   *  See Hysteria2ProfileSchema in @pangeavpn/shared-types; omits daemon-assigned `localPort`. */
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
    /** "start:end" UDP ranges the client hops across; absent means no hopping. */
    remotePorts?: string[];
  };
  /** Shadowsocks (AEAD / SS-2022), present only when the node has a public
   *  listener. `targetHost`/`targetPort` name the WireGuard listener it forwards to. */
  shadowsocks?: {
    remoteHost: string;
    /** Per-transport endpoint address; see naive.remoteIp above. */
    remoteIp?: string;
    remotePort: number;
    method: string;
    password: string;
    targetHost?: string;
    targetPort?: number;
    udpOverTcp?: boolean;
  };
  /** Shadowsocks listener that reaches the hub instead of WireGuard, used as a
   *  fallback path for account traffic. Per-region, but the same for all. */
  controlPlaneShadowsocks?: {
    remoteHost: string;
    remotePort: number;
    method: string;
    password: string;
  };
  /** The node's REALITY user that reaches only the hub; same role as
   *  controlPlaneShadowsocks. See shared/hubRealityCreds.ts. */
  controlPlaneReality?: {
    remoteHost: string;
    remotePort: number;
    uuid: string;
    publicKey: string;
    shortId: string;
    serverName: string;
  };
  /** Edge relays, repeated per region: this route answers with a bare array,
   *  so a top-level field would break clients that expect one. */
  frontedEndpoints?: string[];
  /** Tor Snowflake (WebRTC rendezvous), present only when configured. No single
   *  `remoteHost` — rendezvous is against `brokerURL`; the data peer is discovered per-session. */
  snowflake?: {
    brokerURL: string;
    bridgeFingerprint: string;
    frontDomains?: string[];
    ampCacheURL?: string;
    iceServers?: string[];
  };
}

/** Renderer-facing view of ServerInfo: display fields plus per-transport booleans,
 *  none of the credentials. Cloak is omitted — every node has it. */
export interface PublicServerInfo {
  id: string;
  name: string;
  region: string;
  country: string;
  load?: number | null;
  multihop?: boolean;
  naive?: boolean;
  reality?: boolean;
  hysteria2?: boolean;
  shadowsocks?: boolean;
  snowflake?: boolean;
}

/** Strips per-node transport credentials before a server list crosses into the renderer. */
export function toPublicServerInfo(server: ServerInfo): PublicServerInfo {
  return {
    id: server.id,
    name: server.name,
    region: server.region,
    country: server.country,
    load: server.load,
    multihop: server.multihop === true,
    naive: Boolean(server.naive),
    reality: Boolean(server.reality),
    hysteria2: Boolean(server.hysteria2),
    shadowsocks: Boolean(server.shadowsocks),
    snowflake: Boolean(server.snowflake)
  };
}

export interface DeviceInfo {
  id: string;
  friendlyName: string | null;
  createdAt: string;
  status: string;
  /** Set by the main process by matching identity pubkeys; rename-proof. */
  isCurrentDevice?: boolean;
}

/** Result of a connect attempt. `error: "cancelled"` means the user stopped it —
 *  return to idle, not a failure. Kept separate from the daemon's bare OkResponse. */
export interface ConnectResult {
  ok: boolean;
  error?: string;
  serverId?: string;
  /** Entry the session runs through; absent on a single-hop connection. */
  entryServerId?: string;
}

export interface SubscriptionInfo {
  status: "trialing" | "active" | "past_due" | "canceled" | "unpaid" | "incomplete" | "none";
  /** May this account connect right now, per the hub. Never re-derive from `status`
   *  (stays "active" forever on prepaid plans). Missing on old hubs means entitled. */
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
  getServers: () => Promise<PublicServerInfo[]>;
  provisionAndConnect: (serverIds: string[], entryServerId?: string | null) => Promise<ConnectResult>;
  /** Stop the in-flight connect attempt. No-op when nothing is connecting. */
  cancelConnect: () => Promise<void>;
  provisionAndSwitch: (serverIds: string[], entryServerId?: string | null) => Promise<ConnectResult>;
  setDoh: (enabled: boolean) => Promise<void>;
  getDoh: () => Promise<boolean>;
  /** Toggles one hub-connection method. Resolves `applied: false` and the
   *  unchanged state when the change would have left no method enabled. */
  setHubMethod: (method: HubMethod, enabled: boolean) => Promise<HubMethodResult>;
  getHubMethods: () => Promise<HubMethods>;
  /** Which method is carrying hub traffic right now, plus the switches. */
  getHubStatus: () => Promise<HubStatus>;
  /** Probes one method on its own. Never rejects on an unreachable hub. */
  testHubMethod: (method: HubMethod) => Promise<HubMethodTestResult>;
  /** Subscribes to path changes; returns an unsubscribe function. */
  onHubStatusChanged: (callback: (status: HubStatus) => void) => () => void;
  setAllowLan: (enabled: boolean) => Promise<void>;
  getAllowLan: () => Promise<boolean>;
  /** Post-quantum key for the tunnel. Applies on the next connect. */
  setPostQuantum: (enabled: boolean) => Promise<void>;
  getPostQuantum: () => Promise<boolean>;
  /** Resolves to the MTU actually stored — differs from `mtu` when it was rejected. */
  setWireguardMtu: (mtu: number) => Promise<number>;
  getWireguardMtu: () => Promise<number>;
  /** Empty restores the DNS servers supplied by the VPN server. */
  setCustomDns: (value: string) => Promise<string[]>;
  getCustomDns: () => Promise<string[]>;
  /** Developer option: send hub traffic through the tunnel, not around it. */
  setHubInTunnel: (enabled: boolean) => Promise<void>;
  getHubInTunnel: () => Promise<boolean>;
  setPreferredTransport: (value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard") => Promise<void>;
  getPreferredTransport: () => Promise<"auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard">;
  setLaunchAtStartup: (enabled: boolean) => Promise<void>;
  getLaunchAtStartup: () => Promise<boolean>;
  /** Kill switch stays armed while disconnected. Independent of auto-connect. */
  setLockdown: (enabled: boolean) => Promise<void>;
  getLockdown: () => Promise<boolean>;
  /** Reconnect to the last server on launch and after drops. Independent of lockdown. */
  setAutoConnect: (enabled: boolean) => Promise<void>;
  setDeadDrop: (enabled: boolean) => Promise<void>;
  getDeadDrop: () => Promise<boolean>;
  getAutoConnect: () => Promise<boolean>;
  getLastServer: () => Promise<{ lastServerId: string | null; lastProfileId: string | null; lastEntryServerId: string | null }>;
  clearLastServer: () => Promise<void>;
  setMultihop: (prefs: MultihopPrefs) => Promise<void>;
  getMultihop: () => Promise<MultihopPrefs>;
  /** Stored language preference: a locale code, or "system" when unset. */
  getLocale: () => Promise<string>;
  setLocale: (locale: string) => Promise<void>;
  getIsPackaged: () => Promise<boolean>;
  getCachedServers: () => Promise<PublicServerInfo[]>;
  cacheServers: (servers: PublicServerInfo[]) => Promise<void>;
  listDevices: () => Promise<DeviceInfo[]>;
  removeDevice: (deviceId: string) => Promise<void>;
  renameDevice: (deviceId: string, friendlyName: string) => Promise<void>;
  getSubscription: () => Promise<SubscriptionInfo | null>;
  /** Backed by the main-process secure store — never localStorage. */
  rememberAccountNumber: (accountNumber: string) => Promise<void>;
  getRememberedAccountNumber: () => Promise<string | null>;
  /** The credential this device is signed in with; null while signed out. */
  getAccountNumber: () => Promise<string | null>;
  clearRememberedAccountNumber: () => Promise<void>;
}
