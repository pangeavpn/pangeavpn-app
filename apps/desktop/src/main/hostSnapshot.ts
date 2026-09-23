import { execFile } from "node:child_process";

const HOST_SNAPSHOT_TIMEOUT_MS = 20_000;
const HOST_SNAPSHOT_MAX_BYTES = 256 * 1024;

const SOFTWARE_PATTERN =
  "vpn|nord|mullvad|proton|express|surfshark|cyberghost|windscribe|tunnelbear|privateinternet|zscaler|umbrella|" +
  "cloudflare|warp|adguard|nextdns|eset|kaspersky|norton|mcafee|bitdefender|avast|avg|malwarebytes|sophos|" +
  "crowdstrike|sentinel|webroot|trend|wireguard|openvpn|wintun|pangea|killer|rivet";

// What the daemon cannot see from inside the tunnel: the security and VPN
// software beside it, the adapters it competes with, and the firewall's stance.
const SCRIPT = [
  "$ErrorActionPreference = 'SilentlyContinue'",
  "'## security products'",
  "Get-CimInstance -Namespace root/SecurityCenter2 -ClassName AntiVirusProduct | ForEach-Object { 'av | ' + $_.displayName + ' | state=' + $_.productState }",
  "Get-CimInstance -Namespace root/SecurityCenter2 -ClassName FirewallProduct | ForEach-Object { 'fw | ' + $_.displayName + ' | state=' + $_.productState }",
  "'## services'",
  `Get-Service | Where-Object { $_.Name -match '${SOFTWARE_PATTERN}' -or $_.DisplayName -match '${SOFTWARE_PATTERN}' } | ForEach-Object { [string]$_.Status + ' | ' + $_.Name + ' | ' + $_.DisplayName }`,
  "'## adapters'",
  "Get-NetAdapter -IncludeHidden | Where-Object { $_.InterfaceDescription -match 'wintun|wireguard|tap|tun|vpn|pangea' -or $_.Name -match 'pangea|vpn' } | ForEach-Object { [string]$_.Status + ' | ifIndex=' + $_.ifIndex + ' | ' + $_.Name + ' | ' + $_.InterfaceDescription }",
  "'## firewall profiles'",
  "Get-NetFirewallProfile | ForEach-Object { $_.Name + ' | enabled=' + $_.Enabled + ' | in=' + $_.DefaultInboundAction + ' | out=' + $_.DefaultOutboundAction }"
].join("\n");

function encodedCommand(script: string): string {
  return Buffer.from(script, "utf16le").toString("base64");
}

/** Runs unelevated: everything it reads is open to a standard user. */
export function collectWindowsHostSnapshot(): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(
      "powershell.exe",
      ["-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodedCommand(SCRIPT)],
      { timeout: HOST_SNAPSHOT_TIMEOUT_MS, windowsHide: true, maxBuffer: HOST_SNAPSHOT_MAX_BYTES },
      (error, stdout, stderr) => {
        const text = String(stdout ?? "").trim();
        if (error && text.length === 0) {
          reject(error);
          return;
        }
        const detail = String(stderr ?? "").trim();
        resolve(text.length > 0 ? text : `<empty>${detail ? `\n${detail}` : ""}`);
      }
    );
  });
}
