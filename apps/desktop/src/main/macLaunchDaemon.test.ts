import assert from "node:assert/strict";
import test from "node:test";
import { macLaunchDaemonRestartCommand } from "./macLaunchDaemon.ts";

const label = "com.pangea.pangeavpn.daemon";
const plist = "/Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist";

test("re-registers a dropped service before kickstarting it", () => {
  const command = macLaunchDaemonRestartCommand(label, plist);
  assert.equal(
    command,
    "/bin/launchctl print system/com.pangea.pangeavpn.daemon >/dev/null 2>&1 || " +
      "{ /bin/launchctl enable system/com.pangea.pangeavpn.daemon; " +
      "/bin/launchctl bootstrap system /Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist; }; " +
      "/bin/launchctl kickstart -k system/com.pangea.pangeavpn.daemon"
  );
});

test("enable comes before bootstrap, and kickstart runs last so its status is the result", () => {
  const command = macLaunchDaemonRestartCommand(label, plist);
  assert.ok(command.indexOf("enable") < command.indexOf("bootstrap"));
  assert.ok(command.trimEnd().endsWith(`kickstart -k system/${label}`));
});
