package org.pangeavpn.core

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.net.VpnService

/** Brings the VPN back after a reboot when the user left it on, as iOS's Connect On Demand does. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        AppPrefs.init(context)
        val prefs = AppPrefs.value
        if (!prefs.tunnelWanted || !(prefs.autoConnect || prefs.lockdown)) return
        // Withdrawn consent can only be asked for again in the app.
        if (VpnService.prepare(context) != null) return
        PangeaVpnService.startFromBoot(context)
    }
}
