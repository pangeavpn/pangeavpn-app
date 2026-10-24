package org.pangeavpn.core

import android.content.Context
import android.net.ConnectivityManager
import android.net.Network
import android.net.NetworkCapabilities
import android.net.NetworkRequest
import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.filter

/** Watches the physical networks under the tunnel. WireGuard roams across a
 *  changed address, but the TCP transport carrying it does not. */
class NetworkMonitor(context: Context) {
    private val app = context.applicationContext
    private val manager = app.getSystemService(ConnectivityManager::class.java)
    private val fingerprint = ConnectivityNetworkKey(app)

    /** Emits the new fingerprint on every change to the underlay. NOT_VPN keeps
     *  our own tunnel out, so bringing it up cannot feed back as a change. */
    fun changes(): Flow<String> = callbackFlow {
        val connectivity = manager
        if (connectivity == null) {
            close()
            return@callbackFlow
        }

        val request = NetworkRequest.Builder()
            .addCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)
            .addCapability(NetworkCapabilities.NET_CAPABILITY_NOT_VPN)
            .build()

        val callback = object : ConnectivityManager.NetworkCallback() {
            private fun publish() {
                trySend(fingerprint.networkKey())
            }

            override fun onAvailable(network: Network) = publish()
            override fun onLost(network: Network) = publish()
            override fun onCapabilitiesChanged(
                network: Network,
                capabilities: NetworkCapabilities,
            ) = publish()
        }

        val registered = runCatching { connectivity.registerNetworkCallback(request, callback) }
        if (registered.isFailure) {
            close()
            return@callbackFlow
        }
        awaitClose { runCatching { connectivity.unregisterNetworkCallback(callback) } }
    }
        // An empty key means nothing to rebuild over; the next change carries one.
        .filter(String::isNotEmpty)
        .distinctUntilChanged()
        // Registration replays the networks already up, which is the baseline.
        .drop(1)
}
