package org.pangeavpn.core.settings

/** What settings fields do to typed text. Persian and Arabic keyboards type their own digits. */
object SettingsInput {
    /** Keeps the digits only, as ASCII ones. */
    fun asciiDigits(text: String): String =
        buildString { for (char in text) char.digitToIntOrNull()?.let { append(it) } }

    /** Turns any script's digits into ASCII ones and leaves everything else alone. */
    fun foldDigits(text: String): String =
        buildString { for (char in text) append(char.digitToIntOrNull()?.digitToChar() ?: char) }
}

internal fun String.fields(isSeparator: (Char) -> Boolean): List<String> {
    val fields = mutableListOf<String>()
    val current = StringBuilder()
    for (char in this) {
        if (!isSeparator(char)) {
            current.append(char)
        } else if (current.isNotEmpty()) {
            fields += current.toString()
            current.clear()
        }
    }
    if (current.isNotEmpty()) fields += current.toString()
    return fields
}

internal fun dottedQuad(address: Long): String =
    listOf(24, 16, 8, 0).joinToString(".") { shift -> ((address shr shift) and 0xFF).toString() }

internal fun prefixMask(bits: Int): Long = (0xFFFFFFFFL shl (32 - bits)) and 0xFFFFFFFFL
