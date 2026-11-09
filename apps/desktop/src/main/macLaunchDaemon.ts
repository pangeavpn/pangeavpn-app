// Shell for the elevated restart of the installed macOS LaunchDaemon. Kept free of
// Electron imports so it can be unit-tested.

/**
 * kickstart alone fails with "Could not find service … (113)" whenever launchd has
 * dropped the job while its plist is still on disk (booted out by an interrupted
 * update, an uninstall that didn't finish, or by hand). So re-register it first if
 * it isn't loaded, the same way the pkg postinstall does: enable before bootstrap,
 * because a disabled service refuses to bootstrap. The exit status is kickstart's.
 */
export function macLaunchDaemonRestartCommand(label: string, plistPath: string): string {
  const service = `system/${label}`;
  return [
    `/bin/launchctl print ${service} >/dev/null 2>&1 || { /bin/launchctl enable ${service}; /bin/launchctl bootstrap system ${plistPath}; }`,
    `/bin/launchctl kickstart -k ${service}`
  ].join("; ");
}
