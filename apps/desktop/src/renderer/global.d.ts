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

  interface ServerInfo {
    id: string;
    name: string;
    region: string;
    country: string;
    load?: number | null;
    cloak: {
      remoteHost: string;
      uid: string;
      publicKey: string;
    };
    // Optional per-node transport blocks — present only when the hub node has
    // that transport configured. Mirrors ServerInfo in shared/ipc.ts; the
    // renderer uses their presence to filter the server list per transport.
    naive?: {
      remoteHost: string;
      remotePort: number;
      username: string;
      password: string;
      serverName?: string;
    };
    reality?: {
      remoteHost: string;
      remotePort: number;
      uuid: string;
      publicKey: string;
      shortId: string;
      flow?: string;
      serverName?: string;
    };
    hysteria2?: {
      remoteHost: string;
      remotePort: number;
      password: string;
      obfsPassword: string;
      serverName?: string;
      pinSha256?: string;
    };
    shadowsocks?: {
      remoteHost: string;
      remoteIp?: string;
      remotePort: number;
      method: string;
      password: string;
      targetHost?: string;
      targetPort?: number;
      udpOverTcp?: boolean;
    };
    controlPlaneShadowsocks?: {
      remoteHost: string;
      remotePort: number;
      method: string;
      password: string;
    };
    snowflake?: {
      brokerURL: string;
      bridgeFingerprint: string;
      frontDomains?: string[];
      ampCacheURL?: string;
      iceServers?: string[];
    };
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
      method: "directIp" | "shadowsocks" | "normal",
      enabled: boolean
    ) => Promise<{
      methods: { directIp: boolean; shadowsocks: boolean; normal: boolean };
      applied: boolean;
    }>;
    getHubMethods: () => Promise<{ directIp: boolean; shadowsocks: boolean; normal: boolean }>;
    setAllowLan: (enabled: boolean) => Promise<void>;
    getAllowLan: () => Promise<boolean>;
    /** Resolves to the MTU actually stored — differs from `mtu` when it was rejected. */
    setWireguardMtu: (mtu: number) => Promise<number>;
    getWireguardMtu: () => Promise<number>;
    /** Empty restores the DNS servers supplied by the VPN server. */
    setCustomDns: (value: string) => Promise<string[]>;
    getCustomDns: () => Promise<string[]>;
    setPreferredTransport: (value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake") => Promise<void>;
    getPreferredTransport: () => Promise<"auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake">;
    setLaunchAtStartup: (enabled: boolean) => Promise<void>;
    getLaunchAtStartup: () => Promise<boolean>;
    setAlwaysConnected: (enabled: boolean) => Promise<void>;
    getAlwaysConnected: () => Promise<boolean>;
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
  }

  interface AutoUpdaterApi {
    checkForUpdates: () => Promise<{ version: string; releaseNotes?: string } | null>;
    downloadUpdate: () => Promise<void>;
    installUpdate: () => void;
    onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string; macOnly?: boolean }) => void) => void;
    onUpdateNotAvailable: (callback: () => void) => void;
    onUpdateError: (callback: (message: string) => void) => void;
    onDownloadProgress: (callback: (percent: number) => void) => void;
    onUpdateDownloaded: (callback: () => void) => void;
  }

  interface Window {
    daemonApi?: DaemonApi;
    pangeaApi?: PangeaApi;
    autoUpdater?: AutoUpdaterApi;
    appPlatform?: NodeJS.Platform;
    openExternal?: (url: string) => Promise<void>;
    onAuthInvalidated?: (callback: () => void) => void;
  }
}

export {};
