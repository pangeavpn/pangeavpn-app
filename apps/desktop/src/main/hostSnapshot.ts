import { execFile } from "node:child_process";
import { collectSniProbe, type SniProbeTarget } from "./sniProbe.ts";

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
  "'## system'",
  "$os = Get-CimInstance Win32_OperatingSystem; 'system | ' + $os.Caption + ' | build=' + $os.BuildNumber + ' | boot=' + $os.LastBootUpTime.ToUniversalTime().ToString('o') + ' | uptimeMin=' + [int]((Get-Date) - $os.LastBootUpTime).TotalMinutes",
  "Get-TimeZone | ForEach-Object { 'timezone | ' + $_.Id + ' | utcOffsetMin=' + [int]$_.BaseUtcOffset.TotalMinutes }",
  // The service's own restarts (a crash, an upgrade install) are the one event
  // an otherwise healthy report cannot show from inside the app.
  "'## daemon service events'",
  "Get-WinEvent -FilterHashtable @{LogName='System'; ProviderName='Service Control Manager'; StartTime=(Get-Date).AddDays(-14)} | Where-Object { $_.Message -match 'Pangea' } | Select-Object -First 8 | ForEach-Object { 'svc | ' + $_.TimeCreated.ToUniversalTime().ToString('o') + ' | ' + $_.Id + ' | ' + (($_.Message -split \"`r?`n\")[0]) }",
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

// macOS equivalent: the VPN/security/TLS-interception software the daemon cannot
// see from inside the tunnel. No bare "tun"/"tap" here — ifconfig lists adapters
// already, and those words make the process match too broad.
export const DARWIN_SOFTWARE_PATTERN =
  "vpn|nord|mullvad|proton|express|surfshark|cyberghost|windscribe|tunnelbear|privateinternet|" +
  "zscaler|netskope|umbrella|anyconnect|globalprotect|forcepoint|bluecoat|broadcom|symantec|" +
  "forti|paloalto|checkpoint|sophos|crowdstrike|sentinel|cylance|carbonblack|cloudflare|warp|" +
  "adguard|nextdns|eset|kaspersky|norton|mcafee|bitdefender|avast|malwarebytes|webroot|trend|" +
  "jamf|intune|santa|littlesnitch|lulu|murus|wireguard|openvpn|pangea";

// Each line labels its probe so a report reader can scan sections. The default
// route's interface + address, the resolver, any system proxy/PAC, and a
// filtered pass over processes and the system-trust CAs together show whether a
// filter or TLS interceptor (e.g. Zscaler) sits in front of this Mac. The
// diagnostics redactor keeps RFC1918/link-local addresses, so a VPN client's
// private addresses survive while real endpoints are stripped.
const DARWIN_SCRIPT = [
  "echo '## os'; sw_vers 2>/dev/null",
  "echo '## interfaces'; ifconfig 2>/dev/null",
  "echo '## default route'; route -n get default 2>/dev/null",
  "echo '## routing table (inet)'; netstat -rn -f inet 2>/dev/null | head -n 40",
  "echo '## dns'; scutil --dns 2>/dev/null | grep -Ei 'nameserver|if_index|reach|domain' | head -n 40",
  // Does outbound UDP work at all, and does QUIC on 443 specifically? A filter
  // that blocks UDP/443 (common with Zscaler) stops hysteria2/tuic while leaving
  // DNS/UDP53 alone, so the two lines together localise the block.
  "echo '## udp egress'; " +
    "if command -v dig >/dev/null 2>&1; then " +
    "if [ -n \"$(dig +time=2 +tries=1 +short @1.1.1.1 cloudflare.com A 2>/dev/null)\" ]; then echo 'udp/53 to 1.1.1.1: ok'; else echo 'udp/53 to 1.1.1.1: no reply (blocked or filtered)'; fi; " +
    "else echo 'udp/53: dig unavailable'; fi; " +
    "if curl --http3-only -s -o /dev/null -m 4 https://cloudflare.com 2>/dev/null; then echo 'quic udp/443 to cloudflare: ok'; " +
    "else echo 'quic udp/443 to cloudflare: failed (blocked, or this curl lacks http3)'; fi",
  "echo '## system proxies'; scutil --proxy 2>/dev/null",
  "echo '## network services'; networksetup -listnetworkserviceorder 2>/dev/null",
  `echo '## interceptor/security software (processes)'; ps -axo comm= 2>/dev/null | grep -Ei '${DARWIN_SOFTWARE_PATTERN}' | sort -u | head -n 60`,
  `echo '## system-trust CAs matching known interceptors'; security find-certificate -a /Library/Keychains/System.keychain 2>/dev/null | grep -i labl | grep -Ei '${DARWIN_SOFTWARE_PATTERN}' | sort -u | head -n 40`
].join("\n");

/** Runs unelevated: reads only what a standard user may, and never a private key
 *  (find-certificate on the System keychain lists cert labels, not secrets). */
export function collectDarwinHostSnapshot(
  targets: readonly SniProbeTarget[] = [],
  snis: readonly string[] = []
): Promise<string> {
  return Promise.all([runDarwinSnapshot(), ...targets.map((target) => collectSniProbe(target, snis))])
    .then(([snapshot, ...probes]) => [snapshot, ...probes].filter(Boolean).join("\n"));
}

function runDarwinSnapshot(): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(
      "/bin/sh",
      ["-c", DARWIN_SCRIPT],
      { timeout: HOST_SNAPSHOT_TIMEOUT_MS, maxBuffer: HOST_SNAPSHOT_MAX_BYTES },
      (error, stdout, stderr) => {
        // A trailing grep with no match exits non-zero though the sections above
        // it printed, so trust stdout and only surface error when nothing came.
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
