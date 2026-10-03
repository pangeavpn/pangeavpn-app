import type {
  ConfigResponse,
  LogEntry,
  OkResponse,
  Profile,
  StatusResponse,
} from "@pangeavpn/shared-types";

declare global {
  /** Mirrors DiagnosticsSendResult in src/shared/ipc.ts. */
  type DiagnosticsSendResult =
    | { ok: true; reportCode: string }
    | { ok: false; reason: "unreachable" | "rejected" };

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
    /** Entry the session runs through; absent on a single-hop connection. */
    entryServerId?: string;
  }

  /** Mirrors MultihopPrefs in src/shared/multihop.ts. */
  interface MultihopPrefs {
    enabled: boolean;
    entryServerId: string | null;
  }

  /** Mirrors SplitTunnelConfig in src/shared/splitTunnel.ts. */
  interface SplitTunnelConfig {
    enabled: boolean;
    apps: string[];
    cidrs: string[];
    /** False when the service can't exclude apps here; ranges still work. */
    appsSupported: boolean;
    /** "" or a daemon code: classifierFailed, egressFailed, permitFailed, stackFailed, strictReversePath. */
    unavailableReason: string;
    active: boolean;
    /** Saved, but not yet applied to the live tunnel. */
    pending: boolean;
    /** The ranges stay in the tunnel on this connection: the server's routes can't fit them. */
    cidrsDropped: boolean;
  }

  /** Mirrors SplitTunnelErrorCode; "invalid" stands for a code this build doesn't know. */
  type SplitTunnelErrorCode =
    | "notAbsolute"
    | "tooLong"
    | "nul"
    | "unsupportedForm"
    | "systemProcess"
    | "tooBroad"
    | "ownImage"
    | "tooMany"
    | "duplicate"
    | "notIPv4"
    | "prefixTooShort"
    | "tooManyRoutes"
    | "invalid";

  /** `value` is the rejected entry: the rule, or the ranges token as typed. */
  interface SplitTunnelInvalid {
    field: "apps" | "cidrs";
    index: number;
    code: SplitTunnelErrorCode;
    value?: string;
  }

  type SplitTunnelResult =
    | { ok: true; config: SplitTunnelConfig }
    | { ok: false; invalid: SplitTunnelInvalid[] };

  /** Mirrors SplitTunnelAppEntry. `rule` is "" for unsupported rows; `id` is the icon key. */
  interface SplitTunnelAppEntry {
    id: string;
    name: string;
    rule: string;
    exe: string;
    kind: "file" | "dir" | "bundle";
    missing?: boolean;
    warning?: "inherits" | "mayNotWork";
    unsupported?: "flatpak" | "appimage" | "script";
  }

  interface SplitTunnelIcon {
    key: string;
    icon: string;
  }

  type SplitTunnelBrowseResult =
    | { ok: true; entry: SplitTunnelAppEntry }
    | { ok: false; reason: "ownImage" | "notAnApp" };

  /** Mirrors LoginErrorCode in shared/ipc.ts. */
  type LoginErrorCode =
    | "INVALID_ACCOUNT_NUMBER"
    | "SUBSCRIPTION_EXPIRED"
    | "DEVICE_LIMIT_REACHED"
    | "RATE_LIMITED"
    | "SERVER_ERROR"
    | "HUB_UNREACHABLE"
    | "TIMEOUT"
    | "REGISTRATION_FAILED"
    | "UNKNOWN";

  interface AuthState {
    authenticated: boolean;
    user: AuthUser | null;
    error?: LoginErrorCode;
    friendlyName?: string | null;
  }

  /** Renderer-facing view of a server: display fields plus per-transport
   *  booleans, none of the node credentials. Mirrors PublicServerInfo in shared/ipc.ts. */
  interface ServerInfo {
    id: string;
    name: string;
    region: string;
    country: string;
    load?: number | null;
    /** Hub flag: may be offered as a multihop entry. */
    multihop?: boolean;
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
    /** Set by the main process by matching identity pubkeys; rename-proof. */
    isCurrentDevice?: boolean;
  }

  interface SubscriptionInfo {
    status: "trialing" | "active" | "past_due" | "canceled" | "unpaid" | "incomplete" | "none";
    /** Hub's verdict on whether this account may connect — never re-derive from
     *  status, since prepaid plans stay "active" after they lapse. */
    entitled?: boolean;
    renews: boolean;
    expiresAt: string | null;
  }

  type HubMethodName = "directIp" | "reality" | "shadowsocks" | "fronted" | "normal";

  interface HubMethodFlags {
    directIp: boolean;
    reality: boolean;
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
    provisionAndConnect: (serverIds: string[], entryServerId?: string | null) => Promise<ConnectResult>;
    cancelConnect: () => Promise<void>;
    provisionAndSwitch: (serverIds: string[], entryServerId?: string | null) => Promise<ConnectResult>;
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
    setLockdown: (enabled: boolean) => Promise<void>;
    getLockdown: () => Promise<boolean>;
    setAutoConnect: (enabled: boolean) => Promise<void>;
    getAutoConnect: () => Promise<boolean>;
    setNotifications: (enabled: boolean) => Promise<void>;
    getNotifications: () => Promise<boolean>;
    setDeadDrop: (enabled: boolean) => Promise<void>;
    getDeadDrop: () => Promise<boolean>;
    getLastServer: () => Promise<{ lastServerId: string | null; lastProfileId: string | null; lastEntryServerId?: string | null }>;
    clearLastServer: () => Promise<void>;
    setMultihop: (prefs: MultihopPrefs) => Promise<void>;
    getMultihop: () => Promise<MultihopPrefs>;
    /** Null when the service predates split tunnelling; rejects when unreachable. */
    getSplitTunnel: () => Promise<SplitTunnelConfig | null>;
    /** Reject on anything but a validation failure; re-read with getSplitTunnel then. */
    setSplitTunnelEnabled: (enabled: boolean) => Promise<SplitTunnelResult>;
    setSplitTunnelApp: (rule: string, excluded: boolean) => Promise<SplitTunnelResult>;
    setSplitTunnelCidrs: (text: string) => Promise<SplitTunnelResult>;
    listSplitTunnelApps: (options?: { refresh?: boolean }) => Promise<SplitTunnelAppEntry[]>;
    describeSplitTunnelApps: (rules: string[]) => Promise<SplitTunnelAppEntry[]>;
    getSplitTunnelIcons: (keys: string[]) => Promise<SplitTunnelIcon[]>;
    /** Null when cancelled; adding the pick is up to the caller. */
    browseSplitTunnelApp: () => Promise<SplitTunnelBrowseResult | null>;
    getLocale: () => Promise<string>;
    setLocale: (locale: string) => Promise<void>;
    getIsPackaged: () => Promise<boolean>;
    getCachedServers: () => Promise<ServerInfo[]>;
    cacheServers: (servers: ServerInfo[]) => Promise<void>;
    listDevices: () => Promise<DeviceInfo[]>;
    removeDevice: (deviceId: string) => Promise<void>;
    renameDevice: (deviceId: string, friendlyName: string) => Promise<void>;
    getSubscription: () => Promise<SubscriptionInfo | null>;
    /** Backed by the main-process secure store — never localStorage. */
    rememberAccountNumber: (accountNumber: string) => Promise<void>;
    getRememberedAccountNumber: () => Promise<string | null>;
    getAccountNumber: () => Promise<string | null>;
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
    openLogsFolder?: () => Promise<boolean>;
    sendDiagnostics?: (note?: string) => Promise<DiagnosticsSendResult>;
    onAuthInvalidated?: (callback: () => void) => () => void;
  }
}

export {};
