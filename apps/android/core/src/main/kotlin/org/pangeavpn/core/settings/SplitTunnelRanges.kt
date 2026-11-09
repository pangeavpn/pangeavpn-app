package org.pangeavpn.core.settings

sealed interface RangesParse {
    /** Normalized CIDRs, in the order typed, each once. */
    data class Ranges(val ranges: List<String>) : RangesParse

    /** Why the text was refused; entries are kept as typed, so the message can name them. */
    data class Refused(
        val notIPv4: List<String>,
        val prefixTooShort: List<String>,
        val tooMany: Boolean,
    ) : RangesParse
}

/** Excluded IPv4 ranges for split tunnelling, held to the rules desktop's service applies. */
object SplitTunnelRanges {
    const val MAX_ENTRIES = 32
    const val MIN_PREFIX = 8

    // Arabic, full-width and ideographic commas, as other keyboards type them.
    private const val SEPARATORS = ",;،，、"

    private sealed interface Entry {
        data class Network(val base: Long, val bits: Int) : Entry
        data object NotIPv4 : Entry
        data object PrefixTooShort : Entry
    }

    /** Empty text clears the list; a range typed twice counts once toward the limit. */
    fun parse(text: String): RangesParse {
        val checked = tokens(text).map { it to parsed(it) }
        val ranges = checked.mapNotNull { (_, entry) -> (entry as? Entry.Network)?.let(::cidr) }.distinct()
        val notIPv4 = checked.filter { it.second == Entry.NotIPv4 }.map { it.first }.distinct()
        val prefixTooShort = checked.filter { it.second == Entry.PrefixTooShort }.map { it.first }.distinct()
        val tooMany = ranges.size > MAX_ENTRIES
        if (notIPv4.isEmpty() && prefixTooShort.isEmpty() && !tooMany) return RangesParse.Ranges(ranges)
        return RangesParse.Refused(notIPv4, prefixTooShort, tooMany)
    }

    /** A stored range as a network address and prefix length, for the tunnel's excluded routes. */
    fun route(token: String): Pair<String, Int>? =
        (parsed(token) as? Entry.Network)?.let { dottedQuad(it.base) to it.bits }

    /** What the field shows for a stored list. */
    fun text(ranges: List<String>): String = ranges.joinToString(", ")

    fun tokens(text: String): List<String> = text.fields { it.isWhitespace() || it in SEPARATORS }

    // A bare address becomes a /32 and host bits are cleared, so equal ranges compare equal.
    private fun parsed(token: String): Entry {
        val parts = SettingsInput.foldDigits(token).split('/')
        if (parts.size > 2) return Entry.NotIPv4
        val base = address(parts[0]) ?: return Entry.NotIPv4
        val bits = if (parts.size == 2) number(parts[1], maxDigits = 2) else 32
        if (bits == null || bits > 32) return Entry.NotIPv4
        if (bits < MIN_PREFIX) return Entry.PrefixTooShort
        return Entry.Network(base and prefixMask(bits), bits)
    }

    private fun cidr(network: Entry.Network): String = "${dottedQuad(network.base)}/${network.bits}"

    private fun address(text: String): Long? {
        val octets = text.split('.')
        if (octets.size != 4) return null
        var value = 0L
        for (octet in octets) {
            val number = number(octet, maxDigits = 3)?.takeIf { it <= 255 } ?: return null
            value = (value shl 8) or number.toLong()
        }
        return value
    }

    // Leading zeros are refused, as Go's netip parser refuses them.
    private fun number(text: String, maxDigits: Int): Int? {
        if (text.isEmpty() || text.length > maxDigits || !text.all { it in '0'..'9' }) return null
        if (text != "0" && text.startsWith('0')) return null
        return text.toInt()
    }
}
