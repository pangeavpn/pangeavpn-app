package org.pangeavpn.core.settings

/** Desktop's DNS provider choices; automatic means no override, custom a typed list. */
enum class DnsChoice { AUTOMATIC, ADGUARD, CLOUDFLARE, QUAD9, CLOUDFLARE_FAMILY, GOOGLE, CUSTOM }

object DnsPresets {
    /** Null for custom, which has no fixed servers. */
    fun servers(choice: DnsChoice): List<String>? = when (choice) {
        DnsChoice.AUTOMATIC -> emptyList()
        DnsChoice.ADGUARD -> listOf("94.140.14.14", "94.140.15.15")
        DnsChoice.CLOUDFLARE -> listOf("1.1.1.1", "1.0.0.1")
        DnsChoice.QUAD9 -> listOf("9.9.9.9", "149.112.112.112")
        DnsChoice.CLOUDFLARE_FAMILY -> listOf("1.1.1.3", "1.0.0.3")
        DnsChoice.GOOGLE -> listOf("8.8.8.8", "8.8.4.4")
        DnsChoice.CUSTOM -> null
    }

    /** Exact match, order included, so a reordered preset reads as custom just as on desktop. */
    fun choice(stored: List<String>): DnsChoice {
        if (stored.isEmpty()) return DnsChoice.AUTOMATIC
        return DnsChoice.entries.firstOrNull { it != DnsChoice.AUTOMATIC && servers(it) == stored }
            ?: DnsChoice.CUSTOM
    }

    /** Null when any entry is invalid: the core would drop it silently. Blank is the VPN default. */
    fun normalize(text: String): List<String>? {
        val servers = mutableListOf<String>()
        for (part in typed(text).fields { it.isWhitespace() || it == ',' }) {
            val address = ipv4(part)?.takeUnless(::unroutable) ?: return null
            val literal = dottedQuad(address)
            if (literal !in servers) servers += literal
        }
        return servers
    }

    /** What the Custom DNS field shows as it is typed: local digits become ASCII ones. */
    fun typed(text: String): String = SettingsInput.foldDigits(text)

    // Unspecified, LAN, CGNAT, link-local, multicast and broadcast, plus loopback:
    // the Android core accepts it, but VpnService.Builder.addDnsServer refuses it and the connect fails.
    private val blocked = listOf(
        0x00000000L to 8, 0x0A000000L to 8, 0x64400000L to 10, 0xA9FE0000L to 16,
        0xAC100000L to 12, 0xC0A80000L to 16, 0xE0000000L to 3, 0x7F000000L to 8,
    )

    private fun ipv4(text: String): Long? {
        val octets = text.split('.')
        if (octets.size != 4) return null
        var value = 0L
        for (octet in octets) {
            if (octet.length !in 1..3 || !octet.all { it in '0'..'9' }) return null
            val number = octet.toInt()
            if (number > 255) return null
            value = (value shl 8) or number.toLong()
        }
        return value
    }

    private fun unroutable(address: Long): Boolean =
        blocked.any { (base, bits) -> (address and prefixMask(bits)) == base }
}
