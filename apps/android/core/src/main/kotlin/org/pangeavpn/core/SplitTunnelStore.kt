package org.pangeavpn.core

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow

private const val PREFS = "split_tunnel"
private const val KEY_EXCLUDED = "excludedPackages"

/** Packages kept outside the tunnel through Builder.addDisallowedApplication, while AppPrefs.splitTunnelEnabled is on. */
class SplitTunnelStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    init {
        shared.value = excluded()
    }

    fun excluded(): Set<String> = prefs.getStringSet(KEY_EXCLUDED, emptySet()).orEmpty().toSet()

    fun setExcluded(packages: Set<String>) {
        val copy = packages.toSet()
        prefs.edit().putStringSet(KEY_EXCLUDED, copy).apply()
        shared.value = copy
    }

    fun toggle(packageName: String, excluded: Boolean) {
        setExcluded(if (excluded) excluded() + packageName else excluded() - packageName)
    }

    fun clear() {
        prefs.edit().clear().apply()
        shared.value = emptySet()
    }

    companion object {
        private val shared = MutableStateFlow<Set<String>>(emptySet())

        /** The excluded packages as any instance last read or wrote them, for screens that only count them. */
        val excludedApps: StateFlow<Set<String>> = shared.asStateFlow()
    }
}
