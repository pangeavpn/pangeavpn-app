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
  setLockdown: "settings:setLockdown",
  getLockdown: "settings:getLockdown",
  setAutoConnect: "settings:setAutoConnect",
  getAutoConnect: "settings:getAutoConnect",
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
  openExternal: "app:openExternal",
  authInvalidated: "auth:invalidated",
  rememberAccountNumber: "auth:rememberAccountNumber",
  getRememberedAccountNumber: "auth:getRememberedAccountNumber",
  clearRememberedAccountNumber: "auth:clearRememberedAccountNumber",
} as const;

const daemonApi = {
  getStatus: () => ipcRenderer.invoke(CH.getStatus),
  connect: (profileId: string) => ipcRenderer.invoke(CH.connect, profileId),
  disconnect: () => ipcRenderer.invoke(CH.disconnect),
  getLogs: (since?: number) => ipcRenderer.invoke(CH.getLogs, since),
  getConfig: () => ipcRenderer.invoke(CH.getConfig),
  setConfig: (profiles: unknown[]) => ipcRenderer.invoke(CH.setConfig, profiles),
  restartDaemon: () => ipcRenderer.invoke(CH.restartDaemon),
  getAppVersion: () => ipcRenderer.invoke(CH.getAppVersion),
};

const pangeaApi = {
  login: (vpnToken: string) => ipcRenderer.invoke(CH.authLogin, vpnToken),
  logout: () => ipcRenderer.invoke(CH.authLogout),
  getAuthState: () => ipcRenderer.invoke(CH.authGetState),
  getServers: () => ipcRenderer.invoke(CH.getServers),
  provisionAndConnect: (serverIds: string[]) =>
    ipcRenderer.invoke(CH.provisionAndConnect, serverIds),
  cancelConnect: () => ipcRenderer.invoke(CH.cancelConnect),
  provisionAndSwitch: (serverIds: string[]) =>
    ipcRenderer.invoke(CH.provisionAndSwitch, serverIds),
  setDoh: (enabled: boolean) => ipcRenderer.invoke(CH.setDoh, enabled),
  getDoh: () => ipcRenderer.invoke(CH.getDoh),
  setHubMethod: (method: string, enabled: boolean) =>
    ipcRenderer.invoke(CH.setHubMethod, method, enabled),
  getHubMethods: () => ipcRenderer.invoke(CH.getHubMethods),
  setAllowLan: (enabled: boolean) => ipcRenderer.invoke(CH.setAllowLan, enabled),
  getAllowLan: () => ipcRenderer.invoke(CH.getAllowLan),
  setWireguardMtu: (mtu: number) => ipcRenderer.invoke(CH.setWireguardMtu, mtu),
  getWireguardMtu: () => ipcRenderer.invoke(CH.getWireguardMtu),
  setCustomDns: (value: string) => ipcRenderer.invoke(CH.setCustomDns, value),
  getCustomDns: () => ipcRenderer.invoke(CH.getCustomDns),
  setPreferredTransport: (value: "auto" | "cloak" | "naive" | "reality" | "hysteria2" | "shadowsocks" | "snowflake" | "wireguard") =>
    ipcRenderer.invoke(CH.setPreferredTransport, value),
  getPreferredTransport: () => ipcRenderer.invoke(CH.getPreferredTransport),
  setLaunchAtStartup: (enabled: boolean) => ipcRenderer.invoke(CH.setLaunchAtStartup, enabled),
  getLaunchAtStartup: () => ipcRenderer.invoke(CH.getLaunchAtStartup),
  setLockdown: (enabled: boolean) => ipcRenderer.invoke(CH.setLockdown, enabled),
  getLockdown: () => ipcRenderer.invoke(CH.getLockdown),
  setAutoConnect: (enabled: boolean) => ipcRenderer.invoke(CH.setAutoConnect, enabled),
  getAutoConnect: () => ipcRenderer.invoke(CH.getAutoConnect),
  getLastServer: () => ipcRenderer.invoke(CH.getLastServer),
  clearLastServer: () => ipcRenderer.invoke(CH.clearLastServer),
  getLocale: () => ipcRenderer.invoke(CH.getLocale),
  setLocale: (locale: string) => ipcRenderer.invoke(CH.setLocale, locale),
  getIsPackaged: () => ipcRenderer.invoke(CH.getIsPackaged),
  getCachedServers: () => ipcRenderer.invoke(CH.getCachedServers),
  cacheServers: (servers: unknown[]) => ipcRenderer.invoke(CH.cacheServers, servers),
  listDevices: () => ipcRenderer.invoke(CH.listDevices),
  removeDevice: (deviceId: string) => ipcRenderer.invoke(CH.removeDevice, deviceId),
  getSubscription: () => ipcRenderer.invoke(CH.getSubscription),
  rememberAccountNumber: (accountNumber: string) =>
    ipcRenderer.invoke(CH.rememberAccountNumber, accountNumber),
  getRememberedAccountNumber: () => ipcRenderer.invoke(CH.getRememberedAccountNumber),
  clearRememberedAccountNumber: () => ipcRenderer.invoke(CH.clearRememberedAccountNumber),
};

const autoUpdaterApi = {
  checkForUpdates: () => ipcRenderer.invoke(CH.checkForUpdates),
  downloadUpdate: () => ipcRenderer.invoke(CH.downloadAppUpdate),
  installUpdate: () => ipcRenderer.invoke(CH.installUpdate),
  onUpdateAvailable: (callback: (info: { version: string; releaseNotes?: string; macOnly?: boolean }) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, info: { version: string; releaseNotes?: string; macOnly?: boolean }) =>
      callback(info);
    ipcRenderer.on(CH.updateAvailable, listener);
    return () => ipcRenderer.removeListener(CH.updateAvailable, listener);
  },
  onUpdateNotAvailable: (callback: () => void) => {
    const listener = () => callback();
    ipcRenderer.on(CH.updateNotAvailable, listener);
    return () => ipcRenderer.removeListener(CH.updateNotAvailable, listener);
  },
  onUpdateError: (callback: (message: string) => void) => {
    const listener = (_event: Electron.IpcRendererEvent, message: string) => callback(message);
    ipcRenderer.on(CH.updateError, listener);
    return () => ipcRenderer.removeListener(CH.updateError, listener);
  },
};

contextBridge.exposeInMainWorld("daemonApi", daemonApi);
contextBridge.exposeInMainWorld("pangeaApi", pangeaApi);
contextBridge.exposeInMainWorld("autoUpdater", autoUpdaterApi);
contextBridge.exposeInMainWorld("appPlatform", process.platform);
contextBridge.exposeInMainWorld("openExternal", (url: string) => ipcRenderer.invoke(CH.openExternal, url));
contextBridge.exposeInMainWorld("onAuthInvalidated", (callback: () => void) => {
  const listener = () => callback();
  ipcRenderer.on(CH.authInvalidated, listener);
  return () => ipcRenderer.removeListener(CH.authInvalidated, listener);
});
