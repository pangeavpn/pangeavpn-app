package org.pangeavpn.core.viewmodel

import android.app.Application
import android.content.Intent
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import org.pangeavpn.core.SplitTunnelStore

data class InstalledApp(
    val packageName: String,
    val label: String,
    val excluded: Boolean,
)

data class SplitTunnelUiState(
    /** The apps whose name matches [query], excluded ones first as of the last load. */
    val apps: List<InstalledApp> = emptyList(),
    val loading: Boolean = true,
    val query: String = "",
)

/** The app list of the split tunnelling page. The master switch is AppPrefs.splitTunnelEnabled, not here. */
class SplitTunnelViewModel(app: Application) : AndroidViewModel(app) {
    private val store = SplitTunnelStore(app)
    private var installed = emptyList<InstalledApp>()

    private val _state = MutableStateFlow(SplitTunnelUiState())
    val state: StateFlow<SplitTunnelUiState> = _state.asStateFlow()

    init {
        load()
    }

    /** Reads the installed apps again; a page calls it on opening so the order is current. */
    fun load() {
        viewModelScope.launch {
            installed = withContext(Dispatchers.IO) { queryApps() }
            _state.value = _state.value.copy(apps = matching(_state.value.query), loading = false)
        }
    }

    fun setQuery(query: String) {
        _state.value = _state.value.copy(query = query, apps = matching(query))
    }

    // A toggled row keeps its place, so it never jumps from under the finger; the next load re-sorts.
    fun toggle(packageName: String, excluded: Boolean) {
        store.toggle(packageName, excluded)
        installed = installed.map { if (it.packageName == packageName) it.copy(excluded = excluded) else it }
        _state.value = _state.value.copy(apps = matching(_state.value.query))
    }

    private fun matching(query: String): List<InstalledApp> {
        val needle = query.trim()
        if (needle.isEmpty()) return installed
        return installed.filter { it.label.contains(needle, ignoreCase = true) }
    }

    // Launchable apps only, which the manifest's <queries> covers without QUERY_ALL_PACKAGES.
    // Never this app: excluding ourselves would cut the control plane off from its own tunnel.
    @Suppress("DEPRECATION")
    private fun queryApps(): List<InstalledApp> {
        val context = getApplication<Application>()
        val packageManager = context.packageManager
        val excluded = store.excluded()
        return listOf(Intent.CATEGORY_LAUNCHER, Intent.CATEGORY_LEANBACK_LAUNCHER)
            .flatMap { packageManager.queryIntentActivities(Intent(Intent.ACTION_MAIN).addCategory(it), 0) }
            .mapNotNull { it.activityInfo?.applicationInfo }
            .distinctBy { it.packageName }
            .filter { it.packageName != context.packageName }
            .map { info ->
                InstalledApp(
                    packageName = info.packageName,
                    label = packageManager.getApplicationLabel(info).toString(),
                    excluded = info.packageName in excluded,
                )
            }
            .sortedWith(compareByDescending<InstalledApp> { it.excluded }.thenBy { it.label.lowercase() })
    }
}
