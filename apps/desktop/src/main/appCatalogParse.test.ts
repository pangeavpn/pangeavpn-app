import assert from "node:assert/strict";
import test from "node:test";
import {
  classifyExecutableHead,
  classifyLinuxApp,
  dedupeCandidates,
  deriveWinRule,
  desktopFileId,
  displayNameForRule,
  dropCoveredByDirs,
  epicGameCandidate,
  isWinProtectedDir,
  isWinSharedDir,
  linuxCandidate,
  linuxDesktopDirs,
  linuxIconCandidates,
  linuxPackagedDir,
  macBundleCandidate,
  macBundleRoot,
  parseAppManifest,
  parseDesktopEntry,
  parseInternetShortcut,
  parseRegSteamPath,
  parseVdf,
  isLocalAbsolutePath,
  ruleKind,
  ruleMatchesPath,
  scriptRuleDir,
  selectDesktopEntries,
  settleWithin,
  squirrelProcessStart,
  steamAppIdFromUrl,
  steamGameCandidate,
  steamLibraryPaths,
  steamRootFromIconFile,
  stripEnvPrefix,
  tokenizeExec,
  warningFor,
  winCandidate,
  winEnvFrom,
  winShortcutSkipReason,
  type CatalogCandidate,
  type ExecutableHead,
  type WinProbe
} from "./appCatalogParse.ts";

const ENV = winEnvFrom({
  SystemDrive: "C:",
  SystemRoot: "C:\\Windows",
  ProgramFiles: "C:\\Program Files",
  "ProgramFiles(x86)": "C:\\Program Files (x86)",
  ProgramData: "C:\\ProgramData",
  USERPROFILE: "C:\\Users\\dana",
  APPDATA: "C:\\Users\\dana\\AppData\\Roaming",
  LOCALAPPDATA: "C:\\Users\\dana\\AppData\\Local",
  TEMP: "D:\\Temp"
});

const LOCAL = "C:\\Users\\dana\\AppData\\Local";

/** A fake filesystem: every listed path exists, and its parents list it. */
function probeOf(paths: readonly string[]): WinProbe {
  const lower = new Set(paths.map((path) => path.toLowerCase()));
  return {
    exists: async (path) => lower.has(path.toLowerCase()),
    list: async (dir) => {
      const prefix = `${dir.toLowerCase()}\\`;
      const names = new Set<string>();
      for (const path of paths) {
        if (path.toLowerCase().startsWith(prefix)) names.add(path.slice(prefix.length).split("\\")[0]);
      }
      return [...names];
    }
  };
}

test("Squirrel: Update.exe --processStart becomes the app's root folder", async () => {
  const probe = probeOf([
    `${LOCAL}\\Discord\\Update.exe`,
    `${LOCAL}\\Discord\\app-1.0.9030\\Discord.exe`,
    `${LOCAL}\\Discord\\app-1.0.9029\\Discord.exe`
  ]);
  const derived = await deriveWinRule(`${LOCAL}\\Discord\\Update.exe`, '--processStart "Discord.exe"', ENV, probe);
  assert.deepEqual(derived, {
    rule: `${LOCAL}\\Discord\\`,
    kind: "dir",
    exe: `${LOCAL}\\Discord\\app-1.0.9030\\Discord.exe`
  });
  assert.equal(squirrelProcessStart("--processStartAndWait Slack.exe --process-start-args --startup"), "Slack.exe");
  assert.equal(squirrelProcessStart("--processStart=Teams.exe"), "Teams.exe");
  assert.equal(squirrelProcessStart("--processStart ..\\evil\\x.exe"), null);
});

test("Squirrel: an exe inside app-<version> with Update.exe beside it becomes the root", async () => {
  const probe = probeOf([`${LOCAL}\\Discord\\Update.exe`, `${LOCAL}\\Discord\\app-1.0.9030\\Discord.exe`]);
  const derived = await deriveWinRule(`${LOCAL}\\Discord\\app-1.0.9030\\Discord.exe`, "", ENV, probe);
  assert.equal(derived.rule, `${LOCAL}\\Discord\\`);
  assert.equal(derived.kind, "dir");
});

test("Squirrel: a stub exe in the root becomes the root", async () => {
  const probe = probeOf([`${LOCAL}\\slack\\Update.exe`, `${LOCAL}\\slack\\slack.exe`, `${LOCAL}\\slack\\app-4.41.105\\slack.exe`]);
  const derived = await deriveWinRule(`${LOCAL}\\slack\\slack.exe`, "", ENV, probe);
  assert.equal(derived.rule, `${LOCAL}\\slack\\`);
});

test("app-<version> without an Update.exe sibling stays an exact exe", async () => {
  const derived = await deriveWinRule("D:\\Tools\\app-2.1\\tool.exe", "", ENV, probeOf(["D:\\Tools\\app-2.1\\tool.exe"]));
  assert.deepEqual(derived, { rule: "D:\\Tools\\app-2.1\\tool.exe", kind: "file", exe: "D:\\Tools\\app-2.1\\tool.exe" });
});

test("version-<hash> folders generalise to their parent", async () => {
  const exe = `${LOCAL}\\Roblox\\Versions\\version-0123456789abcdef\\RobloxPlayerBeta.exe`;
  const derived = await deriveWinRule(exe, "", ENV, probeOf([exe]));
  assert.equal(derived.rule, `${LOCAL}\\Roblox\\Versions\\`);
  assert.equal(derived.exe, exe);
});

test("a dotted version folder generalises; a bare number does not", async () => {
  const dotted = await deriveWinRule("C:\\Program Files\\Foo\\1.2.3\\foo.exe", "", ENV, probeOf([]));
  assert.equal(dotted.rule, "C:\\Program Files\\Foo\\");
  const bare = await deriveWinRule("C:\\Program Files\\Foo\\2024\\foo.exe", "", ENV, probeOf([]));
  assert.equal(bare.rule, "C:\\Program Files\\Foo\\2024\\foo.exe");
});

test("only the deepest versioned folder is generalised", async () => {
  const derived = await deriveWinRule("D:\\Apps\\1.0\\Foo\\2.3.4\\foo.exe", "", ENV, probeOf([]));
  assert.equal(derived.rule, "D:\\Apps\\1.0\\Foo\\");
});

test("never a rule at or above a protected root: keep the exact exe", async () => {
  const underProgramFiles = await deriveWinRule("C:\\Program Files\\1.2\\x.exe", "", ENV, probeOf([]));
  assert.equal(underProgramFiles.kind, "file");
  const underLocal = await deriveWinRule(`${LOCAL}\\9.1\\x.exe`, "", ENV, probeOf([]));
  assert.equal(underLocal.rule, `${LOCAL}\\9.1\\x.exe`);
  const driveRoot = await deriveWinRule("D:\\3.0\\x.exe", "", ENV, probeOf([]));
  assert.equal(driveRoot.kind, "file");
  const squirrelInLocal = await deriveWinRule(
    `${LOCAL}\\Update.exe`,
    "--processStart App.exe",
    ENV,
    probeOf([`${LOCAL}\\Update.exe`, `${LOCAL}\\app-1.2.0\\App.exe`])
  );
  assert.deepEqual(squirrelInLocal, { rule: `${LOCAL}\\app-1.2.0\\App.exe`, kind: "file", exe: `${LOCAL}\\app-1.2.0\\App.exe` });
});

test("never a folder rule for a shared folder like Downloads, Temp or Common Files: keep the exact exe", async () => {
  for (const exe of [
    "C:\\Users\\dana\\Downloads\\2.1\\tool.exe",
    "C:\\Users\\dana\\Desktop\\1.0\\tool.exe",
    "C:\\Users\\dana\\Documents\\3.4.5\\tool.exe",
    "C:\\Users\\dana\\Downloads\\version-0123456789abcdef\\tool.exe",
    "C:\\Users\\someone\\Videos\\1.0\\tool.exe",
    "C:\\Users\\dana\\OneDrive\\Desktop\\1.2\\x.exe",
    "C:\\Users\\dana\\OneDrive - Contoso\\1.2\\x.exe",
    "C:\\Users\\Public\\Downloads\\1.2\\x.exe",
    `${LOCAL}\\Temp\\1.2\\x.exe`,
    `${LOCAL}\\Microsoft\\1.2\\x.exe`,
    "C:\\Users\\dana\\AppData\\Roaming\\Microsoft\\1.2\\x.exe",
    "C:\\Program Files\\Common Files\\1.0\\x.exe",
    "C:\\Program Files (x86)\\Common Files\\1.0\\x.exe",
    "D:\\Temp\\1.0\\x.exe"
  ]) {
    assert.deepEqual(await deriveWinRule(exe, "", ENV, probeOf([])), { rule: exe, kind: "file", exe }, exe);
  }
  const nested = await deriveWinRule("C:\\Users\\dana\\Downloads\\tool\\2.1\\tool.exe", "", ENV, probeOf([]));
  assert.deepEqual(nested, { rule: "C:\\Users\\dana\\Downloads\\tool\\", kind: "dir", exe: "C:\\Users\\dana\\Downloads\\tool\\2.1\\tool.exe" });
});

test("a Squirrel app unpacked straight into a shared folder keeps its exe", async () => {
  const downloads = "C:\\Users\\dana\\Downloads";
  const inner = `${downloads}\\app-1.2.0\\App.exe`;
  const probe = probeOf([`${downloads}\\Update.exe`, inner, `${downloads}\\App.exe`]);
  assert.deepEqual(await deriveWinRule(`${downloads}\\Update.exe`, "--processStart App.exe", ENV, probe), {
    rule: inner,
    kind: "file",
    exe: inner
  });
  assert.equal((await deriveWinRule(`${downloads}\\App.exe`, "", ENV, probe)).kind, "file");
  assert.equal((await deriveWinRule(inner, "", ENV, probe)).kind, "file");
});

test("shared folders are only the folders themselves, not the app folders inside them", () => {
  for (const dir of [
    "C:\\Users\\dana\\Downloads",
    "c:\\users\\dana\\desktop\\",
    `${LOCAL}\\Temp`,
    "D:\\Temp",
    "C:\\Program Files\\Common Files",
    "C:\\Users\\Public\\Documents"
  ]) {
    assert.ok(isWinSharedDir(dir, ENV), dir);
  }
  for (const dir of [
    "C:\\Users\\dana\\Downloads\\tool",
    `${LOCAL}\\Discord`,
    `${LOCAL}\\Microsoft\\Teams`,
    "C:\\Program Files\\Common Files\\Vendor",
    "D:\\Games"
  ]) {
    assert.ok(!isWinSharedDir(dir, ENV), dir);
  }
});

test("protected roots cover system, shared and profile folders for any user", () => {
  for (const dir of [
    "C:\\",
    "C:\\Windows",
    "C:\\Windows\\System32",
    "C:\\Program Files",
    "c:\\program files (x86)\\",
    "C:\\ProgramData",
    "C:\\Users",
    "C:\\Users\\someone",
    "C:\\Users\\someone\\AppData",
    "C:\\Users\\someone\\AppData\\Local",
    "C:\\Users\\someone\\AppData\\Local\\Programs",
    "C:\\Users\\dana\\AppData\\Roaming",
    "C:\\Program Files\\WindowsApps",
    "\\\\server\\share\\apps"
  ]) {
    assert.ok(isWinProtectedDir(dir, ENV), dir);
  }
  for (const dir of ["C:\\Program Files\\Foo", `${LOCAL}\\Discord`, "D:\\Games", `${LOCAL}\\Programs\\Microsoft VS Code`]) {
    assert.ok(!isWinProtectedDir(dir, ENV), dir);
  }
});

test("folder rules keep their boundary: Discord never covers DiscordPTB", () => {
  const discord = `${LOCAL}\\Discord\\`;
  assert.ok(ruleMatchesPath(discord, `${LOCAL}\\Discord\\app-1.0.9030\\Discord.exe`, "win32"));
  assert.ok(ruleMatchesPath(discord, `${LOCAL.toLowerCase()}/discord/update.exe`, "win32"));
  assert.ok(!ruleMatchesPath(discord, `${LOCAL}\\DiscordPTB\\app-1.0.1100\\DiscordPTB.exe`, "win32"));
  assert.ok(!ruleMatchesPath(`${LOCAL}\\DiscordPTB\\`, `${LOCAL}\\Discord\\Update.exe`, "win32"));
  const both = dedupeCandidates(
    [
      winCandidate("Discord", { rule: discord, kind: "dir", exe: "" }, ""),
      winCandidate("Discord PTB", { rule: `${LOCAL}\\DiscordPTB\\`, kind: "dir", exe: "" }, "")
    ],
    "win32"
  );
  assert.equal(both.length, 2);
});

test("shortcut filters drop proxies, installers, system tools and hosted scripts", () => {
  const skip = (name: string, target: string, args = "") => winShortcutSkipReason({ name, target, args }, ENV, ["C:\\Program Files\\PangeaVPN\\PangeaVPN.exe"]);
  assert.equal(skip("YouTube", "C:\\Program Files\\Google\\Chrome\\Application\\chrome_proxy.exe", "--profile-directory=Default --app-id=x"), "proxy");
  assert.equal(skip("Edge app", "C:\\Program Files (x86)\\Microsoft\\Edge\\Application\\msedge_proxy.exe"), "proxy");
  assert.equal(skip("Uninstall Foo", "C:\\Program Files\\Foo\\unins000.exe"), "installer");
  assert.equal(skip("Foo Setup", "C:\\Program Files\\Foo\\setup.exe"), "installer");
  assert.equal(skip("Foo Installer", "C:\\Program Files\\Foo\\installer.exe"), "installer");
  assert.equal(skip("Deinstallieren", "C:\\Program Files\\Foo\\uninst-helper.exe"), "installer");
  assert.equal(skip("Remove Foo", "C:\\Program Files\\Foo\\foo.exe", "/x {GUID}"), "installer");
  assert.equal(skip("Repair", "C:\\Windows\\System32\\msiexec.exe", "/f x.msi"), "installer");
  assert.equal(skip("Command Prompt", "C:\\Windows\\System32\\cmd.exe"), "system");
  assert.equal(skip("Remote Desktop Connection", "C:\\Windows\\System32\\mstsc.exe"), null);
  assert.equal(skip("Run script", "C:\\Python312\\python.exe", "C:\\scripts\\x.py"), "hostWithArgs");
  assert.equal(skip("Python 3.12", "C:\\Python312\\python.exe"), null);
  assert.equal(skip("Read me", "C:\\Program Files\\Foo\\readme.html"), "notExe");
  assert.equal(skip("Store app", ""), "noTarget");
  assert.equal(skip("Share", "\\\\server\\apps\\tool.exe"), "notLocal");
  assert.equal(skip("PangeaVPN", "c:\\program files\\pangeavpn\\pangeavpn.exe"), "self");
  assert.equal(skip("PangeaVPN (other install)", "D:\\Apps\\PangeaVPN\\PangeaVPN.exe"), "self");
  assert.equal(skip("Visual Studio Code", `${LOCAL}\\Programs\\Microsoft VS Code\\Code.exe`), null);
});

test("dedupe keeps the shortcut without arguments", () => {
  const derived = { rule: "C:\\Program Files\\Foo\\foo.exe", kind: "file" as const, exe: "C:\\Program Files\\Foo\\foo.exe" };
  const rows = dedupeCandidates(
    [winCandidate("Foo (safe mode)", derived, "--safe-mode"), winCandidate("Foo", derived, ""), winCandidate("Foo again", derived, "")],
    "win32"
  );
  assert.deepEqual(rows.map((row) => row.name), ["Foo"]);
});

test("launchers and shells carry the inheritance warning", () => {
  assert.equal(winCandidate("Steam", { rule: "C:\\Program Files (x86)\\Steam\\steam.exe", kind: "file", exe: "C:\\Program Files (x86)\\Steam\\steam.exe" }, "").warning, "inherits");
  assert.equal(warningFor("C:\\Program Files\\PowerShell\\7\\pwsh.exe", "win32"), "inherits");
  assert.equal(warningFor("C:\\Program Files\\Foo\\foo.exe", "win32"), undefined);
  assert.equal(warningFor("/Applications/Safari.app", "darwin"), "mayNotWork");
  assert.equal(warningFor("/System/Applications/Utilities/Terminal.app", "darwin"), "inherits");
});

const LIBRARY_FOLDERS = `"libraryfolders"
{
\t"0"
\t{
\t\t"path"\t\t"C:\\\\Program Files (x86)\\\\Steam"
\t\t"label"\t\t""
\t\t"apps"
\t\t{
\t\t\t"228980"\t\t"1234"
\t\t\t"570"\t\t"5678"
\t\t}
\t}
\t"1"
\t{
\t\t"path"\t\t"D:\\\\SteamLibrary"
\t\t"apps"
\t\t{
\t\t\t"1091500"\t\t"999"
\t\t}
\t}
}
`;

const OLD_LIBRARY_FOLDERS = `"LibraryFolders"
{
\t"TimeNextStatsReport"\t\t"1690000000"
\t"ContentStatsID"\t\t"-123"
\t"1"\t\t"E:\\\\Games\\\\Steam"
}
`;

test("libraryfolders.vdf: nested format with escaped backslashes", () => {
  assert.deepEqual(steamLibraryPaths(LIBRARY_FOLDERS, "C:\\Program Files (x86)\\Steam"), [
    "C:\\Program Files (x86)\\Steam",
    "D:\\SteamLibrary"
  ]);
  const parsed = parseVdf(LIBRARY_FOLDERS);
  const zero = (parsed.libraryfolders as Record<string, unknown>)["0"] as Record<string, unknown>;
  assert.deepEqual(Object.keys(zero.apps as object).sort(), ["228980", "570"]);
});

test("libraryfolders.vdf: the old flat format still yields libraries", () => {
  assert.deepEqual(steamLibraryPaths(OLD_LIBRARY_FOLDERS, "C:/Program Files (x86)/Steam"), [
    "C:\\Program Files (x86)\\Steam",
    "E:\\Games\\Steam"
  ]);
});

test("parseVdf survives comments, conditionals and truncation", () => {
  const parsed = parseVdf('// header\n"a" { "b" "1" [$WIN32] "c" { "d" "x\\"y" } } "trunc" { "e" "');
  assert.deepEqual(JSON.parse(JSON.stringify(parsed)), { a: { b: "1", c: { d: 'x"y' } }, trunc: { e: "" } });
});

function acf(appid: string, name: string, installdir: string, stateFlags: number): string {
  return `"AppState"\n{\n\t"appid"\t\t"${appid}"\n\t"Universe"\t\t"1"\n\t"name"\t\t"${name}"\n\t"StateFlags"\t\t"${stateFlags}"\n\t"installdir"\t\t"${installdir}"\n}\n`;
}

test("appmanifest: installed games become folder rules; partial installs and redistributables do not", () => {
  const dota = parseAppManifest(acf("570", "Dota 2", "dota 2 beta", 4));
  assert.ok(dota);
  assert.deepEqual(steamGameCandidate("C:\\Program Files (x86)\\Steam", dota), {
    id: "steam:570",
    name: "Dota 2",
    rule: "C:\\Program Files (x86)\\Steam\\steamapps\\common\\dota 2 beta\\",
    exe: "",
    kind: "dir",
    iconSource: { kind: "dirExe", path: "C:\\Program Files (x86)\\Steam\\steamapps\\common\\dota 2 beta" }
  });
  const updating = parseAppManifest(acf("1091500", "Cyberpunk 2077", "Cyberpunk 2077", 6));
  assert.ok(updating && steamGameCandidate("D:\\SteamLibrary", updating));
  const downloading = parseAppManifest(acf("730", "Counter-Strike 2", "Counter-Strike Global Offensive", 1026));
  assert.equal(downloading && steamGameCandidate("D:\\SteamLibrary", downloading), null);
  const redist = parseAppManifest(acf("228980", "Steamworks Common Redistributables", "Steamworks Shared", 4));
  assert.equal(redist && steamGameCandidate("C:\\Steam", redist), null);
  const sneaky = parseAppManifest(acf("1", "x", "..", 4));
  assert.equal(sneaky && steamGameCandidate("C:\\Steam", sneaky), null);
  assert.equal(parseAppManifest('"AppState" { "name" "no id" }'), null);
});

test(".url shortcuts: Steam game id, icon file and the Steam root behind it", () => {
  const url = parseInternetShortcut(
    "[{000214A0-0000-0000-C000-000000000046}]\r\nProp3=19,0\r\n[InternetShortcut]\r\nIDList=\r\nIconIndex=0\r\nURL=steam://rungameid/570\r\nIconFile=C:\\Program Files (x86)\\Steam\\steam\\games\\0bbb630d63262dd66d2fdd0f7d37e8661a410075.ico\r\n"
  );
  assert.equal(url.url, "steam://rungameid/570");
  assert.equal(steamAppIdFromUrl(url.url), "570");
  assert.equal(steamRootFromIconFile(url.iconFile), "C:\\Program Files (x86)\\Steam");
  assert.equal(steamAppIdFromUrl("https://store.steampowered.com/app/570"), null);
  assert.equal(steamRootFromIconFile("C:\\Icons\\x.ico"), null);
});

test("reg query output gives the Steam path with Windows separators", () => {
  const stdout = "\r\nHKEY_CURRENT_USER\\Software\\Valve\\Steam\r\n    SteamPath    REG_SZ    c:/program files (x86)/steam\r\n\r\n";
  assert.equal(parseRegSteamPath(stdout), "c:\\program files (x86)\\steam");
  assert.equal(parseRegSteamPath("ERROR: The system was unable to find the specified registry key or value."), null);
});

test("Epic manifests become folder rules unless incomplete or at a protected root", () => {
  const manifest = (fields: Record<string, unknown>) =>
    JSON.stringify({ DisplayName: "Fortnite", AppName: "Fortnite", InstallLocation: "C:\\Program Files\\Epic Games\\Fortnite", LaunchExecutable: "FortniteGame/Binaries/Win64/FortniteLauncher.exe", ...fields });
  const fortnite = epicGameCandidate(manifest({}), ENV);
  assert.equal(fortnite?.rule, "C:\\Program Files\\Epic Games\\Fortnite\\");
  assert.deepEqual(fortnite?.iconSource, { kind: "exe", path: "C:\\Program Files\\Epic Games\\Fortnite\\FortniteGame\\Binaries\\Win64\\FortniteLauncher.exe" });
  assert.equal(epicGameCandidate(manifest({ bIsIncompleteInstall: true }), ENV), null);
  assert.equal(epicGameCandidate(manifest({ InstallLocation: "C:\\Program Files" }), ENV), null);
  assert.equal(epicGameCandidate("{not json", ENV), null);
});

test("shortcuts into a catalogued game folder are dropped", () => {
  const game = "D:\\SteamLibrary\\steamapps\\common\\Game\\";
  const lnk = winCandidate("Game", { rule: "D:\\SteamLibrary\\steamapps\\common\\Game\\bin\\game.exe", kind: "file", exe: "D:\\SteamLibrary\\steamapps\\common\\Game\\bin\\game.exe" }, "");
  const other = winCandidate("Other", { rule: "D:\\SteamLibrary\\steamapps\\common\\GameTwo\\two.exe", kind: "file", exe: "D:\\SteamLibrary\\steamapps\\common\\GameTwo\\two.exe" }, "");
  assert.deepEqual(dropCoveredByDirs([lnk, other], [game], "win32").map((row) => row.name), ["Other"]);
});

test("a pick inside a bundle maps to the outermost bundle", () => {
  assert.equal(macBundleRoot("/Applications/Slack.app"), "/Applications/Slack.app");
  assert.equal(
    macBundleRoot("/Applications/Slack.app/Contents/Frameworks/Slack Helper.app/Contents/MacOS/Slack Helper"),
    "/Applications/Slack.app"
  );
  assert.equal(macBundleRoot("/usr/local/bin/tool"), null);
  assert.equal(macBundleRoot("/Applications/.app/x"), null);
});

test("macOS bundles: own bundle skipped, Safari flagged, names from the bundle", () => {
  assert.equal(macBundleCandidate("/Applications/PangeaVPN.app", ["/Applications/PangeaVPN.app"]), null);
  assert.equal(macBundleCandidate("/Users/dana/Applications/PangeaVPN.app", []), null);
  assert.equal(macBundleCandidate("/Applications/Electron.app", ["/Applications/Electron.app/"]), null);
  assert.equal(macBundleCandidate("/applications/pangeavpn.app/", ["/Applications/PangeaVPN.app"]), null);
  const safari = macBundleCandidate("/System/Volumes/Preboot/Cryptexes/App/System/Applications/Safari.app", []);
  assert.equal(safari?.warning, "mayNotWork");
  const zoom = macBundleCandidate("/Applications/zoom.us.app/", []);
  assert.equal(zoom?.name, "zoom.us");
  assert.equal(zoom?.rule, "/Applications/zoom.us.app");
  assert.equal(zoom?.kind, "bundle");
  assert.equal(macBundleCandidate("/Applications/Utilities", []), null);
  assert.ok(ruleMatchesPath("/Applications/Zoom.us.app", "/Applications/zoom.us.app/Contents/MacOS/zoom.us", "darwin"));
  assert.ok(!ruleMatchesPath("/Applications/Zoom.app", "/Applications/Zoom.app2/x", "darwin"));
});

test("Exec tokenising: quotes, escapes, field codes and %%", () => {
  assert.deepEqual(tokenizeExec('"/opt/My App/bin/my app" --flag %U'), ["/opt/My App/bin/my app", "--flag"]);
  assert.deepEqual(tokenizeExec('sh -c "echo \\"hi\\" \\$HOME" %f'), ["sh", "-c", 'echo "hi" $HOME']);
  assert.deepEqual(tokenizeExec("app --rate=50%% --file=%f %i %c %k"), ["app", "--rate=50%", "--file="]);
  assert.deepEqual(tokenizeExec('app ""'), ["app", ""]);
  assert.equal(tokenizeExec('app "unterminated'), null);
});

test("desktop string escapes are undone before Exec quoting", () => {
  const keys = parseDesktopEntry('[Desktop Entry]\nName=Two\\sWords\nExec="/opt/a\\\\\\\\b/app" %u\n[Desktop Action new]\nName=Other\n');
  assert.ok(keys);
  assert.equal(keys.Name, "Two Words");
  assert.deepEqual(tokenizeExec(keys.Exec), ["/opt/a\\b/app"]);
  assert.equal(parseDesktopEntry("[Other Group]\nName=x\n"), null);
});

test("env prefixes are stripped with their options and assignments", () => {
  assert.deepEqual(stripEnvPrefix(["env", "BAMF_DESKTOP_FILE_HINT=/x.desktop", "/snap/bin/firefox"]), ["/snap/bin/firefox"]);
  assert.deepEqual(stripEnvPrefix(["/usr/bin/env", "-u", "GTK_THEME", "-i", "GDK_BACKEND=x11", "--", "foo", "A=1"]), ["foo", "A=1"]);
  assert.deepEqual(stripEnvPrefix(["env", "-S", "TERM=xterm byobu", "-x"]), ["byobu", "-x"]);
  assert.deepEqual(stripEnvPrefix(["firefox", "A=1"]), ["firefox", "A=1"]);
});

function facts(desktop: string, overrides: { resolved?: string | null; real?: string | null; head?: ExecutableHead | null; file?: string; home?: string }) {
  const keys = parseDesktopEntry(desktop);
  assert.ok(keys);
  const argv = stripEnvPrefix(tokenizeExec(keys.Exec) ?? []);
  return {
    keys,
    argv,
    desktopFile: overrides.file ?? "/usr/share/applications/x.desktop",
    home: overrides.home ?? "/home/dana",
    resolved: overrides.resolved === undefined ? argv[0] ?? null : overrides.resolved,
    real: overrides.real === undefined ? argv[0] ?? null : overrides.real,
    head: overrides.head === undefined ? "elf" : overrides.head
  };
}

test("snap entries with an env prefix become /snap/<instance>/", () => {
  const desktop =
    "[Desktop Entry]\nType=Application\nName=Firefox Web Browser\nExec=env BAMF_DESKTOP_FILE_HINT=/var/lib/snapd/desktop/applications/firefox_firefox.desktop /snap/bin/firefox %u\nIcon=/snap/firefox/current/default256.png\nX-SnapInstanceName=firefox\n";
  const result = classifyLinuxApp(facts(desktop, { resolved: "/snap/bin/firefox", real: "/usr/bin/snap" }));
  assert.deepEqual(result, { rule: "/snap/firefox/", kind: "dir", exe: "/snap/bin/firefox" });
  const noKey = classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=Code\nExec=/snap/bin/code.url-handler %U\n", { real: "/usr/bin/snap" }));
  assert.deepEqual(noKey, { rule: "/snap/code/", kind: "dir", exe: "/snap/bin/code.url-handler" });
});

test("flatpak entries map to the installation's app folder", () => {
  const desktop =
    "[Desktop Entry]\nType=Application\nName=Firefox\nExec=/usr/bin/flatpak run --branch=stable --arch=x86_64 --command=firefox --file-forwarding org.mozilla.firefox @@u %u @@\nX-Flatpak=org.mozilla.firefox\n";
  assert.deepEqual(classifyLinuxApp(facts(desktop, { file: "/var/lib/flatpak/exports/share/applications/org.mozilla.firefox.desktop" })), {
    rule: "/var/lib/flatpak/app/org.mozilla.firefox/",
    kind: "dir",
    exe: ""
  });
  const user = "[Desktop Entry]\nType=Application\nName=Spotify\nExec=flatpak run com.spotify.Client\n";
  assert.deepEqual(
    classifyLinuxApp(facts(user, { file: "/home/dana/.local/share/flatpak/exports/share/applications/com.spotify.Client.desktop" })),
    { rule: "/home/dana/.local/share/flatpak/app/com.spotify.Client/", kind: "dir", exe: "" }
  );
  assert.deepEqual(classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=X\nExec=flatpak run\n", {})), {
    unsupported: "flatpak",
    exe: "flatpak"
  });
});

test("picked files inside snap and flatpak trees map to their app folder", () => {
  assert.equal(linuxPackagedDir("/snap/firefox/4793/usr/lib/firefox/firefox"), "/snap/firefox/");
  assert.equal(linuxPackagedDir("/snap/bin/code.url-handler"), "/snap/code/");
  assert.equal(linuxPackagedDir("/snap/bin/"), null);
  assert.equal(
    linuxPackagedDir("/var/lib/flatpak/app/org.mozilla.firefox/x86_64/stable/abc/files/lib/firefox/firefox"),
    "/var/lib/flatpak/app/org.mozilla.firefox/"
  );
  assert.equal(
    linuxPackagedDir("/home/dana/.local/share/flatpak/app/com.spotify.Client/current/active/files/bin/spotify"),
    "/home/dana/.local/share/flatpak/app/com.spotify.Client/"
  );
  assert.equal(linuxPackagedDir("/var/lib/flatpak/app/notanid/x"), null);
  assert.equal(linuxPackagedDir("/usr/bin/firefox"), null);
});

test("a quoted path with spaces resolves to the exact binary", () => {
  const desktop = '[Desktop Entry]\nType=Application\nName=My App\nExec="/opt/My App/myapp" --new-window %U\n';
  assert.deepEqual(classifyLinuxApp(facts(desktop, {})), { rule: "/opt/My App/myapp", kind: "file", exe: "/opt/My App/myapp" });
});

test("env VAR=value prefixes are skipped before resolving", () => {
  const desktop = "[Desktop Entry]\nType=Application\nName=Foo\nExec=env GDK_BACKEND=x11 foo %F\n";
  const result = classifyLinuxApp(facts(desktop, { resolved: "/usr/bin/foo", real: "/usr/lib/foo/foo" }));
  assert.deepEqual(result, { rule: "/usr/lib/foo/foo", kind: "file", exe: "/usr/lib/foo/foo" });
});

test("an /opt wrapper script maps to its app folder", () => {
  const desktop = "[Desktop Entry]\nType=Application\nName=Google Chrome\nExec=/usr/bin/google-chrome-stable %U\n";
  const result = classifyLinuxApp(facts(desktop, { resolved: "/usr/bin/google-chrome-stable", real: "/opt/google/chrome/google-chrome", head: "script" }));
  assert.deepEqual(result, { rule: "/opt/google/chrome/", kind: "dir", exe: "/opt/google/chrome/google-chrome" });
  const code = classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=Code\nExec=/usr/share/code/bin/code\n", { head: "script" }));
  assert.deepEqual(code, { rule: "/usr/share/code/", kind: "dir", exe: "/usr/share/code/bin/code" });
});

test("a script in a shared bin folder is unsupported, never /usr/", () => {
  const desktop = "[Desktop Entry]\nType=Application\nName=Byobu\nExec=byobu\n";
  const result = classifyLinuxApp(facts(desktop, { resolved: "/usr/bin/byobu", real: "/usr/bin/byobu", head: "script" }));
  assert.deepEqual(result, { unsupported: "script", exe: "/usr/bin/byobu" });
  assert.equal(scriptRuleDir("/usr/local/bin/tool", "/home/dana"), null);
  assert.equal(scriptRuleDir("/home/dana/bin/tool", "/home/dana"), null);
  assert.equal(scriptRuleDir("/home/dana/.local/bin/tool", "/home/dana"), null);
  assert.equal(scriptRuleDir("/home/dana/apps/tool/run.sh", "/home/dana"), "/home/dana/apps/tool/");
  assert.equal(scriptRuleDir("/nix/store/abc123-firefox-120.0/bin/firefox", "/home/dana"), "/nix/store/abc123-firefox-120.0/");
  assert.equal(scriptRuleDir("/usr/lib/x86_64-linux-gnu/helper", "/home/dana"), null);
  assert.equal(scriptRuleDir("/usr/lib/firefox/firefox.sh", "/home/dana"), "/usr/lib/firefox/");
  assert.equal(scriptRuleDir("/opt/run.sh", "/home/dana"), null);
});

test("interpreters and shells are unsupported", () => {
  const python = classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=Tool\nExec=python3 /opt/tool/app.py\n", { resolved: "/usr/bin/python3", real: "/usr/bin/python3.10" }));
  assert.deepEqual(python, { unsupported: "script", exe: "/usr/bin/python3.10" });
  const shell = classifyLinuxApp(facts('[Desktop Entry]\nType=Application\nName=Tool\nExec=sh -c "cd /opt/x && ./run"\n', { resolved: "/usr/bin/sh", real: "/usr/bin/dash" }));
  assert.deepEqual(shell, { unsupported: "script", exe: "/usr/bin/dash" });
});

test("AppImages are unsupported; missing commands are skipped", () => {
  const appimage = classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=Obsidian\nExec=/home/dana/Apps/Obsidian.AppImage\n", {}));
  assert.deepEqual(appimage, { unsupported: "appimage", exe: "/home/dana/Apps/Obsidian.AppImage" });
  const magic = classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=X\nExec=/home/dana/Apps/x\n", { head: "appimage" }));
  assert.ok("unsupported" in magic && magic.unsupported === "appimage");
  assert.deepEqual(classifyLinuxApp(facts("[Desktop Entry]\nType=Application\nName=Gone\nExec=gone\n", { resolved: null, real: null })), { skip: "missing" });
  const head = new Uint8Array([0x7f, 0x45, 0x4c, 0x46, 2, 1, 1, 0, 0x41, 0x49, 0x02]);
  assert.equal(classifyExecutableHead(head), "appimage");
  assert.equal(classifyExecutableHead(head.slice(0, 8)), "elf");
  assert.equal(classifyExecutableHead(new TextEncoder().encode("#!/bin/sh\n")), "script");
  assert.equal(classifyExecutableHead(new Uint8Array([1, 2, 3])), "other");
});

test("a user's Hidden=true copy shadows the system entry with the same ID", () => {
  const found = [
    { id: "foo.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=Foo\nExec=foo\nHidden=true\n") },
    { id: "foo.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=Foo\nExec=foo\n") },
    { id: "bar.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=Bar\nExec=bar\n") },
    { id: "term.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=Top\nExec=htop\nTerminal=true\n") },
    { id: "kde.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=K\nExec=k\nOnlyShowIn=KDE;\n") },
    { id: "nognome.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=N\nExec=n\nNotShowIn=GNOME;\n") },
    { id: "link.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Link\nName=L\nURL=https://x\n") },
    { id: "nodisplay.desktop", keys: parseDesktopEntry("[Desktop Entry]\nType=Application\nName=ND\nExec=nd\nNoDisplay=true\n") }
  ];
  assert.deepEqual(selectDesktopEntries(found, ["ubuntu", "GNOME"]).map((entry) => entry.id), ["bar.desktop"]);
  assert.deepEqual(selectDesktopEntries(found, ["KDE"]).map((entry) => entry.id), ["bar.desktop", "kde.desktop", "nognome.desktop"]);
  assert.equal(desktopFileId("/usr/share/applications/", "/usr/share/applications/kde4/okular.desktop"), "kde4-okular.desktop");
});

test("linux rows carry the Icon= name and keep unsupported entries rule-less", () => {
  const keys = parseDesktopEntry("[Desktop Entry]\nType=Application\nName=Konsole\nExec=konsole\nIcon=utilities-terminal\n");
  assert.ok(keys);
  const row = linuxCandidate("org.kde.konsole.desktop", keys, { rule: "/usr/bin/konsole", kind: "file", exe: "/usr/bin/konsole" });
  assert.deepEqual(row, {
    id: "desktop:org.kde.konsole.desktop",
    name: "Konsole",
    rule: "/usr/bin/konsole",
    exe: "/usr/bin/konsole",
    kind: "file",
    iconSource: { kind: "theme", name: "utilities-terminal" },
    warning: "inherits"
  });
  const unsupported = linuxCandidate("x.desktop", keys, { unsupported: "appimage", exe: "/x.AppImage" });
  assert.equal(unsupported?.rule, "");
  assert.equal(unsupported?.unsupported, "appimage");
  assert.equal(linuxCandidate("x.desktop", keys, { skip: "missing" }), null);
});

test("linux dirs and icon lookups follow XDG order", () => {
  assert.deepEqual(linuxDesktopDirs({ XDG_DATA_DIRS: "/usr/share/ubuntu:/usr/local/share:/usr/share:/var/lib/snapd/desktop" }, "/home/dana"), [
    "/home/dana/.local/share/applications",
    "/usr/share/ubuntu/applications",
    "/usr/local/share/applications",
    "/usr/share/applications",
    "/var/lib/snapd/desktop/applications",
    "/var/lib/flatpak/exports/share/applications",
    "/home/dana/.local/share/flatpak/exports/share/applications"
  ]);
  const icons = linuxIconCandidates("firefox", {}, "/home/dana");
  assert.equal(icons[0], "/home/dana/.icons/hicolor/scalable/apps/firefox.svg");
  assert.ok(icons.includes("/usr/share/icons/hicolor/48x48/apps/firefox.png"));
  assert.equal(icons[icons.length - 1], "/usr/share/pixmaps/firefox.png");
  assert.ok(!icons.some((path) => path.includes("/scalable/") && path.endsWith(".png")));
  assert.deepEqual(linuxIconCandidates("/snap/firefox/current/default256.png", {}, "/home/dana"), ["/snap/firefox/current/default256.png"]);
  assert.deepEqual(linuxIconCandidates("../etc/x", {}, "/home/dana"), []);
});

test("only local absolute paths are local: no share or device namespace is ever stat'd", () => {
  assert.ok(isLocalAbsolutePath("C:\\Games\\Foo\\foo.exe", "win32"));
  assert.ok(isLocalAbsolutePath("d:/tools/x.exe", "win32"));
  assert.ok(!isLocalAbsolutePath("\\\\attacker\\share\\x.exe", "win32"));
  assert.ok(!isLocalAbsolutePath("//attacker/share/x.exe", "win32"));
  assert.ok(!isLocalAbsolutePath("\\\\?\\C:\\Games\\x.exe", "win32"));
  assert.ok(!isLocalAbsolutePath("\\\\.\\pipe\\x", "win32"));
  assert.ok(!isLocalAbsolutePath("Games\\x.exe", "win32"));
  assert.ok(!isLocalAbsolutePath("C:x.exe", "win32"));
  assert.ok(isLocalAbsolutePath("/Applications/Foo.app", "darwin"));
  assert.ok(isLocalAbsolutePath("/usr/bin/foo", "linux"));
  assert.ok(!isLocalAbsolutePath("//server/share/foo", "linux"));
  assert.ok(!isLocalAbsolutePath("usr/bin/foo", "linux"));
});

test("rule kinds and display names follow the per-OS forms", () => {
  assert.equal(ruleKind("C:\\Games\\Foo\\", "win32"), "dir");
  assert.equal(ruleKind("C:\\Games\\Foo\\foo.exe", "win32"), "file");
  assert.equal(ruleKind("/Applications/Foo.app", "darwin"), "bundle");
  assert.equal(ruleKind("/opt/foo/", "darwin"), "dir");
  assert.equal(ruleKind("/opt/foo/", "linux"), "dir");
  assert.equal(ruleKind("/usr/bin/foo", "linux"), "file");
  assert.equal(displayNameForRule("C:\\Games\\Foo\\", "win32"), "Foo");
  assert.equal(displayNameForRule("C:\\Tools\\Bar.EXE", "win32"), "Bar");
  assert.equal(displayNameForRule("/Applications/Foo Bar.app", "darwin"), "Foo Bar");
  assert.equal(displayNameForRule("/snap/firefox/", "linux"), "firefox");
});

test("dedupe never merges unsupported rows and keeps ids unique", () => {
  const unsupported: CatalogCandidate[] = [
    { id: "desktop:a.desktop", name: "A", rule: "", exe: "/a.AppImage", kind: "file", unsupported: "appimage" },
    { id: "desktop:b.desktop", name: "B", rule: "", exe: "/b.AppImage", kind: "file", unsupported: "appimage" }
  ];
  assert.equal(dedupeCandidates(unsupported, "linux").length, 2);
});

test("settleWithin waits for a running scan, but never past its limit and never rejects", async () => {
  let finish = (_rows: string[]) => {};
  const scan = new Promise<string[]>((resolve) => (finish = resolve));
  let settled = false;
  const waiting = settleWithin(scan, 5000).then(() => (settled = true));
  await new Promise((resolve) => setTimeout(resolve, 20));
  assert.equal(settled, false);
  finish(["row"]);
  await waiting;

  const start = Date.now();
  await settleWithin(new Promise(() => {}), 50);
  assert.ok(Date.now() - start >= 45);
  await settleWithin(Promise.reject(new Error("EIO")), 5000);
});
