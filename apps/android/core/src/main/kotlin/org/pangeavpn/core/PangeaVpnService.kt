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
                val builder = Builder()
                    .setSession("PangeaVPN")
                    .addAddress(config.address, config.prefixLength)
                    .addRoute("0.0.0.0", 0)
                config.dns.forEach { builder.addDnsServer(it) }
                builder.setMtu(config.mtu)
                builder.setBlocking(false)

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
            ConnState.CONNECTED -> "PangeaVPN — Connected"
            ConnState.CONNECTING -> "PangeaVPN — Connecting…"
            ConnState.DISCONNECTING -> "PangeaVPN — Disconnecting…"
            ConnState.ERROR -> "PangeaVPN — Connection error"
            ConnState.DISCONNECTED -> "PangeaVPN"
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
            .addAction(0, "Disconnect", disconnectIntent)
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
