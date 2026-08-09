/**
 * Makes untrusted text (hub responses, daemon output, error messages) safe to
 * write to the console: a message containing CR/LF could otherwise forge extra
 * log lines, and raw control bytes can drive a terminal via escape sequences.
 */
export function sanitizeLog(value: unknown): string {
  const text = value instanceof Error ? value.message : String(value);
  // Drop CR/LF first, then blank the remaining control characters.
  // eslint-disable-next-line no-control-regex
  return text.replace(/[\r\n]/g, "").replace(/[\x00-\x1f\x7f]/g, " ");
}
