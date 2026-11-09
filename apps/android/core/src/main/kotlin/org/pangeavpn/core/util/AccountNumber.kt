package org.pangeavpn.core.util

// The sign-in credential: a 24-character account number, or a legacy 16-digit token.
// The hub decides what is valid; nothing here rejects input.

private const val GROUP_SIZE = 4

const val ACCOUNT_NUMBER_MAX_SYMBOLS = 64

// ASCII only, so Arabic-Indic digits are not mistaken for a legacy token.
private fun isLegacyToken(value: String): Boolean = value.all { it in '0'..'9' }

private fun stripped(value: String): String = value.filterNot { it.isWhitespace() || it == '-' }.uppercase()

/** What the field lets through: ASCII letters, digits and dashes, up to 64 symbols not counting dashes. */
fun sanitizeAccountNumberInput(raw: String): String {
    var symbols = 0
    val out = StringBuilder()
    for (c in raw) {
        if (c != '-') {
            if (c !in '0'..'9' && c !in 'A'..'Z' && c !in 'a'..'z') continue
            if (symbols == ACCOUNT_NUMBER_MAX_SYMBOLS) break
            symbols++
        }
        out.append(c)
    }
    return out.toString()
}

/** What is sent to the hub: folds the characters Crockford omits, so a hand-copied number still matches. */
fun normalizeAccountNumber(value: String): String {
    val trimmed = value.trim()
    if (isLegacyToken(trimmed)) return trimmed
    return stripped(trimmed)
        .map {
            when (it) {
                'O' -> '0'
                'I', 'L' -> '1'
                else -> it
            }
        }
        .joinToString("")
}

/** Display form for what is being typed or revealed: upper case, blocks of 4. */
fun formatAccountNumberInput(value: String): String {
    val trimmed = value.trim()
    if (isLegacyToken(trimmed)) return trimmed
    return stripped(trimmed).chunked(GROUP_SIZE).joinToString("-")
}
