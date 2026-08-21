import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse,
} from "@pangeavpn/shared-types";

declare global {
  interface AuthUser {
    email: string;
    name: string;
  }

  /** Mirrors ConnectResult in src/shared/ipc.ts — the renderer can't import it
   *  (separate tsconfigs), so keep the two in step. */
  interface ConnectResult {
    ok: boolean;
    error?: string;
    serverId?: string;
  }

  interface AuthState {
    authenticated: boolean;
    user: AuthUser | null;
    error?: string;
    friendlyName?: string | null;
  }

  /**
   * Renderer-facing view of a server: display fields plus per-transport
   * booleans, none of the node credentials. Mirrors PublicServerInfo in
   * shared/ipc.ts — the renderer never receives the full credential-bearing
   * shape, which stays in the main process.
   */
  interface ServerInfo {
    id: string;
    name: string;
    region: string;
    country: string;
    load?: number | null;
    // Never populated by the main process (kept optional only so older test
    // mocks built against the pre-redaction shape still type-check).
    cloak?: { remoteHost: string; uid: string; publicKey: string };
    naive?: boolean;
    reality?: boolean;
    hysteria2?: boolean;
    shadowsocks?: boolean;
    snowflake?: boolean;
  }

  interface DaemonApi {
    getStatus: () => Promise<StatusResponse & { killSwitchActive?: boolean }>;
    connect: (profileId: string) => Promise<OkResponse>;
    disconnect: () => Promise<OkResponse>;
    getLogs: (since?: number) => Promise<LogEntry[]>;
    getConfig: () => Promise<ConfigResponse>;
    setConfig: (profiles: Profile[]) => Promise<OkResponse>;
    restartDaemon: () => Promise<{ ok: boolean; error?: string }>;
    getAppVersion: () => Promise<string>;
  }

  interface DeviceInfo {
    id: string;
    friendlyName: string | null;
    createdAt: string;
    status: string;
  }

  interface SubscriptionInfo {
    status: "trialing" | "active" | "past_due" | "canceled" | "unpaid" | "incomplete" | "none";
    /** Hub's verdict on whether this account may connect. Never re-derive from
     *  status: prepaid plans stay "active" after they lapse. Absent on older
     *  hubs — treat that as entitled. */
    entitled?: boolean;
    renews: boolean;
    expiresAt: string | null;
  }

  type HubMethodName = "directIp" | "shadowsocks" | "fronted" | "normal";

  interface HubMethodFlags {
    directIp: boolean;
    shadowsocks: boolean;
    fronted: boolean;
    normal: boolean;
  }

  /** Mirrors HubStatus in src/shared/hubMethods.ts — `active` is the method
   *  carrying hub traffic right now, `detail` the address it won on. */
  interface HubStatus {
    methods: HubMethodFlags;
    active: HubMethodName | null;
    detail: string | null;
  }

  /** Mirrors HubMethodTestResult in src/shared/hubMethods.ts. */
  interface HubMethodTestResult {
    method: HubMethodName;
    ok: boolean;
    detail?: string;
    unavailable?: "noAddress" | "noCredentials" | "noRelay" | "busy";
    ms: number;
  }

  interface PangeaApi {
    login: (vpnToken: string) => Promise<AuthState>;
    logout: () => Promise<void>;
    getAuthState: () => Promise<AuthState>;
    getServers: () => Promise<ServerInfo[]>;
    provisionAndConnect: (serverIds: string[]) => Promise<ConnectResult>;
    cancelConnect: () => Promise<void>;
    provisionAndSwitch: (serverIds: string[]) => Promise<ConnectResult>;
    setDoh: (enabled: boolean) => Promise<void>;
    getDoh: () => Promise<boolean>;
    setHubMethod: (
      method: HubMethodName,
      enabled: boolean
    ) => Promise<{ methods: HubMethodFlags; applied: boolean }>;
    getHubMethods: () => Promise<HubMethodFlags>;
    getHubStatus: () => Promise<HubStatus>;
    testHubMethod: (method: HubMethodName) => Promise<HubMethodTestResult>;
    onHubStatusChanged: (callback: (status: HubStatus) => void) => () => void;
    setAllowLan: (enabled: boolean) => Promise<void>;
    getAllowLan: () => Promise<boolean>;
    /** Resolves to the MTU actually stored — differs from `mtu` when it was rejected. */
    setWireguardMtu: (mtu: number) => Promise<number>;
    getWireguardMtu: () => Promise<number>;
    /** Empty restores the DNS servers supplied by the VPN server. */
    setCustomDns: (value: string) => Promise<string[]>;
    getCustomDns: () => Promise<string[]>;
    setPreferredTransport: (value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard") => Promise<void>;
    getPreferredTransport: () => Promise<"auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard">;
    setLaunchAtStartup: (enabled: boolean) => Promise<void>;
    getLaunchAtStartup: () => Promise<boolean>;
    setLockdown: (enabled: boolean) => Promise<void>;
    getLockdown: () => Promise<boolean>;
    setAutoConnect: (enabled: boolean) => Promise<void>;
    getAutoConnect: () => Promise<boolean>;
    getLastServer: () => Promise<{ lastServerId: string | null; lastProfileId: string | null }>;
    clearLastServer: () => Promise<void>;
    getLocale: () => Promise<string>;
    setLocale: (locale: string) => Promise<void>;
    getIsPackaged: () => Promise<boolean>;
    getCachedServers: () => Promise<ServerInfo[]>;
    cacheServers: (servers: ServerInfo[]) => Promise<void>;
    listDevices: () => Promise<DeviceInfo[]>;
    removeDevice: (deviceId: string) => Promise<void>;
    getSubscription: () => Promise<SubscriptionInfo | null>;
    /** Backed by the main-process secure store — never localStorage. */
    rememberAccountNumber: (accountNumber: string) => Promise<void>;
    getRememberedAccountNumber: () => Promise<string | null>;
    clearRememberedAccountNumber: () => Promise<void>;
  }

  interface AutoUpdaterApi {
    checkForUpdates: () => Promise<{ version: string; releaseNotes?: string } | null>;
    downloadUpdate: () => Promise<void>;
    installUpdate: () => void;
    onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string; macOnly?: boolean }) => void) => () => void;
    onUpdateNotAvailable: (callback: () => void) => () => void;
    onUpdateError: (callback: (message: string) => void) => () => void;
  }

  interface Window {
    daemonApi?: DaemonApi;
    pangeaApi?: PangeaApi;
    autoUpdater?: AutoUpdaterApi;
    appPlatform?: NodeJS.Platform;
    openExternal?: (url: string) => Promise<void>;
    onAuthInvalidated?: (callback: () => void) => () => void;
  }
}

export {};
