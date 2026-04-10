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

  interface AuthState {
    authenticated: boolean;
    user: AuthUser | null;
    error?: string;
  }

  interface ServerInfo {
    id: string;
    name: string;
    region: string;
    country: string;
    cloak: {
      remoteHost: string;
      uid: string;
      publicKey: string;
    };
  }

  interface DaemonApi {
    getStatus: () => Promise<StatusResponse & { killSwitchActive?: boolean }>;
    connect: (profileId: string) => Promise<OkResponse>;
    disconnect: () => Promise<OkResponse>;
    getLogs: (since?: number) => Promise<LogEntry[]>;
    getConfig: () => Promise<ConfigResponse>;
    setConfig: (profiles: Profile[]) => Promise<OkResponse>;
    getAppVersion: () => Promise<string>;
  }

  interface PangeaApi {
    login: (vpnToken: string) => Promise<AuthState>;
    logout: () => Promise<void>;
    getAuthState: () => Promise<AuthState>;
    getServers: () => Promise<ServerInfo[]>;
    provisionAndConnect: (serverId: string) => Promise<OkResponse>;
    setDoh: (enabled: boolean) => Promise<void>;
    getDoh: () => Promise<boolean>;
    setDirectIp: (enabled: boolean) => Promise<void>;
    getDirectIp: () => Promise<boolean>;
    setDirectIpOnly: (enabled: boolean) => Promise<void>;
    getDirectIpOnly: () => Promise<boolean>;
    getCachedServers: () => Promise<ServerInfo[]>;
    cacheServers: (servers: ServerInfo[]) => Promise<void>;
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
    openExternal?: (url: string) => Promise<void>;
    onAuthInvalidated?: (callback: () => void) => void;
  }
}

export {};
