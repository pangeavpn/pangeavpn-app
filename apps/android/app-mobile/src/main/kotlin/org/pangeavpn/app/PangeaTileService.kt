package org.pangeavpn.app

import android.app.PendingIntent
import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.launch
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.ConnectionState
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.TunnelStatus
import org.pangeavpn.core.R as CoreR
import org.pangeavpn.ui.R as UiR

// A connect outlives the panel closing, so it does not run in the listening scope.
private val actions = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

/** Quick Settings toggle: connect to the last pick, or disconnect. Anything needing the app opens it. */
class PangeaTileService : TileService() {
    private var listening: CoroutineScope? = null

    override fun onStartListening() {
        super.onStartListening()
        ConnectionRepository.init(this)
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
        listening = scope
        scope.launch {
            combine(ConnectionRepository.status, ConnectionRepository.state) { status, own -> status to own }
                .collect { (status, own) -> render(status, own) }
        }
    }

    override fun onStopListening() {
        listening?.cancel()
        listening = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        ConnectionRepository.init(this)
        if (isLocked) unlockAndRun { act() } else act()
    }

    private fun act() {
        if (HeroRules.isUp(ConnectionRepository.status.value.state)) {
            if (ConnectionRepository.lockdownBlocksDisconnect) openApp() else ConnectionRepository.toggle()
            return
        }
        if (ConnectionRepository.needsConsent()) {
            openApp()
            return
        }
        actions.launch { if (!ConnectionRepository.connectUnattended()) openApp() }
    }

    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(
                PendingIntent.getActivity(this, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE),
            )
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    private fun render(status: TunnelStatus, own: ConnectionState) {
        val tile = qsTile ?: return
        val display = HeroRules.display(status.state, own.busy, own.switching)
        tile.state = when (display) {
            ConnState.CONNECTED, ConnState.CONNECTING -> Tile.STATE_ACTIVE
            ConnState.DISCONNECTING -> Tile.STATE_UNAVAILABLE
            ConnState.DISCONNECTED, ConnState.ERROR -> Tile.STATE_INACTIVE
        }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            tile.subtitle = when (display) {
                ConnState.CONNECTED -> status.serverName.ifEmpty { null }
                ConnState.CONNECTING -> getString(CoreR.string.notification_connecting)
                ConnState.DISCONNECTING -> getString(UiR.string.hero_disconnecting)
                ConnState.DISCONNECTED, ConnState.ERROR -> null
            }
        }
        tile.updateTile()
    }
}
