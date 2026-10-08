// Fakes the preload bridge (daemonApi, pangeaApi, …) for the browser preview; the query string
// picks the scenario. window.__calls logs every bridge call, window.__setSub changes the plan live.
(() => {
  const q = new URLSearchParams(location.search);
  const state = q.get("state") || "DISCONNECTED";
  const latencyMs = Number(q.get("latency") || 0);
  const live = state === "CONNECTED";
  let sub = q.get("sub") || "active";
  let authenticated = q.get("auth") !== "0";

  const calls = (window.__calls = []);
  window.__setSub = (next) => {
    sub = next;
  };

  const servers = [
    { id: "us-east-1", name: "New Jersey", region: "us-east", country: "US", load: 20, reality: true },
    { id: "eu-west-1", name: "London", region: "eu-west", country: "GB", load: 35, reality: true },
    { id: "eu-west-2", name: "Amsterdam", region: "eu-west-2", country: "NL", load: 10 },
    { id: "eu-central-2", name: "Frankfurt", region: "eu-central", country: "DE", load: 55, reality: true }
  ];
  const plans = {
    active: { status: "active", entitled: true, renews: true, expiresAt: "2026-11-06T12:00:00Z" },
    expired: { status: "active", entitled: false, renews: false, expiresAt: "2026-10-06T12:00:00Z" },
    none: null
  };
  const idle = { running: false, pid: null };
  const running = { running: live, pid: live ? 1 : null };

  const status = () => ({
    state,
    detail: "",
    activeTransport: live ? "reality" : "",
    connectingTransport: state === "CONNECTING" ? "reality" : "",
    cloak: idle,
    naive: idle,
    reality: running,
    hysteria2: idle,
    shadowsocks: idle,
    snowflake: idle,
    wireguard: {
      running: live,
      detail: "",
      bytesIn: live ? 123456789 : 0,
      bytesOut: live ? 23456789 : 0,
      lastHandshakeUnix: Math.floor(Date.now() / 1000),
      postQuantum: true
    },
    killSwitchActive: live || q.get("ks") === "1",
    reconnecting: false,
    transportsExhausted: false,
    offline: false,
    serverId: live ? "us-east-1" : undefined
  });

  const hubMethods = { directIp: true, reality: true, shadowsocks: true, fronted: true, normal: true };
  const answers = {
    getStatus: status,
    getAppVersion: "0.0.0-preview",
    getConfig: { profiles: [] },
    getLogs: [],
    login: () => {
      authenticated = true;
      return { authenticated: true, user: { email: "", name: "" } };
    },
    logout: () => {
      authenticated = false;
    },
    getAuthState: () => ({ authenticated, user: authenticated ? { email: "", name: "" } : null }),
    getServers: servers,
    getCachedServers: servers,
    getSubscription: () => new Promise((resolve) => setTimeout(() => resolve(plans[sub] ?? null), latencyMs)),
    getHubMethods: hubMethods,
    getHubStatus: { methods: hubMethods, active: "directIp", detail: null },
    getMultihop: { enabled: false, entryServerId: null },
    getLastServer: { lastServerId: "us-east-1", lastProfileId: null, lastEntryServerId: null },
    getSplitTunnel: null,
    getLocale: () => q.get("lang") || "en",
    getWireguardMtu: 1280,
    getCustomDns: [],
    getPreferredTransport: "auto",
    getAccountNumber: "ABCD-EFGH-JKMN-PQRS",
    getRememberedAccountNumber: null,
    listDevices: [],
    getIsPackaged: false,
    getDoh: true
  };

  // Anything unlisted answers like an idle install: getters false, actions ok, on* subscriptions inert.
  const bridge = (api) =>
    new Proxy(
      {},
      {
        get(_target, method) {
          if (typeof method !== "string" || method === "then") return undefined;
          if (method.startsWith("on")) return () => () => {};
          return async (...args) => {
            calls.push({ api, method, args });
            const answer = answers[method];
            if (typeof answer === "function") return answer(...args);
            if (answer !== undefined) return structuredClone(answer);
            return method.startsWith("get") ? false : { ok: true };
          };
        }
      }
    );

  window.daemonApi = bridge("daemonApi");
  window.pangeaApi = bridge("pangeaApi");
  window.autoUpdater = bridge("autoUpdater");
  window.appPlatform = q.get("platform") || "darwin";
  window.openExternal = async (url) => {
    calls.push({ api: "window", method: "openExternal", args: [url] });
  };
  window.openLogsFolder = async () => true;
  window.sendDiagnostics = async () => ({ ok: true, reportCode: "PREV-IEW0" });
  window.onAuthInvalidated = () => () => {};
})();
