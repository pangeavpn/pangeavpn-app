import {
  splitRuleKey,
  type SplitTunnelAppEntry,
  type SplitTunnelAppKind
} from "../shared/splitTunnel.ts";

/** Where an entry's icon comes from; the electron side resolves it lazily. */
export type IconSource =
  | { kind: "exe"; path: string }
  | { kind: "image"; path: string }
  | { kind: "dirExe"; path: string }
  | { kind: "theme"; name: string };

export interface CatalogCandidate extends SplitTunnelAppEntry {
  args?: string;
  iconSource?: IconSource;
}

export function toAppEntry(candidate: CatalogCandidate): SplitTunnelAppEntry {
  const entry: SplitTunnelAppEntry = {
    id: candidate.id,
    name: candidate.name,
    rule: candidate.rule,
    exe: candidate.exe,
    kind: candidate.kind
  };
  if (candidate.missing) entry.missing = true;
  if (candidate.warning) entry.warning = candidate.warning;
  if (candidate.unsupported) entry.unsupported = candidate.unsupported;
  return entry;
}

export function ruleKind(rule: string, platform: string): SplitTunnelAppKind {
  if (platform === "win32") return /[\\/]$/.test(rule) ? "dir" : "file";
  if (platform === "darwin" && /\.app\/?$/i.test(rule)) return "bundle";
  return rule.endsWith("/") ? "dir" : "file";
}

/** Same boundaries as the daemon: a dir rule ends in a separator, so
 *  …\Discord\ never matches …\DiscordPTB\. */
export function ruleMatchesPath(rule: string, path: string, platform: string): boolean {
  const r = splitRuleKey(rule, platform);
  const p = splitRuleKey(path, platform);
  switch (ruleKind(rule, platform)) {
    case "dir":
      return p.startsWith(r);
    case "bundle":
      return p === r || p.startsWith(`${r}/`);
    default:
      return p === r;
  }
}

function lastSegment(path: string, platform: string): string {
  const trimmed = path.replace(platform === "win32" ? /[\\/]+$/ : /\/+$/, "");
  const parts = trimmed.split(platform === "win32" ? /[\\/]/ : /\//);
  return parts[parts.length - 1] ?? "";
}

export function displayNameForRule(rule: string, platform: string): string {
  const base = lastSegment(rule, platform);
  if (platform === "win32") return base.replace(/\.exe$/i, "") || rule;
  if (platform === "darwin") return base.replace(/\.app$/i, "") || rule;
  return base || rule;
}

const INHERITS = new Set([
  "cmd",
  "powershell",
  "pwsh",
  "windowsterminal",
  "wt",
  "bash",
  "git-bash",
  "mintty",
  "conhost",
  "python",
  "pythonw",
  "py",
  "pyw",
  "node",
  "java",
  "javaw",
  "steam",
  "epicgameslauncher",
  "epic games launcher",
  "ubisoftconnect",
  "upc",
  "battle.net",
  "battle.net launcher",
  "eadesktop",
  "ea",
  "galaxyclient",
  "gog galaxy",
  "terminal",
  "iterm",
  "iterm2",
  "konsole",
  "xterm",
  "alacritty",
  "kitty",
  "wezterm",
  "tilix",
  "terminator",
  "lutris",
  "heroic"
]);
const MAY_NOT_WORK = new Set(["safari", "safari technology preview", "app store", "gnome-terminal"]);

/** Launchers, shells and terminals pass the exclusion to everything they start;
 *  a few apps hand their networking to processes we can't attribute. */
export function warningFor(path: string, platform: string): "inherits" | "mayNotWork" | undefined {
  const name = lastSegment(path, platform)
    .replace(/\.(exe|app)$/i, "")
    .toLowerCase();
  if (MAY_NOT_WORK.has(name)) return "mayNotWork";
  if (INHERITS.has(name)) return "inherits";
  return undefined;
}

/** One row per rule. Where shortcuts differ only by arguments, the one with
 *  none wins. Unsupported rows have no rule and are kept as they are. */
export function dedupeCandidates(candidates: readonly CatalogCandidate[], platform: string): CatalogCandidate[] {
  const byRule = new Map<string, CatalogCandidate>();
  const ruleless: CatalogCandidate[] = [];
  for (const candidate of candidates) {
    if (!candidate.rule) {
      ruleless.push(candidate);
      continue;
    }
    const key = splitRuleKey(candidate.rule, platform);
    const previous = byRule.get(key);
    if (!previous || ((previous.args ?? "").trim() !== "" && (candidate.args ?? "").trim() === "")) {
      byRule.set(key, candidate);
    }
  }
  const seenIds = new Set<string>();
  return [...byRule.values(), ...ruleless].filter((candidate) => {
    if (seenIds.has(candidate.id)) return false;
    seenIds.add(candidate.id);
    return true;
  });
}

/** Drops entries whose path a folder rule (a Steam or Epic game) already covers. */
export function dropCoveredByDirs(
  candidates: readonly CatalogCandidate[],
  dirRules: readonly string[],
  platform: string
): CatalogCandidate[] {
  return candidates.filter((candidate) => {
    const path = candidate.exe || candidate.rule;
    return !dirRules.some((dir) => ruleMatchesPath(dir, path, platform) && splitRuleKey(dir, platform) !== splitRuleKey(candidate.rule, platform));
  });
}

export interface WinEnv {
  systemRoot: string;
  programFiles: string;
  programFilesX86: string;
  programData: string;
  usersRoot: string;
  userProfile: string;
  appData: string;
  localAppData: string;
  temp: string;
  commonProgramFiles: string;
  commonProgramFilesX86: string;
}

export function winNormalize(path: string): string {
  return path.trim().replace(/\//g, "\\").replace(/(?<!^)\\{2,}/g, "\\");
}

function winKey(path: string): string {
  return winNormalize(path).replace(/\\+$/, "").toLowerCase();
}

export function winDirname(path: string): string {
  const normalized = winNormalize(path).replace(/\\+$/, "");
  const at = normalized.lastIndexOf("\\");
  return at <= 0 ? normalized : normalized.slice(0, at);
}

export function winBasename(path: string): string {
  return lastSegment(winNormalize(path), "win32");
}

export function winEnvFrom(env: Record<string, string | undefined>): WinEnv {
  const drive = env.SystemDrive || "C:";
  const userProfile = env.USERPROFILE || "";
  return {
    systemRoot: env.SystemRoot || env.windir || `${drive}\\Windows`,
    programFiles: env.ProgramFiles || `${drive}\\Program Files`,
    programFilesX86: env["ProgramFiles(x86)"] || `${drive}\\Program Files (x86)`,
    programData: env.ProgramData || `${drive}\\ProgramData`,
    usersRoot: userProfile ? winDirname(userProfile) : `${drive}\\Users`,
    userProfile,
    appData: env.APPDATA || "",
    localAppData: env.LOCALAPPDATA || "",
    temp: env.TEMP || env.TMP || "",
    commonProgramFiles: env.CommonProgramFiles || "",
    commonProgramFilesX86: env["CommonProgramFiles(x86)"] || ""
  };
}

function isInsideOrEqual(path: string, root: string): boolean {
  return path === root || path.startsWith(`${root}\\`);
}

/** Folders a rule must never be (or contain): drive roots, system and shared
 *  install roots, profile and AppData roots, and anything under %SystemRoot%. */
export function isWinProtectedDir(dir: string, env: WinEnv): boolean {
  const d = winKey(dir);
  if (!/^[a-z]:(\\|$)/.test(d) || /^[a-z]:$/.test(d)) return true;
  const roots = [
    env.systemRoot,
    env.programFiles,
    env.programFilesX86,
    env.programData,
    env.usersRoot,
    env.userProfile,
    env.appData,
    env.localAppData,
    env.localAppData && `${env.localAppData}\\Programs`,
    env.programFiles && `${env.programFiles}\\WindowsApps`,
    env.userProfile && `${env.userProfile}\\AppData`
  ]
    .filter((root): root is string => Boolean(root))
    .map(winKey);
  if (roots.some((root) => isInsideOrEqual(root, d))) return true;
  if (env.systemRoot && isInsideOrEqual(d, winKey(env.systemRoot))) return true;
  if (/^[a-z]:\\users(\\[^\\]+)?$/.test(d)) return true;
  return /^[a-z]:\\users\\[^\\]+\\appdata(\\(local|roaming|locallow)(\\programs)?)?$/.test(d);
}

const SHARED_IN_PROFILE =
  /^(desktop|downloads|documents|music|pictures|videos|onedrive[^\\]*(\\[^\\]+)?|appdata\\(local|roaming)\\(temp|microsoft))$/;

/** Folders unrelated programs share (Downloads, Desktop, Temp, Common Files): fine to hold an
 *  app, but a folder rule for one would let everything else in it bypass the VPN too. */
export function isWinSharedDir(dir: string, env: WinEnv): boolean {
  const d = winKey(dir);
  const profiles = [/^[a-z]:\\users\\[^\\]+\\/.exec(d)?.[0], env.userProfile && `${winKey(env.userProfile)}\\`];
  if (profiles.some((profile) => profile && d.startsWith(profile) && SHARED_IN_PROFILE.test(d.slice(profile.length)))) {
    return true;
  }
  if (/^[a-z]:\\users\\public(\\[^\\]+)?$/.test(d) || /^[a-z]:\\program files( \(x86\))?\\common files$/.test(d)) return true;
  return [env.temp, env.commonProgramFiles, env.commonProgramFilesX86].some((root) => root !== "" && winKey(root) === d);
}

function refusesFolderRule(dir: string, env: WinEnv): boolean {
  return isWinProtectedDir(dir, env) || isWinSharedDir(dir, env);
}

/** Splits a shortcut's argument string like CommandLineToArgvW, minus its backslash rules. */
export function splitWinArgs(args: string): string[] {
  const out: string[] = [];
  let current = "";
  let inToken = false;
  let quoted = false;
  for (const char of args) {
    if (char === '"') {
      quoted = !quoted;
      inToken = true;
    } else if (!quoted && /\s/.test(char)) {
      if (inToken) out.push(current);
      current = "";
      inToken = false;
    } else {
      current += char;
      inToken = true;
    }
  }
  if (inToken) out.push(current);
  return out;
}

/** The app a Squirrel `Update.exe --processStart X.exe` shortcut launches. */
export function squirrelProcessStart(args: string): string | null {
  const argv = splitWinArgs(args);
  for (let i = 0; i < argv.length; i++) {
    const match = /^--processStart(?:AndWait)?(?:=(.*))?$/i.exec(argv[i]);
    if (!match) continue;
    const value = match[1] ?? argv[i + 1];
    return value && /^[^\\/:*?"<>|]+\.exe$/i.test(value) ? value : null;
  }
  return null;
}

const SQUIRREL_APP_DIR = /^app-(\d+(?:\.\d+)+)$/i;
const VERSION_HASH_DIR = /^version-[0-9a-f]{16}$/i;
const DOTTED_VERSION_DIR = /^\d+(?:\.\d+){1,3}$/;

/** `app-<version>` folders, newest first. */
export function squirrelAppDirs(names: readonly string[]): string[] {
  const version = (name: string) => (SQUIRREL_APP_DIR.exec(name)?.[1] ?? "").split(".").map(Number);
  return names
    .filter((name) => SQUIRREL_APP_DIR.test(name))
    .sort((a, b) => {
      const va = version(a);
      const vb = version(b);
      for (let i = 0; i < Math.max(va.length, vb.length); i++) {
        const diff = (vb[i] ?? 0) - (va[i] ?? 0);
        if (diff !== 0) return diff;
      }
      return 0;
    });
}

export interface WinProbe {
  exists(path: string): Promise<boolean>;
  list(dir: string): Promise<string[]>;
}

export interface DerivedRule {
  rule: string;
  kind: "file" | "dir";
  exe: string;
}

async function newestSquirrelExe(root: string, app: string, probe: WinProbe): Promise<string | null> {
  for (const dir of squirrelAppDirs(await probe.list(root))) {
    const candidate = `${root}\\${dir}\\${app}`;
    if (await probe.exists(candidate)) return candidate;
  }
  return null;
}

async function isSquirrelRoot(dir: string, probe: WinProbe): Promise<boolean> {
  if (!(await probe.exists(`${dir}\\Update.exe`))) return false;
  return (await probe.list(dir)).some((name) => SQUIRREL_APP_DIR.test(name));
}

async function generalisedVersionDir(exe: string, env: WinEnv, probe: WinProbe): Promise<string | null> {
  const parts = winDirname(exe).split("\\");
  for (let i = parts.length - 1; i >= 1; i--) {
    const segment = parts[i];
    const parent = parts.slice(0, i).join("\\");
    const versioned = SQUIRREL_APP_DIR.test(segment)
      ? await probe.exists(`${parent}\\Update.exe`)
      : VERSION_HASH_DIR.test(segment) || DOTTED_VERSION_DIR.test(segment);
    if (!versioned) continue;
    return refusesFolderRule(parent, env) ? null : `${parent}\\`;
  }
  return null;
}

/** Turns a shortcut target into the rule that keeps matching across updates:
 *  Squirrel roots and versioned folders become folder rules, everything else the exe. */
export async function deriveWinRule(target: string, args: string, env: WinEnv, probe: WinProbe): Promise<DerivedRule> {
  const exe = winNormalize(target);
  const dir = winDirname(exe);
  const exact: DerivedRule = { rule: exe, kind: "file", exe };

  const app = winBasename(exe).toLowerCase() === "update.exe" ? squirrelProcessStart(args) : null;
  if (app) {
    const inner = await newestSquirrelExe(dir, app, probe);
    if (!refusesFolderRule(dir, env)) return { rule: `${dir}\\`, kind: "dir", exe: inner ?? exe };
    return inner ? { rule: inner, kind: "file", exe: inner } : exact;
  }
  if (!refusesFolderRule(dir, env) && (await isSquirrelRoot(dir, probe))) {
    return { rule: `${dir}\\`, kind: "dir", exe };
  }
  const generalised = await generalisedVersionDir(exe, env, probe);
  return generalised ? { rule: generalised, kind: "dir", exe } : exact;
}

const INSTALLER_EXE = /^(unins\d*|uninst(all)?.*|.*uninstall.*|installer|setup)\.exe$/i;
const WINDIR_ALLOWED = new Set(["mstsc.exe"]);
const GENERIC_HOSTS = new Set([
  "cmd.exe",
  "powershell.exe",
  "pwsh.exe",
  "wscript.exe",
  "cscript.exe",
  "mshta.exe",
  "rundll32.exe",
  "mmc.exe",
  "control.exe",
  "explorer.exe",
  "conhost.exe",
  "msiexec.exe",
  "python.exe",
  "pythonw.exe",
  "py.exe",
  "pyw.exe",
  "node.exe",
  "java.exe",
  "javaw.exe"
]);

export interface WinShortcut {
  name: string;
  target: string;
  args: string;
}

/** Why a Start-menu shortcut is not offered, or null to keep it. */
export function winShortcutSkipReason(
  shortcut: WinShortcut,
  env: WinEnv,
  selfPaths: readonly string[]
): string | null {
  const target = winNormalize(shortcut.target);
  if (!target) return "noTarget";
  if (!/^[a-z]:\\/i.test(target)) return "notLocal";
  const base = winBasename(target).toLowerCase();
  if (!base.endsWith(".exe")) return "notExe";
  if (base === "pangeavpn.exe" || selfPaths.some((self) => winKey(self) === winKey(target))) return "self";
  if (base.endsWith("_proxy.exe")) return "proxy";
  if (base === "msiexec.exe" || INSTALLER_EXE.test(base) || base.includes("install")) return "installer";
  if (/install/i.test(shortcut.name) || /\/(x|uninstall)\b/i.test(shortcut.args)) return "installer";
  if (env.systemRoot && isInsideOrEqual(winKey(target), winKey(env.systemRoot)) && !WINDIR_ALLOWED.has(base)) {
    return "system";
  }
  if (shortcut.args.trim() !== "" && GENERIC_HOSTS.has(base)) return "hostWithArgs";
  return null;
}

export function winCandidate(name: string, derived: DerivedRule, args: string): CatalogCandidate {
  const candidate: CatalogCandidate = {
    id: `win:${splitRuleKey(derived.rule, "win32")}`,
    name,
    rule: derived.rule,
    exe: derived.exe,
    kind: derived.kind,
    args,
    iconSource: { kind: "exe", path: derived.exe }
  };
  const warning = derived.kind === "file" ? warningFor(derived.exe, "win32") : undefined;
  if (warning) candidate.warning = warning;
  return candidate;
}

export type VdfValue = string | VdfObject;
export interface VdfObject {
  [key: string]: VdfValue;
}

/** Valve KeyValues text (libraryfolders.vdf, appmanifest_*.acf). Keys are
 *  lower-cased: Steam treats them case-insensitively. */
export function parseVdf(text: string): VdfObject {
  let i = 0;
  const n = text.length;

  const skip = (): void => {
    while (i < n) {
      const char = text[i];
      if (char === "/" && text[i + 1] === "/") {
        while (i < n && text[i] !== "\n") i++;
      } else if (/[\s\uFEFF]/.test(char)) {
        i++;
      } else {
        return;
      }
    }
  };

  const readString = (): string | null => {
    if (text[i] === '"') {
      i++;
      let out = "";
      while (i < n && text[i] !== '"') {
        if (text[i] === "\\" && i + 1 < n) {
          const next = text[i + 1];
          out += next === "n" ? "\n" : next === "t" ? "\t" : next;
          i += 2;
        } else {
          out += text[i++];
        }
      }
      i++;
      return out;
    }
    const start = i;
    while (i < n && !/[\s{}"]/.test(text[i])) i++;
    return i > start ? text.slice(start, i) : null;
  };

  const parseObject = (depth: number): VdfObject => {
    const object: VdfObject = Object.create(null) as VdfObject;
    for (;;) {
      skip();
      if (i >= n) return object;
      if (text[i] === "}") {
        i++;
        return object;
      }
      if (text[i] === "{") {
        i++;
        if (depth < 32) parseObject(depth + 1);
        continue;
      }
      const key = readString();
      if (key === null) {
        i++;
        continue;
      }
      skip();
      if (text[i] === "{") {
        i++;
        object[key.toLowerCase()] = depth < 32 ? parseObject(depth + 1) : (Object.create(null) as VdfObject);
      } else {
        const value = readString();
        if (value === null) continue;
        object[key.toLowerCase()] = value;
      }
      skip();
      if (text[i] === "[") {
        while (i < n && text[i] !== "]") i++;
        i++;
      }
    }
  };

  return parseObject(0);
}

/** Every Steam library folder: the root itself, then each `path` (new format)
 *  or numbered value (pre-2021 format) in libraryfolders.vdf. */
export function steamLibraryPaths(libraryFoldersVdf: string, steamRoot: string): string[] {
  const paths = [winNormalize(steamRoot)];
  const folders = parseVdf(libraryFoldersVdf).libraryfolders;
  if (folders && typeof folders === "object") {
    for (const [key, value] of Object.entries(folders)) {
      if (!/^\d+$/.test(key)) continue;
      const path = typeof value === "string" ? value : typeof value.path === "string" ? value.path : null;
      if (path) paths.push(winNormalize(path));
    }
  }
  const seen = new Set<string>();
  return paths.filter((path) => {
    const key = winKey(path);
    if (!path || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
}

export interface SteamManifest {
  appId: string;
  name: string;
  installDir: string;
  stateFlags: number;
}

export function parseAppManifest(text: string): SteamManifest | null {
  const state = parseVdf(text).appstate;
  if (!state || typeof state !== "object") return null;
  const field = (key: string): string => (typeof state[key] === "string" ? (state[key] as string) : "");
  const appId = field("appid");
  if (!/^\d+$/.test(appId)) return null;
  return {
    appId,
    name: field("name"),
    installDir: field("installdir"),
    stateFlags: Number.parseInt(field("stateflags"), 10) || 0
  };
}

const STEAM_REDIST_APP_ID = "228980";

/** A fully installed game as a folder rule, or null for redistributables,
 *  partial installs and odd install dirs. */
export function steamGameCandidate(
  library: string,
  manifest: SteamManifest,
  iconFile?: string
): CatalogCandidate | null {
  if ((manifest.stateFlags & 4) === 0 || manifest.appId === STEAM_REDIST_APP_ID) return null;
  const installDir = manifest.installDir.trim();
  if (!installDir || /[\\/:]/.test(installDir) || installDir === "." || installDir === "..") return null;
  const dir = `${winNormalize(library).replace(/\\+$/, "")}\\steamapps\\common\\${installDir}`;
  return {
    id: `steam:${manifest.appId}`,
    name: manifest.name.trim() || installDir,
    rule: `${dir}\\`,
    exe: "",
    kind: "dir",
    iconSource: iconFile ? { kind: "image", path: iconFile } : { kind: "dirExe", path: dir }
  };
}

/** The [InternetShortcut] fields of a .url file. */
export function parseInternetShortcut(text: string): { url: string; iconFile: string } {
  let inSection = false;
  let url = "";
  let iconFile = "";
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (line.startsWith("[")) {
      inSection = line.toLowerCase() === "[internetshortcut]";
      continue;
    }
    if (!inSection) continue;
    const eq = line.indexOf("=");
    if (eq <= 0) continue;
    const key = line.slice(0, eq).trim().toLowerCase();
    const value = line.slice(eq + 1).trim();
    if (key === "url") url = value;
    if (key === "iconfile") iconFile = value;
  }
  return { url, iconFile };
}

export function steamAppIdFromUrl(url: string): string | null {
  return /^steam:\/\/rungameid\/(\d+)\b/i.exec(url.trim())?.[1] ?? null;
}

/** Steam keeps game icons in <root>\steam\games, so a shortcut's icon locates Steam. */
export function steamRootFromIconFile(iconFile: string): string | null {
  return /^([a-z]:\\.*?)\\steam\\games\\[^\\]+$/i.exec(winNormalize(iconFile))?.[1] ?? null;
}

/** `reg query HKCU\Software\Valve\Steam /v SteamPath` output. */
export function parseRegSteamPath(stdout: string): string | null {
  const match = /^\s*SteamPath\s+REG_(?:EXPAND_)?SZ\s+(.+?)\s*$/im.exec(stdout);
  return match ? winNormalize(match[1]) : null;
}

/** An Epic Games Launcher .item manifest as a folder rule. */
export function epicGameCandidate(text: string, env: WinEnv): CatalogCandidate | null {
  let manifest: Record<string, unknown>;
  try {
    manifest = JSON.parse(text) as Record<string, unknown>;
  } catch {
    return null;
  }
  if (typeof manifest !== "object" || manifest === null || manifest.bIsIncompleteInstall === true) return null;
  const location = typeof manifest.InstallLocation === "string" ? winNormalize(manifest.InstallLocation).replace(/\\+$/, "") : "";
  const name = typeof manifest.DisplayName === "string" ? manifest.DisplayName.trim() : "";
  const appName = typeof manifest.AppName === "string" ? manifest.AppName : name;
  if (!location || !name || !/^[a-z]:\\/i.test(location) || isWinProtectedDir(location, env)) return null;
  const launch = typeof manifest.LaunchExecutable === "string" ? winNormalize(manifest.LaunchExecutable) : "";
  return {
    id: `epic:${appName}`,
    name,
    rule: `${location}\\`,
    exe: "",
    kind: "dir",
    iconSource: launch && !launch.includes("..") ? { kind: "exe", path: `${location}\\${launch}` } : { kind: "dirExe", path: location }
  };
}

export function macAppDirs(home: string): string[] {
  const dirs = ["/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities"];
  if (home) dirs.push(`${home.replace(/\/+$/, "")}/Applications`);
  return dirs;
}

export function macBundleCandidate(bundlePath: string, ownRules: readonly string[]): CatalogCandidate | null {
  const rule = bundlePath.replace(/\/+$/, "");
  if (!/\.app$/i.test(rule) || /(^|\/)pangeavpn\.app$/i.test(rule)) return null;
  if (ownRules.some((own) => splitRuleKey(own, "darwin") === splitRuleKey(rule, "darwin"))) return null;
  const candidate: CatalogCandidate = {
    id: `mac:${splitRuleKey(rule, "darwin")}`,
    name: displayNameForRule(rule, "darwin"),
    rule,
    exe: rule,
    kind: "bundle",
    iconSource: { kind: "exe", path: rule }
  };
  const warning = warningFor(rule, "darwin");
  if (warning) candidate.warning = warning;
  return candidate;
}

/** The outermost bundle a picked path sits in, so a helper app nested inside never becomes the rule. */
export function macBundleRoot(path: string): string | null {
  return /^(\/.*?[^/]\.app)(?:\/|$)/i.exec(path)?.[1] ?? null;
}

function unescapeDesktopValue(value: string): string {
  return value.replace(/\\([sntr\\])/g, (_match, code: string) =>
    code === "s" ? " " : code === "n" ? "\n" : code === "t" ? "\t" : code === "r" ? "\r" : "\\"
  );
}

/** The [Desktop Entry] group of a .desktop file, string escapes undone; null if absent. */
export function parseDesktopEntry(text: string): Record<string, string> | null {
  const keys: Record<string, string> = Object.create(null) as Record<string, string>;
  let inMain = false;
  let seenMain = false;
  for (const raw of text.split(/\r?\n/)) {
    const line = raw.trim();
    if (!line || line.startsWith("#")) continue;
    if (line.startsWith("[")) {
      inMain = line === "[Desktop Entry]";
      seenMain ||= inMain;
      continue;
    }
    if (!inMain) continue;
    const eq = line.indexOf("=");
    if (eq <= 0) continue;
    const key = line.slice(0, eq).trim();
    if (!(key in keys)) keys[key] = unescapeDesktopValue(line.slice(eq + 1).trim());
  }
  return seenMain ? keys : null;
}

export function desktopFileId(baseDir: string, file: string): string {
  const base = baseDir.replace(/\/+$/, "");
  return file.slice(base.length + 1).replace(/\//g, "-");
}

function isTrue(value: string | undefined): boolean {
  return (value ?? "").trim().toLowerCase() === "true";
}

function desktopList(value: string | undefined): string[] {
  return (value ?? "")
    .split(";")
    .map((item) => item.trim().toLowerCase())
    .filter(Boolean);
}

export function isDesktopEntryShown(keys: Record<string, string>, currentDesktops: readonly string[]): boolean {
  if (keys.Type !== "Application" || !keys.Name || !keys.Exec) return false;
  if (isTrue(keys.NoDisplay) || isTrue(keys.Hidden) || isTrue(keys.Terminal)) return false;
  const current = currentDesktops.map((desktop) => desktop.toLowerCase());
  const only = desktopList(keys.OnlyShowIn);
  if (only.length > 0 && !only.some((desktop) => current.includes(desktop))) return false;
  return !desktopList(keys.NotShowIn).some((desktop) => current.includes(desktop));
}

/** Desktop-file-ID semantics: the first file with an ID wins even when it hides
 *  the app, so a user's Hidden=true copy shadows the system one. */
export function selectDesktopEntries<T extends { id: string; keys: Record<string, string> | null }>(
  found: Iterable<T>,
  currentDesktops: readonly string[]
): T[] {
  const seen = new Set<string>();
  const shown: T[] = [];
  for (const item of found) {
    if (seen.has(item.id)) continue;
    seen.add(item.id);
    if (item.keys && isDesktopEntryShown(item.keys, currentDesktops)) shown.push(item);
  }
  return shown;
}

const FIELD_CODE = /^%[fFuUickdDnNvm]$/;

/** Exec= per the Desktop Entry spec: double quotes with \" \` \$ \\ escapes,
 *  field codes dropped, %% kept as %. Null on an unbalanced quote. */
export function tokenizeExec(exec: string): string[] | null {
  const out: string[] = [];
  let current = "";
  let inToken = false;
  let quoted = false;
  for (let i = 0; i < exec.length; i++) {
    const char = exec[i];
    if (quoted) {
      if (char === "\\" && i + 1 < exec.length && /["`$\\]/.test(exec[i + 1])) {
        current += exec[++i];
      } else if (char === '"') {
        quoted = false;
      } else {
        current += char;
      }
      continue;
    }
    if (char === '"') {
      quoted = true;
      inToken = true;
    } else if (char === " " || char === "\t" || char === "\n") {
      if (inToken) out.push(current);
      current = "";
      inToken = false;
    } else if (char === "\\" && i + 1 < exec.length) {
      current += exec[++i];
      inToken = true;
    } else {
      current += char;
      inToken = true;
    }
  }
  if (quoted) return null;
  if (inToken) out.push(current);
  return out
    .filter((token) => !FIELD_CODE.test(token))
    .map((token) => token.replace(/%(.)/g, (_match, code: string) => (code === "%" ? "%" : "")));
}

function posixBasename(path: string): string {
  return lastSegment(path, "linux");
}

function posixDirname(path: string): string {
  const trimmed = path.replace(/\/+$/, "");
  const at = trimmed.lastIndexOf("/");
  return at <= 0 ? "/" : trimmed.slice(0, at);
}

const ENV_ASSIGNMENT = /^[A-Za-z_][A-Za-z0-9_]*=/;

/** Drops a leading `env` with its options and VAR=value assignments. */
export function stripEnvPrefix(argv: readonly string[]): string[] {
  if (argv.length === 0 || posixBasename(argv[0]) !== "env") return [...argv];
  let rest = argv.slice(1);
  for (;;) {
    const token = rest[0];
    if (token === undefined) return [];
    if (token === "--") {
      rest = rest.slice(1);
      break;
    }
    if (token === "-S" || token === "--split-string") {
      rest = [...(tokenizeExec(rest[1] ?? "") ?? []), ...rest.slice(2)];
      continue;
    }
    if (/^(-S|--split-string=)./.test(token)) {
      rest = [...(tokenizeExec(token.replace(/^(-S|--split-string=)/, "")) ?? []), ...rest.slice(1)];
      continue;
    }
    if (token === "-u" || token === "--unset" || token === "-C" || token === "--chdir") {
      rest = rest.slice(2);
      continue;
    }
    if (token.startsWith("-") || ENV_ASSIGNMENT.test(token)) {
      rest = rest.slice(1);
      continue;
    }
    break;
  }
  while (rest.length > 0 && ENV_ASSIGNMENT.test(rest[0])) rest = rest.slice(1);
  return rest;
}

const INTERPRETER =
  /^(sh|bash|dash|zsh|fish|ksh|mksh|csh|tcsh|env|python[\d.]*|pypy[\d.]*|perl[\d.]*|ruby[\d.]*|node|nodejs|java|mono|php[\d.]*|lua[\d.]*|gjs|electron\d*|wine\w*|flatpak|bwrap|snap|snap-confine|xdg-open|gtk-launch|gio|kioclient\d*|systemd.*|sudo|pkexec|nice|ionice|firejail|sg|setsid|nohup)$/;

export function isLinuxInterpreter(path: string): boolean {
  return INTERPRETER.test(posixBasename(path));
}

export type ExecutableHead = "elf" | "appimage" | "script" | "other";

export function classifyExecutableHead(head: Uint8Array): ExecutableHead {
  if (head.length >= 4 && head[0] === 0x7f && head[1] === 0x45 && head[2] === 0x4c && head[3] === 0x46) {
    return head.length >= 11 && head[8] === 0x41 && head[9] === 0x49 && head[10] === 0x02 ? "appimage" : "elf";
  }
  if (head.length >= 2 && head[0] === 0x23 && head[1] === 0x21) return "script";
  return "other";
}

/** A wrapper script's app-private folder (stepping out of a trailing bin), or
 *  null when that folder is shared: /usr/bin/byobu must never become /usr/. */
export function scriptRuleDir(scriptPath: string, home: string): string | null {
  let dir = posixDirname(scriptPath);
  if (posixBasename(dir) === "bin") dir = posixDirname(dir);
  const parts = dir.split("/").filter(Boolean);
  const [a, b, c] = parts;
  let keep = 0;
  if (a === "opt" && parts.length >= 2) keep = parts.length;
  else if (a === "usr" && (b === "lib" || b === "lib64" || b === "libexec" || b === "share") && parts.length >= 3) {
    keep = /-linux-gnu/.test(c ?? "") && parts.length < 4 ? 0 : parts.length;
  } else if (a === "usr" && b === "local" && (c === "lib" || c === "share") && parts.length >= 4) keep = parts.length;
  else if (a === "nix" && b === "store" && parts.length >= 3) keep = 3;
  else if (home) {
    const homeParts = home.split("/").filter(Boolean);
    const underHome = homeParts.length > 0 && homeParts.every((part, i) => parts[i] === part);
    if (underHome && parts.length - homeParts.length >= 2) keep = parts.length;
  }
  return keep > 0 ? `/${parts.slice(0, keep).join("/")}/` : null;
}

const FLATPAK_ID = /^[A-Za-z][A-Za-z0-9_-]*(\.[A-Za-z0-9_-]+){2,}$/;

function flatpakAppId(keys: Record<string, string>, argv: readonly string[]): string | null {
  const declared = keys["X-Flatpak"]?.trim();
  if (declared) return FLATPAK_ID.test(declared) ? declared : null;
  const run = argv.indexOf("run");
  if (run < 0) return null;
  const id = argv.slice(run + 1).find((token) => !token.startsWith("-"));
  return id && FLATPAK_ID.test(id) ? id : null;
}

function flatpakInstallation(desktopFile: string, argv: readonly string[], home: string): string {
  const at = desktopFile.indexOf("/exports/share/applications/");
  if (at > 0) return desktopFile.slice(0, at);
  return argv.includes("--user") && home ? `${home}/.local/share/flatpak` : "/var/lib/flatpak";
}

const SNAP_NAME = /^[a-z0-9][a-z0-9-]*(_[a-z0-9]+)?$/;

/** A picked file inside a snap or flatpak tree gets the folder rule the catalog would give it, which survives updates. */
export function linuxPackagedDir(path: string): string | null {
  const snapBin = /^\/snap\/bin\/([^/]+)$/.exec(path);
  if (snapBin) {
    const name = snapBin[1].split(".")[0];
    return SNAP_NAME.test(name) ? `/snap/${name}/` : null;
  }
  const snap = /^\/snap\/([^/]+)\/./.exec(path);
  if (snap) return snap[1] !== "bin" && SNAP_NAME.test(snap[1]) ? `/snap/${snap[1]}/` : null;
  const flatpak = /^(\/.*\/flatpak)\/app\/([^/]+)\/./.exec(path);
  return flatpak && FLATPAK_ID.test(flatpak[2]) ? `${flatpak[1]}/app/${flatpak[2]}/` : null;
}

export interface LinuxAppFacts {
  keys: Record<string, string>;
  desktopFile: string;
  home: string;
  /** Exec after tokenising and stripping env. */
  argv: readonly string[];
  /** argv[0] found on PATH, before symlinks are resolved. */
  resolved: string | null;
  real: string | null;
  head: ExecutableHead | null;
}

export type LinuxClassification =
  | { rule: string; kind: "file" | "dir"; exe: string }
  | { unsupported: "flatpak" | "appimage" | "script"; exe: string }
  | { skip: "missing" };

export function classifyLinuxApp(facts: LinuxAppFacts): LinuxClassification {
  const command = facts.argv[0] ?? "";
  const exe = facts.real ?? facts.resolved ?? command;
  if (facts.keys["X-Flatpak"] || posixBasename(command) === "flatpak") {
    const id = flatpakAppId(facts.keys, facts.argv);
    if (!id) return { unsupported: "flatpak", exe };
    return { rule: `${flatpakInstallation(facts.desktopFile, facts.argv, facts.home)}/app/${id}/`, kind: "dir", exe: "" };
  }
  const snapPath = [command, facts.resolved ?? ""].find((path) => path.startsWith("/snap/bin/"));
  if (snapPath) {
    const instance = facts.keys["X-SnapInstanceName"]?.trim() || posixBasename(snapPath).split(".")[0];
    if (SNAP_NAME.test(instance)) {
      return { rule: `/snap/${instance}/`, kind: "dir", exe: snapPath };
    }
  }
  if (!facts.resolved || !facts.real) return { skip: "missing" };
  if (/\.appimage$/i.test(facts.resolved) || /\.appimage$/i.test(facts.real) || facts.head === "appimage") {
    return { unsupported: "appimage", exe };
  }
  if (isLinuxInterpreter(command) || isLinuxInterpreter(facts.real)) return { unsupported: "script", exe };
  if (facts.head === "script") {
    const dir = scriptRuleDir(facts.real, facts.home);
    return dir ? { rule: dir, kind: "dir", exe: facts.real } : { unsupported: "script", exe };
  }
  return { rule: facts.real, kind: "file", exe: facts.real };
}

export function linuxCandidate(desktopId: string, keys: Record<string, string>, result: LinuxClassification): CatalogCandidate | null {
  if ("skip" in result) return null;
  const candidate: CatalogCandidate = {
    id: `desktop:${desktopId}`,
    name: keys.Name.trim(),
    rule: "rule" in result ? result.rule : "",
    exe: result.exe,
    kind: "rule" in result ? result.kind : "file"
  };
  if ("unsupported" in result) candidate.unsupported = result.unsupported;
  const icon = keys.Icon?.trim();
  if (icon) candidate.iconSource = { kind: "theme", name: icon };
  const warning = warningFor(result.exe || desktopId.replace(/\.desktop$/, ""), "linux");
  if (warning) candidate.warning = warning;
  return candidate;
}

function uniqueDirs(dirs: readonly string[]): string[] {
  const seen = new Set<string>();
  return dirs
    .map((dir) => dir.replace(/\/+$/, ""))
    .filter((dir) => {
      if (!dir.startsWith("/") || seen.has(dir)) return false;
      seen.add(dir);
      return true;
    });
}

function xdgDataDirs(env: Record<string, string | undefined>, home: string): { dataHome: string; dataDirs: string[] } {
  return {
    dataHome: env.XDG_DATA_HOME || `${home}/.local/share`,
    dataDirs: (env.XDG_DATA_DIRS || "/usr/local/share:/usr/share").split(":").filter(Boolean)
  };
}

/** Application dirs in priority order, plus the snap/flatpak exports a session
 *  started without their profile.d additions would miss. */
export function linuxDesktopDirs(env: Record<string, string | undefined>, home: string): string[] {
  const { dataHome, dataDirs } = xdgDataDirs(env, home);
  return uniqueDirs([
    `${dataHome}/applications`,
    ...dataDirs.map((dir) => `${dir.replace(/\/+$/, "")}/applications`),
    "/var/lib/snapd/desktop/applications",
    "/var/lib/flatpak/exports/share/applications",
    `${home}/.local/share/flatpak/exports/share/applications`
  ]);
}

const ICON_SIZES = ["scalable", "256x256", "128x128", "64x64", "48x48", "32x32"];

/** Files to try for an Icon= value, best first: hicolor by size, then pixmaps. */
export function linuxIconCandidates(icon: string, env: Record<string, string | undefined>, home: string): string[] {
  const name = icon.trim();
  if (name.startsWith("/")) return /\.(png|svg)$/i.test(name) ? [name] : [];
  if (!name || name.includes("/")) return [];
  const { dataHome, dataDirs } = xdgDataDirs(env, home);
  const files = /\.(png|svg)$/i.test(name) ? [name] : [`${name}.svg`, `${name}.png`];
  const bases = uniqueDirs([
    `${home}/.icons`,
    `${dataHome}/icons`,
    ...dataDirs.map((dir) => `${dir.replace(/\/+$/, "")}/icons`),
    "/var/lib/flatpak/exports/share/icons",
    `${home}/.local/share/flatpak/exports/share/icons`
  ]);
  const out: string[] = [];
  for (const size of ICON_SIZES) {
    for (const base of bases) {
      for (const file of files) {
        if (size === "scalable" && !file.endsWith(".svg")) continue;
        out.push(`${base}/hicolor/${size}/apps/${file}`);
      }
    }
  }
  for (const base of uniqueDirs([...dataDirs.map((dir) => `${dir.replace(/\/+$/, "")}/pixmaps`), "/usr/share/pixmaps"])) {
    for (const file of files) out.push(`${base}/${file}`);
  }
  return out;
}

/** Resolves once `pending` settles or `ms` passes, whichever is first; never rejects. */
export function settleWithin(pending: Promise<unknown>, ms: number): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(resolve, ms);
    const done = () => {
      clearTimeout(timer);
      resolve();
    };
    pending.then(done, done);
  });
}
