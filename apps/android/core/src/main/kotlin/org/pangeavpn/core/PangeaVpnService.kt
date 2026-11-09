package org.pangeavpn.core

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.net.VpnService
import android.os.Build
import android.util.Log
import androidx.core.app.NotificationCompat
import androidx.core.app.ServiceCompat
import androidx.core.content.ContextCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.launch
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.tunnel.StartRequest
import org.pangeavpn.core.tunnel.SystemVpnState
import org.pangeavpn.core.tunnel.TunnelFailure
import org.pangeavpn.core.tunnel.TunnelLifecycle
import org.pangeavpn.core.tunnel.TunnelView
import org.pangeavpn.core.util.runCatchingCancellable

private const val TAG = "PangeaVpnService"
private const val CHANNEL_ID = "vpn"
private const val NOTIFICATION_ID = 1

class PangeaVpnService : VpnService() {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
    internal lateinit var lifecycle: TunnelLifecycle
    private var foreground = false
    private var lastStartId = 0

    // Only what the notification shows, so byte counters never re-post it.
    private data class Card(val state: ConnState, val serverName: String, val lockdown: Boolean)

    override fun onCreate() {
        super.onCreate()
        TunnelBridge.init(this)
        TunnelBridge.protector = { fd -> protect(fd.toInt()) }
        lifecycle = TunnelLifecycle(this)
        instance = this
        scope.launch {
            combine(TunnelBridge.status, AppPrefs.state) { status, prefs ->
                Card(status.state, status.serverName, prefs.lockdown)
            }.distinctUntilChanged().collect { if (foreground) post(it) }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        lastStartId = startId
        when (intent?.action) {
            ACTION_DISCONNECT -> lifecycle.stop()
            ACTION_CONNECT -> {
                AppPrefs.update { it.copy(tunnelWanted = true) }
                val serverId = intent?.getStringExtra(EXTRA_SERVER_ID).orEmpty()
                val candidates = intent?.getStringArrayListExtra(EXTRA_CANDIDATES).orEmpty()
                lifecycle.start(StartRequest(serverId, candidates))
                goForeground()
            }
            VpnService.SERVICE_INTERFACE, null -> systemStart(restart = intent == null, startId = startId)
        }
        refreshSystemVpn()
        return if (AppPrefs.value.tunnelWanted) START_STICKY else START_NOT_STICKY
    }

    // Always-on, boot or a sticky restart: the core's last server, with traffic held until it answers.
    private fun systemStart(restart: Boolean, startId: Int) {
        if (restart && !AppPrefs.value.tunnelWanted) {
            stopSelf(startId)
            return
        }
        if (!lifecycle.active) lifecycle.start(null)
        goForeground()
    }

    // Another VPN took over or the user turned this one off in Settings: a user stop.
    override fun onRevoke() {
        lifecycle.stop()
    }

    override fun onDestroy() {
        instance = null
        _systemVpnState.value = null
        lifecycle.release()
        TunnelBridge.protector = { true }
        scope.cancel()
        super.onDestroy()
    }

    /** The lifecycle's teardown is done: leave the foreground and stop, unless a newer start arrived. */
    internal fun finished() {
        foreground = false
        ServiceCompat.stopForeground(this, ServiceCompat.STOP_FOREGROUND_REMOVE)
        stopSelf(lastStartId)
    }

    internal fun refreshSystemVpn() {
        _systemVpnState.value =
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) SystemVpnState(isAlwaysOn, isLockdownEnabled) else null
    }

    private fun goForeground() {
        ensureChannel()
        val notification = buildNotification(currentCard())
        try {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
                ServiceCompat.startForeground(
                    this,
                    NOTIFICATION_ID,
                    notification,
                    ServiceInfo.FOREGROUND_SERVICE_TYPE_SYSTEM_EXEMPTED,
                )
            } else {
                startForeground(NOTIFICATION_ID, notification)
            }
            foreground = true
        } catch (e: RuntimeException) {
            Log.w(TAG, "could not enter the foreground", e)
        }
    }

    private fun currentCard(): Card {
        val status = TunnelBridge.status.value
        return Card(status.state, status.serverName, AppPrefs.value.lockdown)
    }

    private fun post(card: Card) {
        getSystemService(NotificationManager::class.java)?.notify(NOTIFICATION_ID, buildNotification(card))
    }

    private fun ensureChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val name = getString(R.string.notification_channel_name)
        val channel = NotificationChannel(CHANNEL_ID, name, NotificationManager.IMPORTANCE_LOW)
        getSystemService(NotificationManager::class.java)?.createNotificationChannel(channel)
    }

    private fun buildNotification(card: Card): Notification {
        val title = when (card.state) {
            ConnState.CONNECTED -> R.string.notification_connected
            ConnState.CONNECTING -> R.string.notification_connecting
            ConnState.DISCONNECTING -> R.string.notification_disconnecting
            ConnState.ERROR -> R.string.notification_error
            ConnState.DISCONNECTED -> R.string.notification_disconnected
        }
        val builder = NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle(getString(title))
            .setContentText(card.serverName.ifEmpty { null })
            .setSmallIcon(R.drawable.ic_stat_pangea)
            .setContentIntent(openAppIntent(this))
            .setOngoing(true)
            .setOnlyAlertOnce(true)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setForegroundServiceBehavior(NotificationCompat.FOREGROUND_SERVICE_IMMEDIATE)
        // Lockdown hides Disconnect, as it does in the app.
        if (!card.lockdown) {
            val stop = PendingIntent.getService(
                this,
                0,
                Intent(this, PangeaVpnService::class.java).setAction(ACTION_DISCONNECT),
                PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
            )
            builder.addAction(0, getString(R.string.notification_disconnect), stop)
        }
        return builder.build()
    }

    companion object {
        const val ACTION_CONNECT = "org.pangeavpn.core.action.CONNECT"
        const val ACTION_DISCONNECT = "org.pangeavpn.core.action.DISCONNECT"
        const val EXTRA_SERVER_ID = "org.pangeavpn.core.extra.SERVER_ID"
        const val EXTRA_CANDIDATES = "org.pangeavpn.core.extra.CANDIDATES"

        private val main = CoroutineScope(SupervisorJob() + Dispatchers.Main)

        // Set from onCreate to onDestroy; the service shares the app's process.
        @Volatile private var instance: PangeaVpnService? = null

        private val _systemVpnState = MutableStateFlow<SystemVpnState?>(null)

        /** Android's Always-on VPN and "Block connections without VPN"; null below API 29 or with no service. */
        val systemVpnState: StateFlow<SystemVpnState?> = _systemVpnState.asStateFlow()

        fun connect(context: Context, serverId: String, candidates: List<String> = emptyList()) {
            val intent = Intent(context, PangeaVpnService::class.java)
                .setAction(ACTION_CONNECT)
                .putExtra(EXTRA_SERVER_ID, serverId)
                .putStringArrayListExtra(EXTRA_CANDIDATES, ArrayList(candidates))
            try {
                ContextCompat.startForegroundService(context, intent)
            } catch (e: IllegalStateException) {
                // Android refused a start from the background, so nothing will run and the start ends here.
                Log.w(TAG, "the VPN service could not be started", e)
                val failure = TunnelFailure.CONNECT_FAILED
                TunnelBridge.show(TunnelView(ConnState.ERROR, context.getString(failure.messageRes), failure))
            }
        }

        /** Safe with no service running: then it only makes sure the core holds no tunnel. */
        fun disconnect(context: Context) {
            val service = instance
            if (service != null) {
                main.launch { service.lifecycle.stop() }
                return
            }
            TunnelBridge.init(context)
            AppPrefs.update { it.copy(tunnelWanted = false) }
            TunnelBridge.show(TunnelView(ConnState.DISCONNECTED))
            main.launch { runCatchingCancellable { TunnelBridge.stop() } }
        }

        /** Moves a running tunnel in place. False when there is no tunnel to move. */
        fun switchTo(serverId: String, candidates: List<String>): Boolean {
            val service = instance ?: return false
            if (!service.lifecycle.active) return false
            main.launch { service.lifecycle.switchServer(serverId, candidates) }
            return true
        }

        internal fun startFromBoot(context: Context) {
            val intent = Intent(context, PangeaVpnService::class.java).setAction(VpnService.SERVICE_INTERFACE)
            runCatching { ContextCompat.startForegroundService(context, intent) }
                .onFailure { Log.w(TAG, "the VPN service could not be started at boot", it) }
        }
    }
}

// Resolved from the package, so the phone and TV apps each open their own launcher activity,
// and pinned to it, so the PendingIntent can never resolve anywhere else.
internal fun openAppIntent(context: Context): PendingIntent? {
    val launch = context.packageManager.getLaunchIntentForPackage(context.packageName) ?: return null
    launch.setPackage(context.packageName)
    launch.addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_CLEAR_TOP)
    return PendingIntent.getActivity(
        context,
        0,
        launch,
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
}
