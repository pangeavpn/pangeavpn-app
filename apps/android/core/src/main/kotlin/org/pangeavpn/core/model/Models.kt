package org.pangeavpn.core.model

import kotlinx.serialization.KSerializer
import kotlinx.serialization.Serializable
import kotlinx.serialization.descriptors.PrimitiveKind
import kotlinx.serialization.descriptors.PrimitiveSerialDescriptor
import kotlinx.serialization.descriptors.SerialDescriptor
import kotlinx.serialization.encoding.Decoder
import kotlinx.serialization.encoding.Encoder

enum class ConnState { DISCONNECTED, CONNECTING, CONNECTED, DISCONNECTING, ERROR }

private object ConnStateSerializer : KSerializer<ConnState> {
    override val descriptor: SerialDescriptor = PrimitiveSerialDescriptor("ConnState", PrimitiveKind.STRING)

    override fun serialize(encoder: Encoder, value: ConnState) = encoder.encodeString(value.name)

    override fun deserialize(decoder: Decoder): ConnState =
        runCatching { ConnState.valueOf(decoder.decodeString()) }.getOrDefault(ConnState.ERROR)
}

@Serializable
data class TunnelStatus(
    @Serializable(with = ConnStateSerializer::class) val state: ConnState = ConnState.DISCONNECTED,
    val detail: String = "",
    val bytesIn: Long = 0,
    val bytesOut: Long = 0,
    val serverId: String = "",
    val serverName: String = "",
    // Which cascade rung carried the live tunnel; empty when disconnected.
    val activeTransport: String = "",
    // Unix seconds of the newest WireGuard handshake, 0 before the first one.
    val lastHandshake: Long = 0,
    val postQuantum: Boolean = false,
    // Empty unless the route is multihop; serverId stays the exit.
    val entryServerId: String = "",
    val entryServerName: String = "",
    // The two below are set by PangeaVpnService, never by the core.
    val reconnecting: Boolean = false,
    // Wall-clock millis the session first came up; kept across switches and reconnects.
    val connectedSince: Long = 0,
)

@Serializable
data class Server(
    val id: String,
    val name: String,
    val region: String = "",
    val country: String = "",
    val load: Int? = null,
    // The server may relay as the entry of a two-hop route.
    val multihop: Boolean = false,
    val reality: Boolean = false,
    val hysteria2: Boolean = false,
    val shadowsocks: Boolean = false,
    val anytls: Boolean = false,
    val naive: Boolean = false,
    val snowflake: Boolean = false,
) {
    /** Cloak and plain WireGuard are on every node; the rest are advertised per server. */
    fun supports(transport: String): Boolean = when (transport) {
        "auto", "cloak", "wireguard" -> true
        "reality" -> reality
        "hysteria2" -> hysteria2
        "shadowsocks" -> shadowsocks
        "anytls" -> anytls
        "naive" -> naive
        "snowflake" -> snowflake
        else -> false
    }
}

@Serializable
data class Subscription(
    val status: String = "",
    val renews: Boolean = false,
    val expiresAt: String? = null,
    // null when the hub did not say; only an explicit false blocks connecting.
    val entitled: Boolean? = null,
    // Served from the last answer because the hub was unreachable.
    val cached: Boolean = false,
)

@Serializable
data class Device(
    val id: String,
    val friendlyName: String? = null,
    val createdAt: String = "",
    val status: String = "",
    val current: Boolean = false,
)

@Serializable
data class HubTestResult(
    val method: String = "",
    val ok: Boolean = false,
    val ms: Long = 0,
    val detail: String = "",
    // Why a method could not be tried at all: noAddress, noCredentials, noRelay or busy.
    val unavailable: String = "",
)

@Serializable
data class HubStatus(
    // The method this process last reached the hub by; null before the first request.
    val active: String? = null,
    val detail: String? = null,
)

@Serializable
data class Session(
    val email: String = "",
    val name: String = "",
    val servers: List<Server> = emptyList(),
    /** True when the core served the list from its store because the hub was unreachable. */
    val cached: Boolean = false,
)

/** Mirrors hubMethods in daemon/mobile/hubmethods.go. */
@Serializable
data class HubMethods(
    val directIp: Boolean = true,
    val reality: Boolean = true,
    val shadowsocks: Boolean = true,
    val fronted: Boolean = true,
    val normal: Boolean = false,
    val rev: Int = 2,
)

/** Mirrors the Go config blob in daemon/mobile/config.go. */
@Serializable
data class AppSettings(
    val preferredTransport: String = "auto",
    val customDns: List<String> = emptyList(),
    val mtu: Int = 1380,
    val allowLan: Boolean = false,
    val autoConnect: Boolean = false,
    val lastServerId: String = "",
    val hubMethods: HubMethods = HubMethods(),
    val deadDrop: Boolean = true,
    val postQuantum: Boolean = false,
)

/** Manual choices. NaiveProxy is reachable through "auto" but is not offered
 *  here: only arm64-v8a links it. Snowflake cannot be protect()ed at all. */
val TRANSPORT_CHOICES = listOf("auto", "cloak", "reality", "shadowsocks", "hysteria2", "anytls")

val HUB_METHOD_CHOICES = listOf("directIp", "reality", "shadowsocks", "fronted", "normal")

fun HubMethods.isEnabled(method: String): Boolean = when (method) {
    "directIp" -> directIp
    "reality" -> reality
    "shadowsocks" -> shadowsocks
    "fronted" -> fronted
    "normal" -> normal
    else -> false
}

@Serializable
data class TunnelConfig(
    val address: String,
    val prefixLength: Int,
    val dns: List<String> = emptyList(),
    val mtu: Int,
    val serverId: String,
    val serverName: String,
    val entryServerId: String = "",
    val entryServerName: String = "",
)
