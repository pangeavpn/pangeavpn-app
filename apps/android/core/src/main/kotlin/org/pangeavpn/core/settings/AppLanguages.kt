package org.pangeavpn.core.settings

data class AppLanguage(val code: String, val name: String)

/** The app's languages by their own names, as desktop's language menu lists them. */
object AppLanguages {
    val supported = listOf(
        AppLanguage("en", "English"),
        AppLanguage("es", "Español"),
        AppLanguage("fr", "Français"),
        AppLanguage("ru", "Русский"),
        AppLanguage("uk", "Українська"),
        AppLanguage("zh", "中文"),
        AppLanguage("ar", "العربية"),
        AppLanguage("fa", "فارسی"),
    )

    /** Anything the app has no strings for is shown in English, so it reads as English. */
    fun forTag(tag: String?): AppLanguage {
        val code = tag.orEmpty().substringBefore('-').substringBefore('_').lowercase()
        return supported.firstOrNull { it.code == code } ?: supported[0]
    }
}
