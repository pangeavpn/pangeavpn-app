package org.pangeavpn.core

import android.content.Context
import android.net.ConnectivityManager
import android.net.LinkProperties
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import android.os.Build
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.distinctUntilChanged

/** iOS's path update: whether any network can carry traffic, and which way traffic would leave. */
data class PathUpdate(val usable: Boolean, val key: String)

private data class Link(val capabilities: NetworkCapabilities? = null, val properties: LinkProperties? = null)

/** Watches the physical networks under the tunnel, as iOS's path monitor does. */
class NetworkMonitor(context: Context) {
    private val manager = context.applicationContext.getSystemService(ConnectivityManager::class.java)

    // NOT_VPN keeps our own tunnel out, so bringing it up cannot feed back as a change.
    fun updates(): Flow<PathUpdate> = callbackFlow {
        val connectivity = manager
        if (connectivity == null) {
            close()
            return@callbackFlow
        }
        val links = HashMap<Network, Link>()
        fun publish() {
            trySend(synchronized(links) { pathUpdate(links.values) })
        }

        val callback = object : ConnectivityManager.NetworkCallback() {
            override fun onAvailable(network: Network) {
                // Only releases before 8.0 skip the capability and link callbacks that follow this one.
                if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) return
                val link = Link(connectivity.getNetworkCapabilities(network), connectivity.getLinkProperties(network))
                synchronized(links) { links[network] = link }
                publish()
            }

            override fun onCapabilitiesChanged(network: Network, capabilities: NetworkCapabilities) {
                synchronized(links) { links[network] = (links[network] ?: Link()).copy(capabilities = capabilities) }
                publish()
            }

            override fun onLinkPropertiesChanged(network: Network, properties: LinkProperties) {
                synchronized(links) { links[network] = (links[network] ?: Link()).copy(properties = properties) }
                publish()
            }

            override fun onLost(network: Network) {
                synchronized(links) { links.remove(network) }
                publish()
            }
        }

        // Seeded first, so the first update describes the networks already up rather than none.
        synchronized(links) {
            @Suppress("DEPRECATION")
            for (network in runCatching { connectivity.allNetworks }.getOrNull().orEmpty()) {
                val capabilities = connectivity.getNetworkCapabilities(network) ?: continue
                if (!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)) continue
                if (!capabilities.hasCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)) continue
                links[network] = Link(capabilities, connectivity.getLinkProperties(network))
            }
        }
        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()
        if (runCatching { connectivity.registerNetworkCallback(request, callback) }.isFailure) {
            close()
            return@callbackFlow
        }
        publish()
        awaitClose { runCatching { connectivity.unregisterNetworkCallback(callback) } }
    }.distinctUntilChanged()
}

// "wifi=wlan0,cell=rmnet0", then the stable address tokens of only those interfaces, as iOS keys a path.
private fun pathUpdate(links: Collection<Link>): PathUpdate {
    val usable = links.any { it.capabilities?.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET) == true }
    val physical = links.mapNotNull { link ->
        val label = link.capabilities?.let(::transportLabel) ?: return@mapNotNull null
        val properties = link.properties ?: return@mapNotNull null
        val name = properties.interfaceName ?: return@mapNotNull null
        Triple("$label=$name", name, properties)
    }.distinctBy { it.second }.sortedBy { it.first }
    if (physical.isEmpty()) return PathUpdate(usable, "")
    val route = physical.joinToString(",") { it.first }
    val tokens = physical.flatMap { (_, name, properties) ->
        properties.linkAddresses.mapNotNull { address -> networkToken(address.address)?.let { "$name:$it" } }
    }.sorted()
    return PathUpdate(usable, if (tokens.isEmpty()) route else route + "|" + tokens.joinToString("|"))
}

private fun transportLabel(capabilities: NetworkCapabilities): String? = when {
    capabilities.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) -> "wifi"
    capabilities.hasTransport(NetworkCapabilities.TRANSPORT_CELLULAR) -> "cell"
    capabilities.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) -> "wired"
    else -> null
}
