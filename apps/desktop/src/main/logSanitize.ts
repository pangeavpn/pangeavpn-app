/**
 * Makes untrusted text (hub responses, daemon output, error messages) safe to
 * write to the console: a message containing CR/LF could otherwise forge extra
 * log lines, and raw control bytes can drive a terminal via escape sequences.
 */
export function sanitizeLog(value: unknown): string {
  const text = value instanceof Error ? value.message : safeString(value);
  // Drop CR/LF first, then blank the remaining control characters.
  // eslint-disable-next-line no-control-regex
  const stripped = text.replace(/[\r\n]/g, "").replace(/[\x00-\x1f\x7f]/g, " ");
  return redactSecrets(stripped);
}

// Called mostly from catch handlers, so a throwing coercion must not escape.
function safeString(value: unknown): string {
  try {
    return String(value);
  } catch {
    return "[unstringifiable value]";
  }
}

const SECRET_PATTERNS: ReadonlyArray<readonly [RegExp, string]> = [
  [/\bBearer\s+[A-Za-z0-9\-_.]+/gi, "Bearer [redacted]"],
  [/(x-license-key["']?\s*[:=]\s*["']?)[^\s"',}]+/gi, "$1[redacted]"],
  [/\b[0-9a-f]{64}\b/gi, "[redacted]"],
  [/\b[A-Za-z0-9+/]{43}=(?![A-Za-z0-9+/=])/g, "[redacted]"],
  [/("(?:password|token|secret|api[_-]?key|license[_-]?key|uuid)"\s*:\s*")[^"]*(")/gi, "$1[redacted]$2"]
];

// Blanks credential shapes (bearer tokens, license keys, daemon/WireGuard keys, JSON secret fields).
function redactSecrets(text: string): string {
  return SECRET_PATTERNS.reduce((acc, [pattern, replacement]) => acc.replace(pattern, replacement), text);
}
