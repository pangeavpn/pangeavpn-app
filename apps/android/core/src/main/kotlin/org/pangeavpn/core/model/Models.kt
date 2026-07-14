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

@Serializable
data class TunnelConfig(
    val address: String,
    val prefixLength: Int,
    val dns: List<String>,
    val mtu: Int,
    val serverId: String,
    val serverName: String,
)
