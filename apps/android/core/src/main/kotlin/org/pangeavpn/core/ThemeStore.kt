package org.pangeavpn.core

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update

private const val PREFS = "appearance"
private const val KEY_DARK = "darkTheme"
private const val KEY_ACCENT = "accent"

enum class ThemeChoice { SYSTEM, LIGHT, DARK }

/** The five desktop accents as ARGB, with desktop's hover shade. Stored by lowercase name, as on iOS. */
enum class Accent(val argb: Long, val hoverArgb: Long) {
    TERRA(0xFFC3562B, 0xFFA84A25),
    PURPLE(0xFF8451C9, 0xFF6F41B0),
    OCEAN(0xFF2B7BC3, 0xFF226499),
    EMERALD(0xFF26804F, 0xFF1D6A40),
    ROSE(0xFFC32B5E, 0xFFA32450);

    val key: String get() = name.lowercase()
}

data class Appearance(
    val theme: ThemeChoice = ThemeChoice.SYSTEM,
    val accent: Accent = Accent.TERRA,
)

/** Theme override and accent. Absent keys mean follow the system and terra, as on a fresh install.
 *  Every instance shares one state, so a change repaints the whole process at once. */
class ThemeStore(context: Context) {
    private val prefs = context.applicationContext.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    val state: StateFlow<Appearance> get() = shared

    init {
        shared.value = Appearance(theme = choice(), accent = accent())
    }

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
        shared.update { it.copy(theme = choice) }
    }

    fun accent(): Accent {
        val stored = prefs.getString(KEY_ACCENT, null)
        return Accent.entries.firstOrNull { it.key == stored } ?: Accent.TERRA
    }

    fun setAccent(accent: Accent) {
        prefs.edit().putString(KEY_ACCENT, accent.key).apply()
        shared.update { it.copy(accent = accent) }
    }

    /** Back to a fresh install, for the panic wipe. */
    fun reset() {
        prefs.edit().clear().apply()
        shared.value = Appearance()
    }

    private companion object {
        val shared = MutableStateFlow(Appearance())
    }
}

fun ThemeChoice.isDark(systemInDark: Boolean): Boolean = when (this) {
    ThemeChoice.SYSTEM -> systemInDark
    ThemeChoice.DARK -> true
    ThemeChoice.LIGHT -> false
}
