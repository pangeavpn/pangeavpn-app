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
  setDoh: "pangea:setDoh",
  getDoh: "pangea:getDoh",
  setDirectIp: "pangea:setDirectIp",
  getDirectIp: "pangea:getDirectIp",
  setDirectIpOnly: "pangea:setDirectIpOnly",
  getDirectIpOnly: "pangea:getDirectIpOnly",
  getCachedServers: "pangea:getCachedServers",
  cacheServers: "pangea:cacheServers",
  listDevices: "pangea:listDevices",
  removeDevice: "pangea:removeDevice",
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
  cloak: {
    remoteHost: string;
    uid: string;
    publicKey: string;
  };
}

export interface DeviceInfo {
  id: string;
  friendlyName: string | null;
  createdAt: string;
  status: string;
}

export interface PangeaApi {
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
  listDevices: () => Promise<DeviceInfo[]>;
  removeDevice: (deviceId: string) => Promise<void>;
}
