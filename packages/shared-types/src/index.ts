import { z } from "zod";

export const DaemonStateSchema = z.enum([
  "DISCONNECTED",
  "CONNECTING",
  "CONNECTED",
  "DISCONNECTING",
  "ERROR"
]);

export const LogLevelSchema = z.enum(["info", "warn", "error", "debug"]);
export const LogSourceSchema = z.enum([
  "daemon",
  "cloak",
  "naive",
  "shadowsocks",
  "snowflake",
  "wireguard"
]);

export const CloakProfileSchema = z.object({
  localPort: z.number().int().positive(),
  remoteHost: z.string().min(1),
  remotePort: z.number().int().positive(),
  uid: z.string().min(1),
  publicKey: z.string().min(1),
  encryptionMethod: z.string().min(1),
  password: z.string(),
  // Optional cover SNI advertised by the hub (daemon defaults to www.microsoft.com when absent).
  serverName: z.string().optional()
});

export const NaiveProfileSchema = z.object({
  localPort: z.number().int().nonnegative(),
  remoteHost: z.string().min(1),
  remotePort: z.number().int().positive(),
  username: z.string().min(1),
  password: z.string(),
  serverName: z.string().optional()
});

export const RealityProfileSchema = z.object({
  localPort: z.number().int().nonnegative(),
  remoteHost: z.string().min(1),
  remotePort: z.number().int().positive(),
  uuid: z.string().min(1),
  publicKey: z.string().min(1),
  shortId: z.string().min(1),
  flow: z.string().optional(),
  serverName: z.string().optional(),
  targetPort: z.number().int().positive().optional()
});

export const Hysteria2ProfileSchema = z.object({
  localPort: z.number().int().nonnegative(),
  remoteHost: z.string().min(1),
  remotePort: z.number().int().positive(),
  serverName: z.string().optional(),
  password: z.string(),
  obfsPassword: z.string(),
  upMbps: z.number().int().nonnegative().optional(),
  downMbps: z.number().int().nonnegative().optional(),
  insecure: z.boolean().optional(),
  pinSha256: z.string().optional()
});

export const ShadowsocksProfileSchema = z.object({
  localPort: z.number().int().nonnegative(),
  remoteHost: z.string().min(1),
  remotePort: z.number().int().positive(),
  method: z.string().min(1),
  password: z.string().min(1),
  // Where the SS server relays decoded packets (the node's WireGuard
  // listener); hub-configurable because the node's ACL scopes it.
  targetHost: z.string().optional(),
  targetPort: z.number().int().positive().optional(),
  udpOverTcp: z.boolean().optional()
});

export const SnowflakeProfileSchema = z.object({
  localPort: z.number().int().nonnegative(),
  brokerURL: z.string().min(1),
  fronts: z.array(z.string()).optional(),
  ampCacheUrl: z.string().optional(),
  iceServers: z.array(z.string()).optional(),
  bridgeFingerprint: z.string().min(1),
  keepLocalAddresses: z.boolean().optional()
});

export const WireGuardProfileSchema = z.object({
  configText: z.string(),
  tunnelName: z.string().min(1),
  dns: z.array(z.string()),
  bypassHosts: z.array(z.string()).optional(),
  /**
   * The node's own WireGuard listener as host:port. `configText` always points
   * at a loopback transport bridge instead, so this is what the daemon rewrites
   * the Endpoint line to when the user picks plain WireGuard. Absent means that
   * method is unavailable for this profile.
   */
  directEndpoint: z.string().optional()
});

export const ProfileSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  cloak: CloakProfileSchema,
  /**
   * Every transport endpoint of this profile's node as a raw IP, straight from
   * the hub. The daemon permits these through the kill switch and routes them
   * outside the tunnel without a DNS lookup — which matters because an engaged
   * Lockdown lock blocks DNS, so a hostname permit can never be resolved behind
   * it and every transport but Cloak would be blocked by our own kill switch.
   */
  transportEndpointIPs: z.array(z.string()).optional(),
  naive: NaiveProfileSchema.optional(),
  reality: RealityProfileSchema.optional(),
  hysteria2: Hysteria2ProfileSchema.optional(),
  shadowsocks: ShadowsocksProfileSchema.optional(),
  snowflake: SnowflakeProfileSchema.optional(),
  wireguard: WireGuardProfileSchema
});

export const AppConfigSchema = z.object({
  profiles: z.array(ProfileSchema).default([])
});

export const StatusResponseSchema = z.object({
  state: DaemonStateSchema,
  detail: z.string(),
  activeTransport: z
    .enum(["cloak", "naive", "reality", "hysteria2", "shadowsocks", "snowflake", "wireguard", ""])
    .default(""),
  connectingTransport: z
    .enum(["cloak", "naive", "reality", "hysteria2", "shadowsocks", "snowflake", "wireguard", ""])
    .default(""),
  cloak: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  naive: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  reality: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  hysteria2: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  shadowsocks: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  snowflake: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  wireguard: z.object({
    running: z.boolean(),
    detail: z.string(),
    bytesIn: z.number().default(0),
    bytesOut: z.number().default(0),
    // Connection readiness gates on this; older daemons omit it.
    lastHandshakeUnix: z.number().optional()
  }),
  killSwitchActive: z.boolean().default(false),
  // An ERROR the daemon is still retrying by itself, after a session dropped
  // on its own. Older daemons omit it.
  reconnecting: z.boolean().default(false),
  // A live session whose every transport stopped getting traffic through this
  // server; the client answers by rotating servers. Older daemons omit it.
  transportsExhausted: z.boolean().default(false)
});

export const ConnectRequestSchema = z.object({
  profileId: z.string().min(1),
  // "wireguard" is the direct method: no transport, straight to the node. Only
  // ever set when the user asks for it — the daemon's auto cascade never picks it.
  preferredTransport: z
    .enum(["cloak", "naive", "reality", "hysteria2", "shadowsocks", "snowflake", "wireguard"])
    .optional(),
  allowLAN: z.boolean().optional(),
  lockdown: z.boolean().optional()
});

export const OkResponseSchema = z.object({
  ok: z.boolean()
});

export const LogEntrySchema = z.object({
  ts: z.number().int(),
  level: LogLevelSchema,
  source: LogSourceSchema,
  msg: z.string()
});

export const LogsResponseSchema = z.array(LogEntrySchema);

export const ConfigResponseSchema = z.object({
  profiles: z.array(ProfileSchema)
});

export const ConfigUpdateRequestSchema = z.object({
  profiles: z.array(ProfileSchema)
});

export type DaemonState = z.infer<typeof DaemonStateSchema>;
export type LogLevel = z.infer<typeof LogLevelSchema>;
export type LogSource = z.infer<typeof LogSourceSchema>;

export type CloakProfile = z.infer<typeof CloakProfileSchema>;
export type NaiveProfile = z.infer<typeof NaiveProfileSchema>;
export type RealityProfile = z.infer<typeof RealityProfileSchema>;
export type Hysteria2Profile = z.infer<typeof Hysteria2ProfileSchema>;
export type ShadowsocksProfile = z.infer<typeof ShadowsocksProfileSchema>;
export type SnowflakeProfile = z.infer<typeof SnowflakeProfileSchema>;
export type WireGuardProfile = z.infer<typeof WireGuardProfileSchema>;
export type Profile = z.infer<typeof ProfileSchema>;
export type AppConfig = z.infer<typeof AppConfigSchema>;

export type StatusResponse = z.infer<typeof StatusResponseSchema>;
export type ConnectRequest = z.infer<typeof ConnectRequestSchema>;
export type OkResponse = z.infer<typeof OkResponseSchema>;
export type LogEntry = z.infer<typeof LogEntrySchema>;
export type LogsResponse = z.infer<typeof LogsResponseSchema>;
export type ConfigResponse = z.infer<typeof ConfigResponseSchema>;
export type ConfigUpdateRequest = z.infer<typeof ConfigUpdateRequestSchema>;
