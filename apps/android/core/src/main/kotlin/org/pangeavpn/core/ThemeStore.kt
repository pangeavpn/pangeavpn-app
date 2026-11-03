package org.pangeavpn.core

import android.content.Context

private const val PREFS = "appearance"
private const val KEY_DARK = "darkTheme"

/** Which way the header's theme toggle has been thrown, if it has been. */
enum class ThemeChoice { SYSTEM, LIGHT, DARK }

/** Remembers the theme override. Absent means follow the system, which is what
 *  a fresh install does until the user says otherwise. */
class ThemeStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    fun choice(): ThemeChoice = when {
        !prefs.contains(KEY_DARK) -> ThemeChoice.SYSTEM
        prefs.getBoolean(KEY_DARK, true) -> ThemeChoice.DARK
        else -> ThemeChoice.LIGHT
    }

    fun set(choice: ThemeChoice) {
        val editor = prefs.edit()
        when (choice) {
            ThemeChoice.SYSTEM -> editor.remove(KEY_DARK)
            ThemeChoice.DARK -> editor.putBoolean(KEY_DARK, true)
            ThemeChoice.LIGHT -> editor.putBoolean(KEY_DARK, false)
        }
        editor.apply()
    }
}

/** What the toggle does next: whatever is not on screen right now. */
fun ThemeChoice.toggled(systemInDark: Boolean): ThemeChoice {
    val showingDark = when (this) {
        ThemeChoice.SYSTEM -> systemInDark
        ThemeChoice.DARK -> true
        ThemeChoice.LIGHT -> false
    }
    return if (showingDark) ThemeChoice.LIGHT else ThemeChoice.DARK
}

fun ThemeChoice.isDark(systemInDark: Boolean): Boolean = when (this) {
    ThemeChoice.SYSTEM -> systemInDark
    ThemeChoice.DARK -> true
    ThemeChoice.LIGHT -> false
}
