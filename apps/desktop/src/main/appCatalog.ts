import { app, nativeImage, shell, type NativeImage, type OpenDialogOptions } from "electron";
import { execFile } from "node:child_process";
import type { Dirent } from "node:fs";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { mt } from "./i18n";
import {
  classifyExecutableHead,
  classifyLinuxApp,
  dedupeCandidates,
  deriveWinRule,
  desktopFileId,
  displayNameForRule,
  dropCoveredByDirs,
  epicGameCandidate,
  linuxCandidate,
  linuxDesktopDirs,
  linuxIconCandidates,
  linuxPackagedDir,
  macAppDirs,
  macBundleCandidate,
  macBundleRoot,
  parseAppManifest,
  parseDesktopEntry,
  parseInternetShortcut,
  parseRegSteamPath,
  ruleKind,
  selectDesktopEntries,
  settleWithin,
  steamAppIdFromUrl,
  steamGameCandidate,
  steamLibraryPaths,
  steamRootFromIconFile,
  stripEnvPrefix,
  toAppEntry,
  tokenizeExec,
  warningFor,
  winCandidate,
  winEnvFrom,
  winNormalize,
  winShortcutSkipReason,
  type CatalogCandidate,
  type ExecutableHead,
  type IconSource,
  type WinEnv,
  type WinProbe
} from "./appCatalogParse";
import {
  protectRulesFor,
  splitRuleKey,
  type SplitTunnelAppEntry,
  type SplitTunnelBrowseResult,
  type SplitTunnelIcon
} from "../shared/splitTunnel";

export interface AppCatalog {
  list(refresh: boolean): Promise<SplitTunnelAppEntry[]>;
  describe(rules: readonly string[]): Promise<SplitTunnelAppEntry[]>;
  icons(keys: readonly string[]): Promise<SplitTunnelIcon[]>;
  dialogOptions(): OpenDialogOptions;
  describePick(picked: string): Promise<SplitTunnelBrowseResult>;
}

const MAX_TEXT_BYTES = 256 * 1024;
const MAX_SVG_BYTES = 256 * 1024;
const MAX_WALK_FILES = 5000;
const MAX_ICON_KEYS = 64;
const DESCRIBE_SCAN_WAIT_MS = 3000;

const yieldToLoop = (): Promise<void> => new Promise((resolve) => setImmediate(resolve));

async function exists(target: string): Promise<boolean> {
  try {
    await fs.stat(target);
    return true;
  } catch {
    return false;
  }
}

async function readText(file: string): Promise<string | null> {
  try {
    const handle = await fs.open(file, "r");
    try {
      const { size } = await handle.stat();
      if (size > MAX_TEXT_BYTES) return null;
      return await handle.readFile("utf8");
    } finally {
      await handle.close();
    }
  } catch {
    return null;
  }
}

async function readHead(file: string): Promise<ExecutableHead | null> {
  try {
    const handle = await fs.open(file, "r");
    try {
      const buffer = Buffer.alloc(16);
      const { bytesRead } = await handle.read(buffer, 0, buffer.length, 0);
      return classifyExecutableHead(buffer.subarray(0, bytesRead));
    } finally {
      await handle.close();
    }
  } catch {
    return null;
  }
}

async function listNames(dir: string): Promise<string[]> {
  try {
    return await fs.readdir(dir);
  } catch {
    return [];
  }
}

async function walk(root: string, extensions: readonly string[], depth: number, out: string[] = []): Promise<string[]> {
  let entries: Dirent[];
  try {
    entries = await fs.readdir(root, { withFileTypes: true });
  } catch {
    return out;
  }
  for (const entry of entries) {
    if (out.length >= MAX_WALK_FILES) break;
    const full = path.join(root, entry.name);
    if (entry.isDirectory()) {
      if (depth > 0) await walk(full, extensions, depth - 1, out);
    } else if (extensions.some((ext) => entry.name.toLowerCase().endsWith(ext))) {
      out.push(full);
    }
  }
  return out;
}

const winProbe: WinProbe = { exists, list: listNames };

// libuv's realpath resolves junctions, 8.3 names and subst drives; a share it maps to UNC keeps the drive form.
async function realWinPath(target: string): Promise<string | null> {
  const raw = winNormalize(target);
  try {
    const real = await fs.realpath(raw);
    return /^[a-z]:\\/i.test(real) ? real : raw;
  } catch {
    return null;
  }
}

async function realPosixPath(target: string): Promise<string | null> {
  try {
    return await fs.realpath(target);
  } catch {
    return null;
  }
}

function regSteamPath(env: WinEnv): Promise<string | null> {
  const reg = path.join(env.systemRoot, "System32", "reg.exe");
  return new Promise((resolve) => {
    execFile(
      reg,
      ["query", "HKCU\\Software\\Valve\\Steam", "/v", "SteamPath"],
      { windowsHide: true, timeout: 3000, maxBuffer: 64 * 1024 },
      (error, stdout) => resolve(error ? null : parseRegSteamPath(String(stdout)))
    );
  });
}

function emptyToBlank(image: NativeImage): string {
  return image.isEmpty() ? "" : image.toDataURL();
}

async function largestExeIn(dir: string): Promise<string | null> {
  let best: { file: string; size: number } | null = null;
  for (const name of await listNames(dir)) {
    if (!name.toLowerCase().endsWith(".exe")) continue;
    const file = path.join(dir, name);
    const stat = await fs.stat(file).catch(() => null);
    if (stat && (!best || stat.size > best.size)) best = { file, size: stat.size };
  }
  return best?.file ?? null;
}

async function linuxIcon(name: string): Promise<string> {
  for (const file of linuxIconCandidates(name, process.env, os.homedir())) {
    const stat = await fs.stat(file).catch(() => null);
    if (!stat?.isFile()) continue;
    if (file.toLowerCase().endsWith(".svg")) {
      if (stat.size > MAX_SVG_BYTES) continue;
      return `data:image/svg+xml;base64,${(await fs.readFile(file)).toString("base64")}`;
    }
    const image = nativeImage.createFromPath(file);
    if (!image.isEmpty()) return image.resize({ width: 64 }).toDataURL();
  }
  return "";
}

async function iconFor(source: IconSource, platform: string): Promise<string> {
  if (source.kind === "image") {
    const image = nativeImage.createFromPath(source.path);
    return image.isEmpty() ? "" : image.resize({ width: 32, height: 32 }).toDataURL();
  }
  if (platform === "linux") return source.kind === "theme" ? linuxIcon(source.name) : "";
  if (source.kind === "theme") return "";
  if (platform === "darwin") {
    return emptyToBlank(await nativeImage.createThumbnailFromPath(source.path, { width: 64, height: 64 }));
  }
  const target = source.kind === "dirExe" ? ((await largestExeIn(source.path)) ?? source.path) : source.path;
  return emptyToBlank(await app.getFileIcon(target, { size: "large" }));
}

export function createAppCatalog(): AppCatalog {
  const platform = process.platform;
  const iconSources = new Map<string, IconSource | null>();
  const iconCache = new Map<string, string>();
  const shortcutCache = new Map<string, { mtimeMs: number; target: string; args: string }>();
  let catalog: CatalogCandidate[] | null = null;
  let byRule = new Map<string, CatalogCandidate>();
  let scanning: Promise<CatalogCandidate[]> | null = null;

  const ownImages = (): string[] => protectRulesFor(process.execPath, platform);

  function remember(candidate: CatalogCandidate, id = candidate.id): void {
    iconSources.set(id, candidate.iconSource ?? null);
  }

  async function readShortcut(file: string): Promise<{ target: string; args: string } | null> {
    try {
      const { mtimeMs } = await fs.stat(file);
      const cached = shortcutCache.get(file);
      if (cached && cached.mtimeMs === mtimeMs) return cached;
      const link = shell.readShortcutLink(file);
      const parsed = { mtimeMs, target: link.target ?? "", args: link.args ?? "" };
      shortcutCache.set(file, parsed);
      return parsed;
    } catch {
      return null;
    }
  }

  async function steamLibraryGames(root: string, icons: Map<string, string>): Promise<CatalogCandidate[] | null> {
    const vdf =
      (await readText(`${root}\\steamapps\\libraryfolders.vdf`)) ?? (await readText(`${root}\\config\\libraryfolders.vdf`));
    if (vdf === null && !(await exists(`${root}\\steamapps`))) return null;
    const games: CatalogCandidate[] = [];
    for (const library of steamLibraryPaths(vdf ?? "", root)) {
      const steamapps = `${library}\\steamapps`;
      for (const name of await listNames(steamapps)) {
        if (!/^appmanifest_\d+\.acf$/i.test(name)) continue;
        const manifest = parseAppManifest((await readText(`${steamapps}\\${name}`)) ?? "");
        const game = manifest && steamGameCandidate(library, manifest, icons.get(manifest.appId));
        if (game && (await exists(game.rule))) games.push(game);
      }
    }
    return games;
  }

  async function steamGames(env: WinEnv, roots: string[], icons: Map<string, string>): Promise<CatalogCandidate[]> {
    const tried = new Set<string>();
    const attempt = async (root: string | null): Promise<CatalogCandidate[] | null> => {
      if (!root || tried.has(root.toLowerCase())) return null;
      tried.add(root.toLowerCase());
      return steamLibraryGames(root, icons);
    };
    for (const root of roots) {
      const games = await attempt(root);
      if (games) return games;
    }
    return (
      (await attempt(await regSteamPath(env))) ??
      (await attempt(env.programFilesX86 ? `${env.programFilesX86}\\Steam` : null)) ??
      []
    );
  }

  async function epicGames(env: WinEnv): Promise<CatalogCandidate[]> {
    if (!env.programData) return [];
    const dir = `${env.programData}\\Epic\\EpicGamesLauncher\\Data\\Manifests`;
    const games: CatalogCandidate[] = [];
    for (const name of await listNames(dir)) {
      if (!name.toLowerCase().endsWith(".item")) continue;
      const game = epicGameCandidate((await readText(`${dir}\\${name}`)) ?? "", env);
      if (game && (await exists(game.rule))) games.push(game);
    }
    return games;
  }

  async function scanWindows(): Promise<CatalogCandidate[]> {
    const env = winEnvFrom(process.env);
    const roots = [env.programData, env.appData]
      .filter(Boolean)
      .map((base) => `${base}\\Microsoft\\Windows\\Start Menu\\Programs`);
    const files = (await Promise.all(roots.map((root) => walk(root, [".lnk", ".url"], 6)))).flat();
    const self = ownImages();
    const shortcuts: CatalogCandidate[] = [];
    const steamIcons = new Map<string, string>();
    const steamRoots: string[] = [];
    let unreadable = 0;
    let skipped = 0;
    for (const [index, file] of files.entries()) {
      if (index > 0 && index % 10 === 0) await yieldToLoop();
      if (file.toLowerCase().endsWith(".url")) {
        const { url, iconFile } = parseInternetShortcut((await readText(file)) ?? "");
        const appId = steamAppIdFromUrl(url);
        if (!appId || !iconFile) continue;
        steamIcons.set(appId, winNormalize(iconFile));
        const root = steamRootFromIconFile(iconFile);
        if (root) steamRoots.push(root);
        continue;
      }
      const link = await readShortcut(file);
      if (!link) {
        unreadable++;
        continue;
      }
      const shortcut = { name: path.basename(file).replace(/\.lnk$/i, ""), target: link.target, args: link.args };
      const real = winShortcutSkipReason(shortcut, env, self) ? null : await realWinPath(link.target);
      if (!real || winShortcutSkipReason({ ...shortcut, target: real }, env, self)) {
        skipped++;
        continue;
      }
      shortcuts.push(winCandidate(shortcut.name, await deriveWinRule(real, link.args, env, winProbe), link.args));
    }
    const games = [...(await steamGames(env, steamRoots, steamIcons)), ...(await epicGames(env))];
    const covered = dropCoveredByDirs(shortcuts, games.map((game) => game.rule), "win32");
    console.log(
      `split tunnel catalog: ${files.length} shortcuts, ${unreadable} unreadable, ${skipped} skipped, ${games.length} games`
    );
    return dedupeCandidates([...games, ...covered], "win32");
  }

  async function scanMac(): Promise<CatalogCandidate[]> {
    const home = os.homedir();
    const own = ownImages();
    const ownReal = await Promise.all(own.map(async (bundle) => (await realPosixPath(bundle)) ?? bundle));
    const bundles: string[] = [];
    for (const dir of macAppDirs(home)) {
      const nested = dir === "/Applications" || dir === `${home}/Applications`;
      for (const name of await listNames(dir)) {
        const full = `${dir}/${name}`;
        if (/\.app$/i.test(name)) {
          bundles.push(full);
        } else if (nested && !name.startsWith(".") && name !== "Utilities") {
          for (const inner of await listNames(full)) {
            if (/\.app$/i.test(inner)) bundles.push(`${full}/${inner}`);
          }
        }
      }
    }
    const rows: CatalogCandidate[] = [];
    for (const [index, bundle] of bundles.entries()) {
      if (index > 0 && index % 10 === 0) await yieldToLoop();
      const candidate = macBundleCandidate((await realPosixPath(bundle)) ?? bundle, [...own, ...ownReal]);
      if (candidate) rows.push({ ...candidate, name: displayNameForRule(bundle, "darwin") });
    }
    console.log(`split tunnel catalog: ${bundles.length} bundles, ${rows.length} listed`);
    return dedupeCandidates(rows, "darwin");
  }

  async function which(command: string): Promise<string | null> {
    if (!command) return null;
    if (command.includes("/")) return command.startsWith("/") && (await exists(command)) ? command : null;
    const dirs = [...(process.env.PATH ?? "").split(":"), "/usr/local/bin", "/usr/bin", "/bin", "/snap/bin"];
    for (const dir of new Set(dirs.filter((item) => item.startsWith("/")))) {
      const candidate = `${dir.replace(/\/+$/, "")}/${command}`;
      const stat = await fs.stat(candidate).catch(() => null);
      if (stat?.isFile() && (stat.mode & 0o111) !== 0) return candidate;
    }
    return null;
  }

  async function classifyDesktop(
    id: string,
    file: string,
    keys: Record<string, string>,
    ownReal: string | null
  ): Promise<CatalogCandidate | null> {
    if (/pangeavpn/i.test(id)) return null;
    if (keys.TryExec && !(await which(keys.TryExec.trim()))) return null;
    const argv = stripEnvPrefix(tokenizeExec(keys.Exec ?? "") ?? []);
    if (argv.length === 0) return null;
    const resolved = await which(argv[0]);
    const real = resolved ? await realPosixPath(resolved) : null;
    if (real && ownReal && real === ownReal) return null;
    const head = real ? await readHead(real) : null;
    return linuxCandidate(
      id,
      keys,
      classifyLinuxApp({ keys, desktopFile: file, home: os.homedir(), argv, resolved, real, head })
    );
  }

  async function scanLinux(): Promise<CatalogCandidate[]> {
    const home = os.homedir();
    const found: { id: string; file: string; keys: Record<string, string> | null }[] = [];
    for (const base of linuxDesktopDirs(process.env, home)) {
      for (const file of await walk(base, [".desktop"], 4)) {
        found.push({ id: desktopFileId(base, file), file, keys: parseDesktopEntry((await readText(file)) ?? "") });
      }
    }
    const desktops = (process.env.XDG_CURRENT_DESKTOP ?? "").split(":").filter(Boolean);
    const shown = selectDesktopEntries(found, desktops);
    const ownReal = await realPosixPath(process.execPath);
    const rows: CatalogCandidate[] = [];
    for (const [index, entry] of shown.entries()) {
      if (index > 0 && index % 10 === 0) await yieldToLoop();
      const candidate = entry.keys ? await classifyDesktop(entry.id, entry.file, entry.keys, ownReal) : null;
      if (candidate) rows.push(candidate);
    }
    console.log(`split tunnel catalog: ${found.length} desktop files, ${rows.length} listed`);
    return dedupeCandidates(rows, "linux");
  }

  function scan(): Promise<CatalogCandidate[]> {
    if (platform === "win32") return scanWindows();
    if (platform === "darwin") return scanMac();
    if (platform === "linux") return scanLinux();
    return Promise.resolve([]);
  }

  async function loadCatalog(refresh: boolean): Promise<CatalogCandidate[]> {
    if (scanning) return scanning;
    if (catalog && !refresh) return catalog;
    if (refresh) iconCache.clear();
    scanning = scan()
      .then((rows) => {
        rows.sort((a, b) => a.name.localeCompare(b.name, undefined, { sensitivity: "base" }));
        catalog = rows;
        byRule = new Map(rows.filter((row) => row.rule).map((row) => [splitRuleKey(row.rule, platform), row]));
        for (const row of rows) remember(row);
        return rows;
      })
      .finally(() => {
        scanning = null;
      });
    return scanning;
  }

  function sourceForRule(rule: string): IconSource | null {
    const kind = ruleKind(rule, platform);
    if (platform === "linux") return null;
    if (kind === "dir") return { kind: "dirExe", path: rule.replace(/[\\/]+$/, "") };
    return { kind: "exe", path: rule };
  }

  async function describeRule(rule: string): Promise<CatalogCandidate> {
    const known = byRule.get(splitRuleKey(rule, platform));
    const present = await exists(rule);
    if (known && present) return { ...known, rule };
    const kind = ruleKind(rule, platform);
    const candidate: CatalogCandidate = {
      id: `rule:${splitRuleKey(rule, platform)}`,
      name: known?.name ?? displayNameForRule(rule, platform),
      rule,
      exe: kind === "dir" ? "" : rule,
      kind
    };
    const source = present ? sourceForRule(rule) : null;
    if (source) candidate.iconSource = source;
    if (!present) candidate.missing = true;
    const warning = kind === "dir" ? undefined : warningFor(rule, platform);
    if (warning) candidate.warning = warning;
    return candidate;
  }

  function isOwnImage(real: string): boolean {
    return ownImages().some((own) => {
      const key = splitRuleKey(own, platform);
      const target = splitRuleKey(real, platform);
      return target === key || (platform === "darwin" && target.startsWith(`${key}/`));
    });
  }

  function handOut(candidate: CatalogCandidate): SplitTunnelBrowseResult {
    const known = candidate.rule ? byRule.get(splitRuleKey(candidate.rule, platform)) : undefined;
    const chosen = known ? { ...known, rule: candidate.rule } : candidate;
    remember(chosen);
    return { ok: true, entry: toAppEntry(chosen) };
  }

  async function pickWindows(picked: string): Promise<SplitTunnelBrowseResult> {
    const env = winEnvFrom(process.env);
    const lower = picked.toLowerCase();
    if (lower.endsWith(".url")) {
      const appId = steamAppIdFromUrl(parseInternetShortcut((await readText(picked)) ?? "").url);
      const game = appId ? (await loadCatalog(false)).find((row) => row.id === `steam:${appId}`) : undefined;
      return game ? handOut(game) : { ok: false, reason: "notAnApp" };
    }
    let target = picked;
    let args = "";
    if (lower.endsWith(".lnk")) {
      const link = await readShortcut(picked);
      if (!link) return { ok: false, reason: "notAnApp" };
      target = link.target;
      args = link.args;
    }
    const real = await realWinPath(target);
    if (!real || !real.toLowerCase().endsWith(".exe")) return { ok: false, reason: "notAnApp" };
    if (isOwnImage(real)) return { ok: false, reason: "ownImage" };
    const derived = await deriveWinRule(real, args, env, winProbe);
    const name = displayNameForRule(derived.kind === "dir" ? derived.rule : real, "win32");
    return handOut(winCandidate(name, derived, args));
  }

  async function pickMac(picked: string): Promise<SplitTunnelBrowseResult> {
    const real = (await realPosixPath(picked)) ?? picked;
    if (isOwnImage(real) || isOwnImage(picked)) return { ok: false, reason: "ownImage" };
    const bundle = macBundleRoot(real);
    if (bundle) {
      const candidate = macBundleCandidate(bundle, []);
      return candidate ? handOut(candidate) : { ok: false, reason: "notAnApp" };
    }
    const stat = await fs.stat(real).catch(() => null);
    if (!stat?.isFile()) return { ok: false, reason: "notAnApp" };
    return handOut(await describeRule(real));
  }

  async function pickLinux(picked: string): Promise<SplitTunnelBrowseResult> {
    const ownReal = await realPosixPath(process.execPath);
    if (picked.toLowerCase().endsWith(".desktop")) {
      const keys = parseDesktopEntry((await readText(picked)) ?? "");
      const id = path.posix.basename(picked);
      const candidate = keys?.Name && keys.Exec ? await classifyDesktop(id, picked, keys, ownReal) : null;
      return candidate ? handOut(candidate) : { ok: false, reason: "notAnApp" };
    }
    const real = await realPosixPath(picked);
    if (!real) return { ok: false, reason: "notAnApp" };
    if (real === ownReal || isOwnImage(real)) return { ok: false, reason: "ownImage" };
    const packaged = linuxPackagedDir(picked) ?? linuxPackagedDir(real);
    if (packaged) return handOut(await describeRule(packaged));
    const head = await readHead(real);
    if (!head || head === "other") return { ok: false, reason: "notAnApp" };
    const name = path.posix.basename(picked);
    const result = classifyLinuxApp({
      keys: {},
      desktopFile: "",
      home: os.homedir(),
      argv: [picked],
      resolved: picked,
      real,
      head
    });
    const candidate = linuxCandidate(`pick:${real}`, { Name: name }, result);
    return candidate ? handOut(candidate) : { ok: false, reason: "notAnApp" };
  }

  return {
    async list(refresh) {
      return (await loadCatalog(refresh)).map((row) => toAppEntry(row));
    },

    async describe(rules) {
      // Described before the first scan lands, a Steam or Squirrel rule would be named after its folder.
      if (!catalog && scanning) await settleWithin(scanning, DESCRIBE_SCAN_WAIT_MS);
      const used = new Set<string>();
      const out: SplitTunnelAppEntry[] = [];
      for (const [index, rule] of rules.entries()) {
        const candidate = await describeRule(rule);
        const id = used.has(candidate.id) ? `${candidate.id}#${index}` : candidate.id;
        used.add(id);
        remember(candidate, id);
        out.push(toAppEntry({ ...candidate, id }));
      }
      return out;
    },

    async icons(keys) {
      const out: SplitTunnelIcon[] = [];
      for (const key of keys.slice(0, MAX_ICON_KEYS)) {
        let icon = iconCache.get(key);
        if (icon === undefined) {
          const source = iconSources.get(key);
          icon = source ? await iconFor(source, platform).catch(() => "") : "";
          if (source !== undefined) iconCache.set(key, icon);
        }
        out.push({ key, icon });
      }
      return out;
    },

    dialogOptions() {
      const options: OpenDialogOptions = {
        title: mt("dialog.splitTunnel.title"),
        buttonLabel: mt("dialog.splitTunnel.button"),
        properties: ["openFile"]
      };
      const apps = mt("dialog.splitTunnel.apps");
      const all = { name: mt("dialog.splitTunnel.allFiles"), extensions: ["*"] };
      if (platform === "win32") options.filters = [{ name: apps, extensions: ["exe", "lnk", "url"] }, all];
      if (platform === "darwin") {
        options.defaultPath = "/Applications";
        options.filters = [{ name: apps, extensions: ["app"] }, all];
      }
      return options;
    },

    async describePick(picked) {
      if (platform === "win32") return pickWindows(picked);
      if (platform === "darwin") return pickMac(picked);
      return pickLinux(picked);
    }
  };
}
