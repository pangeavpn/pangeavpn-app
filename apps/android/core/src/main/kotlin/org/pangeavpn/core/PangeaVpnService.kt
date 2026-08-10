package org.pangeavpn.core

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.net.VpnService
import android.os.Build
import android.os.ParcelFileDescriptor
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.util.runCatchingCancellable

private const val CHANNEL_ID = "vpn"
private const val NOTIFICATION_ID = 1

private const val BLACKHOLE_ADDRESS_V6 = "fd00::1"
private const val BLACKHOLE_PREFIX_V6 = 128

class PangeaVpnService : VpnService() {

    private val scope = CoroutineScope(Dispatchers.Main + SupervisorJob())
    private var activeFd: ParcelFileDescriptor? = null
    @Volatile private var foregroundActive = false

    override fun onCreate() {
        super.onCreate()
        TunnelBridge.init(this)
        TunnelBridge.protector = { fd -> protect(fd.toInt()) }

        scope.launch {
            TunnelBridge.status.collect { status ->
                if (foregroundActive) {
                    notificationManager().notify(NOTIFICATION_ID, buildNotification(status))
                }
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_CONNECT -> {
                val serverId = intent.getStringExtra(EXTRA_SERVER_ID)
                if (serverId.isNullOrEmpty()) return START_NOT_STICKY
                beginForeground()
                startTunnel(serverId)
            }
            ACTION_DISCONNECT -> stopTunnel()
        }
        return START_NOT_STICKY
    }

    private fun beginForeground() {
        ensureChannel()
        foregroundActive = true
        startForeground(NOTIFICATION_ID, buildNotification(TunnelBridge.status.value))
    }

    private fun startTunnel(serverId: String) {
        scope.launch {
            try {
                val config = TunnelBridge.prepare(serverId)
                val settings = TunnelBridge.getSettings()
                val builder = Builder()
                    .setSession("PangeaVPN")
                    .addAddress(config.address, config.prefixLength)
                tunnelRoutes(settings.allowLan).forEach { builder.addRoute(it.address, it.prefixLength) }
                builder.blackholeIpv6(settings.allowLan)
                config.dns.forEach { builder.addDnsServer(it) }
                builder.setMtu(config.mtu)
                builder.setBlocking(false)
                applySplitTunnel(builder)

                val pfd = builder.establish() ?: error("VPN permission not granted")
                activeFd?.close()
                activeFd = pfd

                TunnelBridge.start(pfd.detachFd().toLong())
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                TunnelBridge.setError(e.message ?: "connection failed")
                endForeground()
                stopSelf()
            }
        }
    }

    /** The peer only advertises 0.0.0.0/0, so captured IPv6 is dropped by
     *  WireGuard rather than forwarded. Address and routes go in together or
     *  not at all: routes alone send IPv6 out around the tunnel instead of
     *  into it, and a device with IPv6 switched off rejects the address. */
    private fun Builder.blackholeIpv6(allowLan: Boolean) {
        val added = runCatching { addAddress(BLACKHOLE_ADDRESS_V6, BLACKHOLE_PREFIX_V6) }
        if (added.isFailure) return
        tunnelRoutesV6(allowLan).forEach { route -> addRoute(route.address, route.prefixLength) }
    }

    /** A package that has since been uninstalled throws, and would otherwise
     *  block every connect until the user found the stale entry. */
    private fun applySplitTunnel(builder: Builder) {
        val excluded = SplitTunnelStore(this).excluded()
        if (excluded.isEmpty()) return
        val stillInstalled = mutableSetOf<String>()
        for (packageName in excluded) {
            runCatching { builder.addDisallowedApplication(packageName) }
                .onSuccess { stillInstalled.add(packageName) }
        }
        if (stillInstalled.size != excluded.size) {
            SplitTunnelStore(this).setExcluded(stillInstalled)
        }
    }

    private fun stopTunnel() {
        scope.launch {
            runCatchingCancellable { TunnelBridge.stop() }
            activeFd?.close()
            activeFd = null
        }
        endForeground()
        stopSelf()
    }

    private fun endForeground() {
        foregroundActive = false
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
    }

    override fun onRevoke() {
        stopTunnel()
        super.onRevoke()
    }

    override fun onDestroy() {
        TunnelBridge.protector = { true }
        activeFd?.close()
        activeFd = null
        scope.cancel()
        super.onDestroy()
    }

    private fun notificationManager() = getSystemService(NotificationManager::class.java)

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(CHANNEL_ID, "VPN", NotificationManager.IMPORTANCE_LOW)
            notificationManager().createNotificationChannel(channel)
        }
    }

    private fun buildNotification(status: TunnelStatus): Notification {
        val title = when (status.state) {
            ConnState.CONNECTED -> getString(org.pangeavpn.core.R.string.notification_connected)
            ConnState.CONNECTING -> getString(org.pangeavpn.core.R.string.notification_connecting)
            ConnState.DISCONNECTING -> getString(org.pangeavpn.core.R.string.notification_disconnecting)
            ConnState.ERROR -> getString(org.pangeavpn.core.R.string.notification_error)
            ConnState.DISCONNECTED -> getString(org.pangeavpn.core.R.string.notification_disconnected)
        }
        val disconnectIntent = PendingIntent.getService(
            this,
            0,
            Intent(this, PangeaVpnService::class.java).setAction(ACTION_DISCONNECT),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(title)
            .setSmallIcon(android.R.drawable.ic_dialog_info)
            .setOngoing(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .addAction(0, getString(org.pangeavpn.core.R.string.notification_disconnect), disconnectIntent)
            .build()
    }

    companion object {
        const val ACTION_CONNECT = "org.pangeavpn.core.action.CONNECT"
        const val ACTION_DISCONNECT = "org.pangeavpn.core.action.DISCONNECT"
        const val EXTRA_SERVER_ID = "org.pangeavpn.core.extra.SERVER_ID"

        fun connect(context: Context, serverId: String) {
            val intent = Intent(context, PangeaVpnService::class.java)
                .setAction(ACTION_CONNECT)
                .putExtra(EXTRA_SERVER_ID, serverId)
            ContextCompat.startForegroundService(context, intent)
        }

        fun disconnect(context: Context) {
            val intent = Intent(context, PangeaVpnService::class.java).setAction(ACTION_DISCONNECT)
            ContextCompat.startForegroundService(context, intent)
        }
    }
}
