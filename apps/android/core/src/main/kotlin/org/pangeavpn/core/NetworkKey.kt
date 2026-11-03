package org.pangeavpn.core

import android.content.Context
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import java.net.Inet4Address
import java.net.Inet6Address
import java.net.InetAddress
import org.pangeavpn.mobile.NetworkKeyProvider

/** Fingerprints the physical network so the cascade can try whatever transport
 *  last worked here first. Mirrors the daemon's currentNetworkKey. */
class ConnectivityNetworkKey(context: Context) : NetworkKeyProvider {
    private val manager =
        context.applicationContext.getSystemService(ConnectivityManager::class.java)

    /** Empty disables the optimization, which only costs a full cascade walk. */
    override fun networkKey(): String {
        val connectivity = manager ?: return ""
        val parts = mutableListOf<String>()
        for (network in runCatching { connectivity.allNetworks }.getOrNull().orEmpty()) {
            val capabilities = connectivity.getNetworkCapabilities(network) ?: continue
            // Skip our own tunnel, or bringing it up would change the key.
            if (capabilities.hasTransport(NetworkCapabilities.TRANSPORT_VPN)) continue
            if (!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) continue

            val properties = connectivity.getLinkProperties(network) ?: continue
            val interfaceName = properties.interfaceName ?: continue
            for (linkAddress in properties.linkAddresses) {
                val token = networkToken(linkAddress.address) ?: continue
                parts += "$interfaceName:$token"
            }
        }
        return if (parts.isEmpty()) "" else parts.sorted().joinToString("|")
    }
}

// The stable part of the key: a full IPv4 address, or an IPv6 /64 whose host
// bits rotate under privacy extensions.
internal fun networkToken(address: InetAddress): String? {
    if (address.isLoopbackAddress || address.isLinkLocalAddress ||
        address.isMulticastAddress || address.isAnyLocalAddress
    ) {
        return null
    }
    return when (address) {
        is Inet4Address -> address.hostAddress
        is Inet6Address -> networkPrefix64(address)
        else -> null
    }
}

internal fun networkPrefix64(address: Inet6Address): String? {
    val bytes = address.address.copyOf()
    for (index in 8 until bytes.size) {
        bytes[index] = 0
    }
    val prefix = runCatching { InetAddress.getByAddress(bytes).hostAddress }.getOrNull()
    return prefix?.let { "$it/64" }
}
