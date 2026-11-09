package org.pangeavpn.core

import android.app.Activity
import android.app.LocaleManager
import android.content.Context
import android.content.res.Configuration
import android.content.res.Resources
import android.os.Build
import android.os.LocaleList
import androidx.annotation.RequiresApi

/** The app's own language: the system's per-app locale from API 33, a stored pick wrapped into each Activity before. */
object AppLocale {
    /** BCP-47 tag, or null to follow the system. */
    fun current(context: Context): String? {
        AppPrefs.init(context)
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return AppPrefs.value.language
        return systemPick(context).also(::adopt)
    }

    fun set(activity: Activity, tag: String?) {
        if (current(activity) == tag) return
        AppPrefs.update { it.copy(language = tag) }
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            activity.getSystemService(LocaleManager::class.java)?.applicationLocales =
                if (tag == null) LocaleList.getEmptyLocaleList() else LocaleList.forLanguageTags(tag)
        } else {
            activity.recreate()
        }
    }

    fun wrap(base: Context): Context {
        AppPrefs.init(base)
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) {
            adopt(systemPick(base))
            return base
        }
        val locales = AppPrefs.value.language?.let { LocaleList.forLanguageTags(it) }
            ?: Resources.getSystem().configuration.locales
        LocaleList.setDefault(locales)
        // The app context words the core's errors and the service's notices, so it follows the pick too.
        val app = base.applicationContext.resources
        @Suppress("DEPRECATION")
        app.updateConfiguration(Configuration(app.configuration).apply { setLocales(locales) }, app.displayMetrics)
        return base.createConfigurationContext(Configuration(base.resources.configuration).apply { setLocales(locales) })
    }

    @RequiresApi(Build.VERSION_CODES.TIRAMISU)
    private fun systemPick(context: Context): String? {
        val locales = context.getSystemService(LocaleManager::class.java)?.applicationLocales ?: return null
        return if (locales.isEmpty) null else locales[0].toLanguageTag()
    }

    // System settings can change the language behind the app's back, so the stored copy follows it.
    private fun adopt(tag: String?) {
        AppPrefs.update { it.copy(language = tag) }
    }
}
