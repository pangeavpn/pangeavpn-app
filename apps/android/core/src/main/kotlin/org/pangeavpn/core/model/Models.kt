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
)

@Serializable
data class Server(
    val id: String,
    val name: String,
    val region: String,
    val country: String,
    val load: Int?,
)

@Serializable
data class Subscription(
    val status: String,
    val renews: Boolean,
    val expiresAt: String?,
)

@Serializable
data class Device(
    val id: String,
    val friendlyName: String?,
    val createdAt: String,
    val status: String,
)

@Serializable
data class Session(
    val email: String = "",
    val name: String = "",
    val servers: List<Server> = emptyList(),
)

/** Mirrors hubMethods in daemon/mobile/hubmethods.go. */
@Serializable
data class HubMethods(
    val directIp: Boolean = true,
    val shadowsocks: Boolean = true,
    val fronted: Boolean = true,
    val normal: Boolean = false,
    val rev: Int = 1,
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
)

/** Transports the Android build can actually run; NaiveProxy is cgo-only. */
val TRANSPORT_CHOICES = listOf("auto", "cloak", "reality", "shadowsocks", "hysteria2", "snowflake")

val HUB_METHOD_CHOICES = listOf("directIp", "shadowsocks", "fronted", "normal")

fun HubMethods.isEnabled(method: String): Boolean = when (method) {
    "directIp" -> directIp
    "shadowsocks" -> shadowsocks
    "fronted" -> fronted
    "normal" -> normal
    else -> false
}

@Serializable
data class TunnelConfig(
    val address: String,
    val prefixLength: Int,
    val dns: List<String>,
    val mtu: Int,
    val serverId: String,
    val serverName: String,
)
