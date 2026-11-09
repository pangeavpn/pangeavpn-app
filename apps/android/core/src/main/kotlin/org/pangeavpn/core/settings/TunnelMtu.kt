package org.pangeavpn.core.settings

/** The core quietly swaps an out-of-range MTU for its default, so the page refuses it first. */
object TunnelMtu {
    val range = 1280..1420
    const val DEFAULT = 1380

    /** Null unless the text is a whole number in range, written in ASCII digits. */
    fun parse(text: String): Int? {
        val trimmed = text.trim()
        if (trimmed.isEmpty() || !trimmed.all { it in '0'..'9' }) return null
        return trimmed.toIntOrNull()?.takeIf { it in range }
    }
}
