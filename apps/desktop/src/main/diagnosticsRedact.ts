/** Strips identifying values out of log text bound for the hub. Stronger than
 *  logSanitize, which only guards the console against forged lines. */

import os from "node:os";

const IPV4 = /(?<![\d.])(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?![\d.])/g;
const IPV6_FULL = /(?<![0-9A-Za-z:])(?:[0-9A-Fa-f]{1,4}:){7}[0-9A-Fa-f]{1,4}(?![0-9A-Za-z:])/g;
const IPV6_COMPRESSED =
  /(?<![0-9A-Za-z:])(?:[0-9A-Fa-f]{1,4})?(?::[0-9A-Fa-f]{1,4}){0,6}::(?:[0-9A-Fa-f]{1,4})?(?::[0-9A-Fa-f]{1,4}){0,6}(?![0-9A-Za-z:])/g;

function octets(value: string): number[] | null {
  const parts = value.split(".").map((part) => Number(part));
  if (parts.length !== 4 || parts.some((n) => !Number.isInteger(n) || n < 0 || n > 255)) return null;
  return parts;
}

/** Loopback, RFC1918, link-local and the unspecified/broadcast addresses. These
 *  say how the tunnel was wired and identify nobody, so they survive. */
export function isKeptIpv4(value: string): boolean {
  const parts = octets(value);
  if (!parts) return true;
  const [a, b] = parts;
  if (a === 127 || a === 10 || a === 0) return true;
  if (a === 172 && b >= 16 && b <= 31) return true;
  if (a === 192 && b === 168) return true;
  if (a === 169 && b === 254) return true;
  if (a === 255 && parts.every((n) => n === 255)) return true;
  return false;
}

export function isKeptIpv6(value: string): boolean {
  const lower = value.toLowerCase();
  if (lower === "::" || lower === "::1") return true;
  const head = lower.split(":", 1)[0] ?? "";
  if (head.length === 0) return false;
  const group = Number.parseInt(head, 16);
  if (Number.isNaN(group)) return false;
  if (group >= 0xfe80 && group <= 0xfebf) return true;
  if (group >= 0xfc00 && group <= 0xfdff) return true;
  return false;
}

function isLikelyKey(run: string): boolean {
  if (run.endsWith("=")) return run.length >= 24;
  if (run.length >= 43) return /[a-z]/.test(run) && /[A-Z]/.test(run) && /\d/.test(run);
  return run.length >= 32 && /[a-z]/.test(run) && /[A-Z]/.test(run) && /\d/.test(run);
}

type Rule = readonly [RegExp, string | ((match: string, ...rest: string[]) => string)];

// The separators also take the JSON-escaped (\\) and C:/Users spellings.
const WINDOWS_HOME =
  /((?:[A-Za-z]:|\\{1,2}Device\\{1,2}[^\\\s"]+)?(?:\\{1,2}|\/)(?:Users|Documents and Settings)(?:\\{1,2}|\/))[^\\/\r\n"':]+/gi;

// Order matters: the widest shapes run first so a narrower rule cannot bite a
// piece out of one and leave the rest readable.
const RULES: readonly Rule[] = [
  [WINDOWS_HOME, "$1[redacted:user]"],
  [/(\/home\/)[^/\s"':]+/g, "$1[redacted:user]"],
  [/(?<![\w.-])\/root(?=[/\s"':,;)\]}]|$)/gm, "/[redacted:user]"],
  [/[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g, "[redacted:email]"],
  [/\bBearer\s+[A-Za-z0-9\-_.=+/]+/gi, "Bearer [redacted:token]"],
  [/((?:x-)?license[-_ ]?key["']?\s*[:=]\s*["']?)[^\s"',}]+/gi, "$1[redacted:licenseKey]"],
  [
    /("(?:password|secret|token|api[_-]?key|license[_-]?key|private[_-]?key|preshared[_-]?key|public[_-]?key|account[_-]?number)"\s*:\s*")[^"]*(")/gi,
    "$1[redacted:secret]$2"
  ],
  [/\b((?:Private|Preshared|Public)Key\s*=\s*)\S+/g, "$1[redacted:key]"],
  [/\b[0-9a-f]{64}\b/gi, "[redacted:token]"],
  [
    /\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/gi,
    "[redacted:uuid]"
  ],
  [
    /(?<![A-Za-z0-9+/=])[A-Za-z0-9+/]{24,}={0,2}(?![A-Za-z0-9+/=])/g,
    (match: string) => (isLikelyKey(match) ? "[redacted:key]" : match)
  ],
  [/(?<![A-Za-z0-9-])[0-9A-Z]{4}(?:-[0-9A-Z]{4}){5}(?![A-Za-z0-9-])/g, "[redacted:accountNumber]"],
  [/(?<![A-Za-z0-9])[0-9A-HJKMNP-TV-Z]{24}(?![A-Za-z0-9])/g, "[redacted:accountNumber]"],
  [/(?<![A-Za-z0-9])\d{16}(?![A-Za-z0-9])/g, "[redacted:accountNumber]"],
  [IPV6_FULL, (match: string) => (isKeptIpv6(match) ? match : "[redacted:ipv6]")],
  [IPV6_COMPRESSED, (match: string) => (isKeptIpv6(match) ? match : "[redacted:ipv6]")],
  [IPV4, (match: string) => (isKeptIpv4(match) ? match : "[redacted:ipv4]")]
];

function currentHome(): string {
  try {
    return os.homedir();
  } catch {
    return "";
  }
}

// Catches profiles redirected outside Users/home, in raw, JSON-escaped and
// forward-slash spellings. Never the bare username: a short one would mangle prose.
function homeDirRule(homeDir: string): Rule | null {
  const home = homeDir.trim().replace(/[\\/]+$/, "");
  if (home.length < 4 || /^[A-Za-z]:$/.test(home)) return null;
  const forms = [...new Set([home.replace(/\\/g, "\\\\"), home, home.replace(/\\/g, "/")])];
  const escaped = forms.map((form) => form.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")).join("|");
  const flags = /^[A-Za-z]:|\\/.test(home) ? "gi" : "g";
  return [new RegExp(`(?:${escaped})(?=[\\\\/\\s"':,;)\\]}]|$)`, flags), "[redacted:user]"];
}

export function redactDiagnostics(text: string, homeDir: string = currentHome()): string {
  const home = homeDirRule(homeDir);
  return (home ? [home, ...RULES] : RULES).reduce<string>(
    (acc, [pattern, replacement]) =>
      typeof replacement === "string"
        ? acc.replace(pattern, replacement)
        : acc.replace(pattern, replacement as (substring: string, ...args: unknown[]) => string),
    text
  );
}
