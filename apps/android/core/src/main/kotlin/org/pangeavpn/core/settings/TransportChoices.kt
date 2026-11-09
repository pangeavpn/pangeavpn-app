package org.pangeavpn.core.settings

object TransportChoices {
    /** Desktop's picker order, which is the order automatic mode walks. */
    val pickerOrder = listOf("auto", "reality", "cloak", "shadowsocks", "hysteria2", "anytls")

    /** A stored choice with no row of its own (naive, wireguard) reads as automatic. */
    fun selected(preferredTransport: String): String =
        if (preferredTransport in pickerOrder) preferredTransport else "auto"
}
