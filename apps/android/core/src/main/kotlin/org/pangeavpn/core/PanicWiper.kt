package org.pangeavpn.core

import android.content.Context
import java.io.File
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.CoroutineStart
import kotlinx.coroutines.Deferred
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.async
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.withContext
import kotlinx.coroutines.withTimeoutOrNull
import org.pangeavpn.core.data.ServersRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.diagnostics.DiagnosticsStore
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.util.runCatchingCancellable

private const val CORE_WIPE_TIMEOUT_MS = 3_000L

// The core listens on this socket for the whole process; unlinking it would break the next connect.
private const val PROTECT_SOCKET = "protect.sock"

/** Panic: local only, because speed and leaving nothing behind matter more than freeing the device slot. */
object PanicWiper {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var running: Deferred<Unit>? = null

    private val _wiping = MutableStateFlow(false)
    /** True while a wipe runs, so the root can show the splash instead of a screen being emptied. */
    val wiping: StateFlow<Boolean> = _wiping.asStateFlow()

    /** Disconnects, wipes the core, clears every store and file. Never calls the hub. */
    suspend fun wipe(context: Context) {
        val app = context.applicationContext
        // Every caller awaits the one wipe in flight, and a caller that goes away does not stop it.
        val job = synchronized(this) {
            running?.takeUnless { it.isCompleted }
                ?: scope.async(start = CoroutineStart.LAZY) { erase(app) }.also { running = it }
        }
        job.await()
    }

    private suspend fun erase(app: Context) {
        _wiping.value = true
        try {
            // A cold launch from the shortcut wipes before anything else has opened the core.
            TunnelBridge.init(app)
            val idle = TunnelBridge.status.value.state == ConnState.DISCONNECTED && !AppPrefs.value.tunnelWanted
            if (!idle) PangeaVpnService.disconnect(app)
            // Past the deadline the core's wipe runs on and the rest goes ahead without it.
            val core = scope.async { runCatchingCancellable { TunnelBridge.wipe() } }
            withTimeoutOrNull(CORE_WIPE_TIMEOUT_MS) { core.await() }
            withContext(Dispatchers.IO) { clearStorage(app) }
            SessionRepository.clearLocal()
            SettingsRepository.reset()
            ServersRepository.reset()
            DiagnosticsStore.reset()
        } finally {
            _wiping.value = false
        }
    }

    private fun clearStorage(app: Context) {
        SecretStorePrefs.wipe(app)
        AppPrefs.clear()
        SplitTunnelStore(app).clear()
        ThemeStore(app).reset()
        clearOtherPreferences(app)
        app.filesDir.listFiles()?.filter { it.name != PROTECT_SOCKET }?.forEach { it.deleteRecursively() }
        app.cacheDir.listFiles()?.forEach { it.deleteRecursively() }
    }

    // Any preference file a store above does not own. Cleared in place, since its owner may hold it open.
    private fun clearOtherPreferences(app: Context) {
        val names = File(app.applicationInfo.dataDir, "shared_prefs").list().orEmpty()
            .filter { it.endsWith(".xml") }
            .map { it.removeSuffix(".xml") }
        for (name in names - SECRET_PREFS_FILE) {
            app.getSharedPreferences(name, Context.MODE_PRIVATE).edit().clear().commit()
        }
    }
}
