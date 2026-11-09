package org.pangeavpn.core

import android.content.Context
import android.content.SharedPreferences
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

private const val PREFS_FILE = "pangea_prefs"
private const val LIST_SEPARATOR = ","

/** Everything the app remembers outside the Go config. Keys match iOS SharedPrefs. */
data class Prefs(
    val autoConnect: Boolean = true,
    val lockdown: Boolean = false,
    val connectionAlerts: Boolean = true,
    val panicWipe: Boolean = false,
    val developerMode: Boolean = false,
    val splitTunnelEnabled: Boolean = false,
    val splitTunnelRanges: List<String> = emptyList(),
    val recentRegions: List<String> = emptyList(),
    val rotationVisited: List<String> = emptyList(),
    val pinnedServerId: String? = null,
    val multihopEnabled: Boolean = false,
    // null or empty lets the core pick the entry.
    val multihopEntryId: String? = null,
    val dataNoticeVersion: Int = 0,
    // The user's last word on the VPN: set by a connect, cleared by a disconnect.
    val tunnelWanted: Boolean = false,
    // A language tag picked in the app; null follows the system.
    val language: String? = null,
)

object AppPrefs {
    private val _state = MutableStateFlow(Prefs())
    val state: StateFlow<Prefs> = _state.asStateFlow()
    val value: Prefs get() = _state.value

    /** In-memory only: the service shares this process, and a dead app is not in front. */
    @Volatile var appInForeground: Boolean = false

    private var prefs: SharedPreferences? = null

    @Synchronized
    fun init(context: Context) {
        if (prefs != null) return
        val app = context.applicationContext
        val store = app.getSharedPreferences(PREFS_FILE, Context.MODE_PRIVATE)
        prefs = store
        _state.value = read(store, splitDefault = SplitTunnelStore(app).excluded().isNotEmpty())
    }

    @Synchronized
    fun update(transform: (Prefs) -> Prefs) {
        val next = transform(_state.value)
        if (next == _state.value) return
        _state.value = next
        prefs?.let { write(it, next) }
    }

    /** Back to a fresh install, for the panic wipe. */
    @Synchronized
    fun clear() {
        prefs?.edit()?.clear()?.apply()
        _state.value = Prefs()
    }

    // Installs from before the master switch had exclusions that were simply on.
    private fun read(store: SharedPreferences, splitDefault: Boolean): Prefs {
        val defaults = Prefs()
        return Prefs(
            autoConnect = store.getBoolean("autoConnect", defaults.autoConnect),
            lockdown = store.getBoolean("lockdown", defaults.lockdown),
            connectionAlerts = store.getBoolean("connectionAlerts", defaults.connectionAlerts),
            panicWipe = store.getBoolean("panicWipe", defaults.panicWipe),
            developerMode = store.getBoolean("developerMode", defaults.developerMode),
            splitTunnelEnabled = store.getBoolean("splitTunnelEnabled", splitDefault),
            splitTunnelRanges = store.list("splitTunnelRanges"),
            recentRegions = store.list("recentRegions"),
            rotationVisited = store.list("rotationVisited"),
            pinnedServerId = store.getString("pinnedServerId", null),
            multihopEnabled = store.getBoolean("multihopEnabled", defaults.multihopEnabled),
            multihopEntryId = store.getString("multihopEntryId", null),
            dataNoticeVersion = store.getInt("dataNoticeVersion", defaults.dataNoticeVersion),
            tunnelWanted = store.getBoolean("tunnelWanted", defaults.tunnelWanted),
            language = store.getString("language", null),
        )
    }

    private fun write(store: SharedPreferences, value: Prefs) {
        store.edit()
            .putBoolean("autoConnect", value.autoConnect)
            .putBoolean("lockdown", value.lockdown)
            .putBoolean("connectionAlerts", value.connectionAlerts)
            .putBoolean("panicWipe", value.panicWipe)
            .putBoolean("developerMode", value.developerMode)
            .putBoolean("splitTunnelEnabled", value.splitTunnelEnabled)
            .putString("splitTunnelRanges", value.splitTunnelRanges.joinToString(LIST_SEPARATOR))
            .putString("recentRegions", value.recentRegions.joinToString(LIST_SEPARATOR))
            .putString("rotationVisited", value.rotationVisited.joinToString(LIST_SEPARATOR))
            .putOrRemove("pinnedServerId", value.pinnedServerId)
            .putBoolean("multihopEnabled", value.multihopEnabled)
            .putOrRemove("multihopEntryId", value.multihopEntryId)
            .putInt("dataNoticeVersion", value.dataNoticeVersion)
            .putBoolean("tunnelWanted", value.tunnelWanted)
            .putOrRemove("language", value.language)
            .apply()
    }

    private fun SharedPreferences.list(key: String): List<String> =
        getString(key, "").orEmpty().split(LIST_SEPARATOR).filter(String::isNotEmpty)

    private fun SharedPreferences.Editor.putOrRemove(key: String, value: String?): SharedPreferences.Editor =
        if (value == null) remove(key) else putString(key, value)
}
