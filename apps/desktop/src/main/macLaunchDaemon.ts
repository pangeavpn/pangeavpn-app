// Shell for the elevated restart of the installed macOS LaunchDaemon. Kept free of
// Electron imports so it can be unit-tested.

// kickstart fails with "Could not find service (113)" once launchd drops the job, so
// re-register it first; enable precedes bootstrap, which a disabled service refuses.
export function macLaunchDaemonRestartCommand(label: string, plistPath: string): string {
  const service = `system/${label}`;
  return [
    `/bin/launchctl print ${service} >/dev/null 2>&1 || { /bin/launchctl enable ${service}; /bin/launchctl bootstrap system ${plistPath}; }`,
    `/bin/launchctl kickstart -k ${service}`
  ].join("; ");
}

// Lets the account that asked read the daemon's token. Only joins a group the pkg
// created, so an install without one keeps its admin-only token.
export function macAccessGroupJoinCommand(user: string): string {
  const quotedUser = `'${user.replace(/'/g, "'\\''")}'`;
  return [
    `/usr/sbin/dseditgroup -o edit -a ${quotedUser} -t user pangeavpn >/dev/null 2>&1 || true`,
    "/usr/bin/dsmemberutil flushcache >/dev/null 2>&1 || true"
  ].join("; ");
}
