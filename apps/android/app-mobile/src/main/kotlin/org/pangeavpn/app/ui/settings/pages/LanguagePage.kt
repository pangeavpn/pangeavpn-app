package org.pangeavpn.app.ui.settings.pages

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.AppLocale
import org.pangeavpn.core.settings.AppLanguages
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ChoiceRow
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage

@Composable
fun LanguagePage(onBack: () -> Unit) {
    val context = LocalContext.current
    // null follows the system; a tag the app has no strings for reads as English, as on iOS.
    var selected by remember { mutableStateOf(AppLocale.current(context)?.let { AppLanguages.forTag(it).code }) }

    fun choose(code: String?) {
        if (code == selected) return
        val activity = context.findActivity() ?: return
        selected = code
        AppLocale.set(activity, code)
    }

    SettingsPage(
        title = stringResource(R.string.settings_section_language),
        onBack = onBack,
        description = stringResource(R.string.language_description),
    ) {
        SettingsGroup(title = stringResource(R.string.language_current_heading)) {
            Column(
                modifier = Modifier.selectableGroup(),
                verticalArrangement = Arrangement.spacedBy(9.dp),
            ) {
                ChoiceRow(
                    label = stringResource(R.string.language_android_system),
                    selected = selected == null,
                    onClick = { choose(null) },
                )
                AppLanguages.supported.forEach { language ->
                    ChoiceRow(
                        label = language.name,
                        selected = selected == language.code,
                        onClick = { choose(language.code) },
                    )
                }
            }
        }
    }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
