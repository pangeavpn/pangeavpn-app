package org.pangeavpn.core.tunnel

import android.app.NotificationChannel
import android.app.NotificationManager
import android.content.Context
import android.os.Build
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.R
import org.pangeavpn.core.openAppIntent

private const val CHANNEL_ID = "alerts"
private const val ALERT_ID = 2

/** Posts what [AlertPolicy] decides, on the main thread only. */
internal class ConnectionAlerts(private val context: Context, private val scope: CoroutineScope) {
    private val policy = AlertPolicy()
    private var lostTimer: Job? = null

    fun handle(event: TunnelEvent) = perform(policy.decide(event, gate()))

    private fun gate() = AlertGate(AppPrefs.value.connectionAlerts, AppPrefs.appInForeground)

    private fun perform(actions: List<AlertAction>) {
        for (action in actions) {
            when (action) {
                is AlertAction.ScheduleLost -> scheduleLost(action.after)
                AlertAction.CancelLost -> {
                    lostTimer?.cancel()
                    lostTimer = null
                }
                is AlertAction.Remove -> action.kinds.forEach { manager()?.cancel(it.tag, ALERT_ID) }
                is AlertAction.Post -> post(action.alert)
            }
        }
    }

    private fun scheduleLost(after: Double) {
        lostTimer?.cancel()
        lostTimer = scope.launch {
            delay((after * 1000).toLong())
            lostTimer = null
            perform(policy.lostDelayElapsed(gate()))
        }
    }

    // Without permission to notify there is nothing to show, and the tunnel cannot ask.
    private fun post(alert: ConnectionAlert) {
        val manager = manager() ?: return
        if (!manager.areNotificationsEnabled()) return
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val name = context.getString(R.string.notification_alerts_channel)
            manager.createNotificationChannel(NotificationChannel(CHANNEL_ID, name, NotificationManager.IMPORTANCE_DEFAULT))
        }
        val (title, body) = text(alert)
        val sound = alert is ConnectionAlert.Blocked
        val builder = NotificationCompat.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.ic_stat_pangea)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(NotificationCompat.BigTextStyle().bigText(body))
            .setContentIntent(openAppIntent(context))
            .setAutoCancel(true)
            .setCategory(NotificationCompat.CATEGORY_STATUS)
            .setSilent(!sound)
        if (sound) builder.setDefaults(NotificationCompat.DEFAULT_SOUND)
        runCatching { manager.notify(alert.kind.tag, ALERT_ID, builder.build()) }
    }

    private fun text(alert: ConnectionAlert): Pair<String, String> = when (alert) {
        ConnectionAlert.Connected ->
            context.getString(R.string.alerts_connected_title) to context.getString(R.string.alerts_connected_body)
        ConnectionAlert.Lost ->
            context.getString(R.string.alerts_lost_title) to context.getString(R.string.alerts_lost_body)
        ConnectionAlert.Restored ->
            context.getString(R.string.alerts_restored_title) to context.getString(R.string.alerts_restored_body)
        is ConnectionAlert.Blocked -> context.getString(R.string.alerts_blocked_title) to
            context.getString(R.string.alerts_blocked_body, context.getString(alert.reason.messageRes))
    }

    private fun manager(): NotificationManager? = context.getSystemService(NotificationManager::class.java)
}
