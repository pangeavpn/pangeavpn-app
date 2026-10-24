package org.pangeavpn.core.viewmodel

import android.app.Application
import android.content.Intent
import android.content.pm.ApplicationInfo
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
    val apps: List<InstalledApp> = emptyList(),
    val loading: Boolean = true,
)

class SplitTunnelViewModel(app: Application) : AndroidViewModel(app) {
    private val store = SplitTunnelStore(app)

    private val _state = MutableStateFlow(SplitTunnelUiState())
    val state: StateFlow<SplitTunnelUiState> = _state.asStateFlow()

    init {
        load()
    }

    fun load() {
        viewModelScope.launch {
            val apps = withContext(Dispatchers.IO) { queryApps() }
            _state.value = SplitTunnelUiState(apps = apps, loading = false)
        }
    }

    fun toggle(packageName: String, excluded: Boolean) {
        store.toggle(packageName, excluded)
        _state.value = _state.value.copy(
            apps = _state.value.apps.map {
                if (it.packageName == packageName) it.copy(excluded = excluded) else it
            },
        )
    }

    /** Launchable apps only, and never this app: excluding ourselves would cut
     *  the control plane off from the tunnel it is managing. */
    private fun queryApps(): List<InstalledApp> {
        val context = getApplication<Application>()
        val packageManager = context.packageManager
        val excluded = store.excluded()
        val launcherIntent = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)

        return packageManager.queryIntentActivities(launcherIntent, 0)
            .mapNotNull { it.activityInfo?.applicationInfo }
            .distinctBy { it.packageName }
            .filter { it.packageName != context.packageName }
            .map { info: ApplicationInfo ->
                InstalledApp(
                    packageName = info.packageName,
                    label = packageManager.getApplicationLabel(info).toString(),
                    excluded = excluded.contains(info.packageName),
                )
            }
            .sortedBy { it.label.lowercase() }
    }
}
