package org.pangeavpn.core

import android.content.Context

private const val PREFS = "split_tunnel"
private const val KEY_EXCLUDED = "excludedPackages"

/** Packages the user has chosen to keep outside the tunnel. Android applies
 *  this per app via Builder.addDisallowedApplication. */
class SplitTunnelStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun excluded(): Set<String> = prefs.getStringSet(KEY_EXCLUDED, emptySet()).orEmpty()

    fun setExcluded(packages: Set<String>) {
        prefs.edit().putStringSet(KEY_EXCLUDED, packages).apply()
    }

    fun toggle(packageName: String, excluded: Boolean) {
        val next = excluded().toMutableSet()
        if (excluded) next.add(packageName) else next.remove(packageName)
        setExcluded(next)
    }
}
