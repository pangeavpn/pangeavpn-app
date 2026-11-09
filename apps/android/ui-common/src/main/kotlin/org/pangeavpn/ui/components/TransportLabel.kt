package org.pangeavpn.ui.components

// Product names, so they read the same in every language, as on desktop and iOS.
private val TRANSPORT_NAMES = mapOf(
    "cloak" to "Cloak",
    "reality" to "VLESS+REALITY",
    "shadowsocks" to "Shadowsocks",
    "hysteria2" to "Hysteria2",
    "anytls" to "AnyTLS",
    "snowflake" to "Snowflake",
    "naive" to "NaiveProxy",
    "wireguard" to "WireGuard",
)

/** The transport carrying the live tunnel, for the Via fact; empty when there is none to name. */
fun transportStatusLabel(activeTransport: String?): String =
    activeTransport?.let { TRANSPORT_NAMES[it] }.orEmpty()
