package org.pangeavpn.tv

import android.app.Application
import kotlin.concurrent.thread
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.TunnelBridge

class TvApp : Application() {
    override fun onCreate() {
        super.onCreate()
        AppPrefs.init(this)
        // The native library and the Keystore warm up off the main thread before the first screen needs them.
        thread(name = "core-init") { TunnelBridge.init(this) }
    }
}
