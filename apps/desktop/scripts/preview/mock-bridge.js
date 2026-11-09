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
    { id: "us-east-1", name: "New Jersey", region: "us-east", country: "US", load: 20, reality: true, multihop: true },
    { id: "eu-west-1", name: "London", region: "eu-west", country: "GB", load: 35, reality: true, multihop: true },
    { id: "eu-west-2", name: "Amsterdam", region: "eu-west-2", country: "NL", load: 10 },
    { id: "eu-central-2", name: "Frankfurt", region: "eu-central", country: "DE", load: 55, reality: true, multihop: true }
  ];
  if (q.get("servers") === "many") {
    servers.push(
      { id: "us-west-1", name: "Los Angeles", region: "us-west", country: "US", load: 42 },
      { id: "us-west-2", name: "Los Angeles", region: "us-west", country: "US", load: 81 },
      { id: "ca-east-1", name: "Toronto", region: "ca-east", country: "CA", load: 18 },
      { id: "fr-par-1", name: "Paris", region: "fr-par", country: "FR", load: 64 },
      { id: "es-mad-1", name: "Madrid", region: "es-mad", country: "ES", load: 27 },
      { id: "ch-zrh-1", name: "Zurich", region: "ch-zrh", country: "CH", load: 12, multihop: true },
      { id: "se-sto-1", name: "Stockholm", region: "se-sto", country: "SE", load: 33, multihop: true },
      { id: "pl-waw-1", name: "Warsaw", region: "pl-waw", country: "PL", load: 93 },
      { id: "jp-tyo-1", name: "Tokyo", region: "jp-tyo", country: "JP", load: 71 },
      { id: "jp-tyo-2", name: "Tokyo", region: "jp-tyo", country: "JP", load: 48 },
      { id: "sg-sin-1", name: "Singapore", region: "sg-sin", country: "SG", load: 39 },
      { id: "au-syd-1", name: "Sydney", region: "au-syd", country: "AU", load: 22 },
      { id: "br-sao-1", name: "S\u00e3o Paulo", region: "br-sao", country: "BR", load: 58 }
    );
  }
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
    anytls: idle,
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
  // ?daemon=down fails every status poll; ?daemon=setup also reports a Mac account outside the group.
  const daemon = q.get("daemon");
  const answers = {
    getStatus: () => (daemon ? Promise.reject(new Error("daemon unavailable")) : status()),
    needsAccountSetup: daemon === "setup",
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
