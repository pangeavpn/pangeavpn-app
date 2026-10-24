package org.pangeavpn.app

import android.content.Intent
import android.os.Build
import android.service.quicksettings.Tile
import android.service.quicksettings.TileService
import androidx.annotation.RequiresApi
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.launch
import org.pangeavpn.core.PangeaVpnService
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.model.ConnState

/** Quick Settings toggle: connect to the last server, or disconnect. */
@RequiresApi(Build.VERSION_CODES.N)
class PangeaTileService : TileService() {

    private var scope: CoroutineScope? = null

    override fun onStartListening() {
        super.onStartListening()
        TunnelBridge.init(this)
        val active = CoroutineScope(Dispatchers.Main + SupervisorJob())
        scope = active
        active.launch {
            TunnelBridge.status.collect { render(it.state) }
        }
    }

    override fun onStopListening() {
        scope?.cancel()
        scope = null
        super.onStopListening()
    }

    override fun onClick() {
        super.onClick()
        when (TunnelBridge.status.value.state) {
            ConnState.CONNECTED, ConnState.CONNECTING -> PangeaVpnService.disconnect(this)
            else -> connect()
        }
    }

    /** VpnService needs consent the tile cannot request, so an unprepared
     *  device is sent to the app instead of failing silently. */
    private fun connect() {
        val consent = android.net.VpnService.prepare(this)
        if (consent != null) {
            openApp()
            return
        }
        val scopeForLaunch = scope ?: CoroutineScope(Dispatchers.Main + Job()).also { scope = it }
        scopeForLaunch.launch {
            val lastServer = runCatching { TunnelBridge.getSettings().lastServerId }.getOrNull()
            if (lastServer.isNullOrEmpty()) {
                openApp()
            } else {
                PangeaVpnService.connect(this@PangeaTileService, lastServer)
            }
        }
    }

    private fun openApp() {
        val intent = Intent(this, MainActivity::class.java).addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.UPSIDE_DOWN_CAKE) {
            startActivityAndCollapse(
                android.app.PendingIntent.getActivity(
                    this,
                    0,
                    intent,
                    android.app.PendingIntent.FLAG_UPDATE_CURRENT or android.app.PendingIntent.FLAG_IMMUTABLE,
                ),
            )
        } else {
            @Suppress("DEPRECATION")
            startActivityAndCollapse(intent)
        }
    }

    private fun render(state: ConnState) {
        val tile = qsTile ?: return
        tile.state = when (state) {
            ConnState.CONNECTED -> Tile.STATE_ACTIVE
            ConnState.CONNECTING, ConnState.DISCONNECTING -> Tile.STATE_UNAVAILABLE
            else -> Tile.STATE_INACTIVE
        }
        tile.updateTile()
    }
}
