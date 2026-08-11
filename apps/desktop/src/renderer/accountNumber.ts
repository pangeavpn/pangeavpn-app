// Sign-in credential handling: a 24-char Crockford-base32 account number, or a
// legacy 16-digit token. The server decides validity — nothing here rejects input.

const GROUP_SIZE = 4;

/** A pure-digit entry is a legacy token; leave it exactly as the user typed it. */
function isLegacyToken(value: string): boolean {
  return /^\d*$/.test(value);
}

/** Folds the characters Crockford omits, so a hand-copied number still matches. */
export function normalizeAccountNumber(value: string): string {
  const trimmed = value.trim();
  if (isLegacyToken(trimmed)) return trimmed;
  return trimmed
    .replace(/[\s-]/g, "")
    .toUpperCase()
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1");
}

/** Display form for what is being typed: upper case, blocks of 4. */
export function formatAccountNumberInput(value: string): string {
  const trimmed = value.trim();
  if (isLegacyToken(trimmed)) return trimmed;
  const compact = trimmed.replace(/[\s-]/g, "").toUpperCase();
  return compact.replace(new RegExp(`(.{${GROUP_SIZE}})(?=.)`, "g"), "$1-");
}
