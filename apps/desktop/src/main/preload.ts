import { contextBridge, ipcRenderer } from "electron";

// IPC channel strings inlined here because sandbox mode only allows
// require('electron') — no relative imports. Keep in sync with ../shared/ipc.ts.
const CH = {
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
  provisionAndSwitch: "pangea:provisionAndSwitch",
  setDoh: "pangea:setDoh",
  getDoh: "pangea:getDoh",
  setDirectIp: "pangea:setDirectIp",
  getDirectIp: "pangea:getDirectIp",
  setDirectIpOnly: "pangea:setDirectIpOnly",
  getDirectIpOnly: "pangea:getDirectIpOnly",
  setAllowLan: "pangea:setAllowLan",
  getAllowLan: "pangea:getAllowLan",
  setLaunchAtStartup: "settings:setLaunchAtStartup",
  getLaunchAtStartup: "settings:getLaunchAtStartup",
  setAlwaysConnected: "settings:setAlwaysConnected",
  getAlwaysConnected: "settings:getAlwaysConnected",
  getLastServer: "settings:getLastServer",
  clearLastServer: "settings:clearLastServer",
  getIsPackaged: "app:getIsPackaged",
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
  updateDownloaded: "app:updateDownloaded",
} as const;

const daemonApi = {
  getStatus: () => ipcRenderer.invoke(CH.getStatus),
  connect: (profileId: string) => ipcRenderer.invoke(CH.connect, profileId),
  disconnect: () => ipcRenderer.invoke(CH.disconnect),
  getLogs: (since?: number) => ipcRenderer.invoke(CH.getLogs, since),
  getConfig: () => ipcRenderer.invoke(CH.getConfig),
  setConfig: (profiles: unknown[]) => ipcRenderer.invoke(CH.setConfig, profiles),
  getAppVersion: () => ipcRenderer.invoke(CH.getAppVersion),
};

const pangeaApi = {
  login: (vpnToken: string) => ipcRenderer.invoke(CH.authLogin, vpnToken),
  logout: () => ipcRenderer.invoke(CH.authLogout),
  getAuthState: () => ipcRenderer.invoke(CH.authGetState),
  getServers: () => ipcRenderer.invoke(CH.getServers),
  provisionAndConnect: (serverId: string) =>
    ipcRenderer.invoke(CH.provisionAndConnect, serverId),
  provisionAndSwitch: (serverId: string) =>
    ipcRenderer.invoke(CH.provisionAndSwitch, serverId),
  setDoh: (enabled: boolean) => ipcRenderer.invoke(CH.setDoh, enabled),
  getDoh: () => ipcRenderer.invoke(CH.getDoh),
  setDirectIp: (enabled: boolean) => ipcRenderer.invoke(CH.setDirectIp, enabled),
  getDirectIp: () => ipcRenderer.invoke(CH.getDirectIp),
  setDirectIpOnly: (enabled: boolean) => ipcRenderer.invoke(CH.setDirectIpOnly, enabled),
  getDirectIpOnly: () => ipcRenderer.invoke(CH.getDirectIpOnly),
  setAllowLan: (enabled: boolean) => ipcRenderer.invoke(CH.setAllowLan, enabled),
  getAllowLan: () => ipcRenderer.invoke(CH.getAllowLan),
  setLaunchAtStartup: (enabled: boolean) => ipcRenderer.invoke(CH.setLaunchAtStartup, enabled),
  getLaunchAtStartup: () => ipcRenderer.invoke(CH.getLaunchAtStartup),
  setAlwaysConnected: (enabled: boolean) => ipcRenderer.invoke(CH.setAlwaysConnected, enabled),
  getAlwaysConnected: () => ipcRenderer.invoke(CH.getAlwaysConnected),
  getLastServer: () => ipcRenderer.invoke(CH.getLastServer),
  clearLastServer: () => ipcRenderer.invoke(CH.clearLastServer),
  getIsPackaged: () => ipcRenderer.invoke(CH.getIsPackaged),
  getCachedServers: () => ipcRenderer.invoke(CH.getCachedServers),
  cacheServers: (servers: unknown[]) => ipcRenderer.invoke(CH.cacheServers, servers),
  listDevices: () => ipcRenderer.invoke(CH.listDevices),
  removeDevice: (deviceId: string) => ipcRenderer.invoke(CH.removeDevice, deviceId),
};

const autoUpdaterApi = {
  checkForUpdates: () => ipcRenderer.invoke(CH.checkForUpdates),
  downloadUpdate: () => ipcRenderer.invoke(CH.downloadAppUpdate),
  installUpdate: () => ipcRenderer.invoke(CH.installUpdate),
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string }) => void) => {
    ipcRenderer.on(CH.updateAvailable, (_event, info) => callback(info));
  },
  onUpdateNotAvailable: (callback: () => void) => {
    ipcRenderer.on(CH.updateNotAvailable, () => callback());
  },
  onUpdateError: (callback: (message: string) => void) => {
    ipcRenderer.on(CH.updateError, (_event, message: string) => callback(message));
  },
  onDownloadProgress: (callback: (percent: number) => void) => {
    ipcRenderer.on(CH.updateDownloadProgress, (_event, percent: number) => callback(percent));
  },
  onUpdateDownloaded: (callback: () => void) => {
    ipcRenderer.on(CH.updateDownloaded, () => callback());
  },
};

contextBridge.exposeInMainWorld("daemonApi", daemonApi);
contextBridge.exposeInMainWorld("pangeaApi", pangeaApi);
contextBridge.exposeInMainWorld("autoUpdater", autoUpdaterApi);
contextBridge.exposeInMainWorld("appPlatform", process.platform);
contextBridge.exposeInMainWorld("openExternal", (url: string) => ipcRenderer.invoke("app:openExternal", url));
contextBridge.exposeInMainWorld("onAuthInvalidated", (callback: () => void) => {
  ipcRenderer.on("auth:invalidated", () => callback());
});
