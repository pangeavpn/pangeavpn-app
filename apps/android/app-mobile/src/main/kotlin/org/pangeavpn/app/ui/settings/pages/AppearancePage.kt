package org.pangeavpn.app.ui.settings.pages

import androidx.compose.runtime.Composable
import androidx.compose.ui.res.stringResource
import org.pangeavpn.core.ThemeChoice
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.AccentSwatches
import org.pangeavpn.ui.components.SegmentedControl
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.themeChoiceTitle

@Composable
fun AppearancePage(onBack: () -> Unit) {
    val appearance = LocalAppearance.current

    SettingsPage(
        title = stringResource(R.string.settings_section_appearance),
        onBack = onBack,
        description = stringResource(R.string.appearance_description),
    ) {
        SettingsGroup(title = stringResource(R.string.appearance_theme_heading)) {
            SegmentedControl(
                options = ThemeChoice.entries,
                selected = appearance.theme,
                onSelect = { appearance.set(it) },
                label = { themeChoiceTitle(it) },
            )
        }
        SettingsGroup(title = stringResource(R.string.appearance_accent_heading)) {
            AccentSwatches(selected = appearance.accent, onSelect = { appearance.setAccent(it) })
        }
    }
}
