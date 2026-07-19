import { z } from "zod";

export const DaemonStateSchema = z.enum([
  "DISCONNECTED",
  "CONNECTING",
  "CONNECTED",
  "DISCONNECTING",
  "ERROR"
]);

export const LogLevelSchema = z.enum(["info", "warn", "error", "debug"]);
export const LogSourceSchema = z.enum(["daemon", "cloak", "wireguard"]);

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
  bypassHosts: z.array(z.string()).optional()
});

export const ProfileSchema = z.object({
  id: z.string().min(1),
  name: z.string().min(1),
  cloak: CloakProfileSchema,
  naive: NaiveProfileSchema.optional(),
  reality: RealityProfileSchema.optional(),
  hysteria2: Hysteria2ProfileSchema.optional(),
  snowflake: SnowflakeProfileSchema.optional(),
  wireguard: WireGuardProfileSchema
});

export const AppConfigSchema = z.object({
  profiles: z.array(ProfileSchema).default([])
});

export const StatusResponseSchema = z.object({
  state: DaemonStateSchema,
  detail: z.string(),
  activeTransport: z.enum(["cloak", "naive", "reality", "hysteria2", "snowflake", ""]).default(""),
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
  snowflake: z.object({
    running: z.boolean(),
    pid: z.number().nullable()
  }),
  wireguard: z.object({
    running: z.boolean(),
    detail: z.string(),
    bytesIn: z.number().default(0),
    bytesOut: z.number().default(0)
  }),
  killSwitchActive: z.boolean().default(false)
});

export const ConnectRequestSchema = z.object({
  profileId: z.string().min(1),
  preferredTransport: z.enum(["cloak", "naive", "reality", "hysteria2", "snowflake"]).optional()
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
