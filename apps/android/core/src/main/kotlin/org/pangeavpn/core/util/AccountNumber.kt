package org.pangeavpn.core.util

// Sign-in credential handling: a 24-char Crockford-base32 account number, or a
// legacy 16-digit token. The server decides validity — nothing here rejects input.

private const val GROUP_SIZE = 4

// ASCII-only, not isDigit(): that is Unicode-aware, so Arabic-Indic digits would
// take a different branch here than on desktop, where the check is /^\d*$/.
private fun isLegacyToken(value: String): Boolean = value.all { it in '0'..'9' }

/** Folds the characters Crockford omits, so a hand-copied number still matches. */
fun normalizeAccountNumber(value: String): String {
    val trimmed = value.trim()
    if (isLegacyToken(trimmed)) return trimmed
    return trimmed
        .filterNot { it.isWhitespace() || it == '-' }
        .uppercase()
        .map {
            when (it) {
                'O' -> '0'
                'I', 'L' -> '1'
                else -> it
            }
        }
        .joinToString("")
}

/** Display form for what is being typed: upper case, blocks of 4. */
fun formatAccountNumberInput(value: String): String {
    val trimmed = value.trim()
    if (isLegacyToken(trimmed)) return trimmed
    return trimmed
        .filterNot { it.isWhitespace() || it == '-' }
        .uppercase()
        .chunked(GROUP_SIZE)
        .joinToString("-")
}
