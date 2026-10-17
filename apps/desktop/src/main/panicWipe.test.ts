import assert from "node:assert/strict";
import test from "node:test";
import {
  PANIC_DOUBLE_PRESS_WINDOW_MS,
  PANIC_WIPE_ACCELERATOR,
  deferredUserCleanupCommand,
  performPanicWipe,
  userStatePathsToRemove,
  type PanicPhase,
  type PanicWipeContext
} from "./panicWipe.ts";

function ctx(overrides: Partial<PanicWipeContext> = {}): PanicWipeContext {
  return {
    platform: "win32",
    isPackaged: true,
    appPid: 4242,
    homeDir: "C:\\Users\\alice",
    appDataDir: "C:\\Users\\alice\\AppData\\Roaming",
    userDataDir: "C:\\Users\\alice\\AppData\\Roaming\\PangeaVPN",
    logsDir: "C:\\Users\\alice\\AppData\\Roaming\\PangeaVPN\\logs",
    userStateDir: "C:\\Users\\alice\\AppData\\Roaming\\pangeavpn-desktop",
    ...overrides
  };
}

function decodePs(b64: string): string {
  return Buffer.from(b64, "base64").toString("utf16le");
}

test("the panic accelerator is a hard-to-press, hardcoded chord", () => {
  assert.equal(PANIC_WIPE_ACCELERATOR, "CommandOrControl+Alt+Shift+F12");
  for (const mod of ["CommandOrControl", "Alt", "Shift"]) {
    assert.ok(PANIC_WIPE_ACCELERATOR.includes(mod), `missing modifier ${mod}`);
  }
  assert.equal(PANIC_DOUBLE_PRESS_WINDOW_MS, 2000);
});

test("user-state paths cover our own state plus Electron's per-user data", () => {
  const paths = userStatePathsToRemove(ctx());
  assert.ok(paths.includes("C:\\Users\\alice\\AppData\\Roaming\\pangeavpn-desktop"));
  assert.ok(paths.includes("C:\\Users\\alice\\AppData\\Roaming\\PangeaVPN"));
  assert.equal(new Set(paths).size, paths.length, "paths must be deduped");
});

test("macOS user-state paths include caches, prefs, and saved state", () => {
  const paths = userStatePathsToRemove(ctx({ platform: "darwin", homeDir: "/Users/alice" }));
  assert.ok(paths.some((p) => p.endsWith("/Library/Caches/com.pangea.pangeavpn")));
  assert.ok(paths.some((p) => p.endsWith("/Library/Preferences/com.pangea.pangeavpn.plist")));
});

test("windows deferred cleanup waits for the app, then purges user state (unelevated)", () => {
  const command = deferredUserCleanupCommand(ctx());
  assert.ok(command);
  assert.equal(command.file, "powershell.exe");
  const script = decodePs(command.args[command.args.indexOf("-EncodedCommand") + 1]);
  assert.doesNotMatch(script, /RunAs/, "deferred user cleanup must not request elevation");
  assert.match(script, /Wait-Process -Id 4242/);
  assert.match(script, /pangeavpn-desktop/);
});

test("posix deferred cleanup uses /bin/sh and waits on the pid", () => {
  const command = deferredUserCleanupCommand(ctx({ platform: "darwin", homeDir: "/Users/alice" }));
  assert.ok(command);
  assert.equal(command.file, "/bin/sh");
  assert.match(command.args[1], /kill -0 4242/);
  assert.match(command.args[1], /rm -rf/);
});

function recordingActions() {
  const calls: string[] = [];
  const removed: string[] = [];
  const phases: PanicPhase[] = [];
  const uninstalled: number[] = [];
  let spawned: { file: string; args: string[] } | null = null;
  return {
    calls,
    removed,
    phases,
    uninstalled,
    get spawned() {
      return spawned;
    },
    actions: {
      lowerLock: async () => {
        calls.push("lowerLock");
      },
      requestDaemonUninstall: async (appPid: number) => {
        uninstalled.push(appPid);
        calls.push("requestDaemonUninstall");
      },
      stopDaemonProcess: () => {
        calls.push("stopDaemonProcess");
      },
      removeLoginItem: async () => {
        calls.push("removeLoginItem");
      },
      removePath: async (target: string) => {
        removed.push(target);
        calls.push(`removePath:${target}`);
      },
      spawnDetached: (file: string, args: string[]) => {
        spawned = { file, args };
        calls.push("spawnDetached");
      },
      notify: (phase: PanicPhase) => {
        phases.push(phase);
      },
      quit: () => {
        calls.push("quit");
      },
      logf: () => {}
    }
  };
}

test("performPanicWipe lowers the lock, asks the daemon to uninstall, and quits last", async () => {
  const rec = recordingActions();
  await performPanicWipe(ctx(), rec.actions);
  assert.deepEqual(rec.phases, ["start", "done"]);
  assert.equal(rec.calls[0], "lowerLock");
  assert.equal(rec.calls.at(-1), "quit");
  assert.deepEqual(rec.uninstalled, [4242]);
  assert.ok(rec.calls.indexOf("requestDaemonUninstall") < rec.calls.indexOf("quit"));
  assert.deepEqual(rec.removed, userStatePathsToRemove(ctx()));
  assert.equal(rec.spawned?.file, "powershell.exe");
  assert.ok(rec.calls.includes("removeLoginItem"));
});

test("the daemon is asked to uninstall before the user dirs (and any token in them) are removed", async () => {
  const rec = recordingActions();
  await performPanicWipe(ctx(), rec.actions);
  const firstRemoval = rec.calls.findIndex((call) => call.startsWith("removePath:"));
  assert.ok(firstRemoval > 0);
  assert.ok(rec.calls.indexOf("requestDaemonUninstall") < firstRemoval);
});

test("performPanicWipe stays local when unpackaged (no daemon uninstall, no cleanup spawn)", async () => {
  const rec = recordingActions();
  await performPanicWipe(ctx({ isPackaged: false }), rec.actions);
  assert.ok(!rec.calls.includes("requestDaemonUninstall"));
  assert.ok(!rec.calls.includes("spawnDetached"));
  assert.equal(rec.spawned, null);
  assert.ok(rec.calls.includes("quit"));
});

test("a failed daemon uninstall still finishes the wipe and quits", async () => {
  const rec = recordingActions();
  const actions = {
    ...rec.actions,
    requestDaemonUninstall: async () => {
      throw new Error("daemon unreachable");
    }
  };
  await performPanicWipe(ctx(), actions);
  assert.ok(rec.calls.includes("spawnDetached"));
  assert.ok(rec.calls.includes("quit"));
  assert.deepEqual(rec.phases, ["start", "error"], "a failed removal must not be reported as done");
});

test("a failing removePath does not abort the wipe", async () => {
  const rec = recordingActions();
  const actions = {
    ...rec.actions,
    removePath: async (target: string) => {
      if (target.includes("PangeaVPN")) throw new Error("EBUSY");
      rec.removed.push(target);
    }
  };
  await performPanicWipe(ctx(), actions);
  assert.ok(rec.calls.includes("quit"));
  assert.ok(rec.calls.includes("requestDaemonUninstall"));
});
