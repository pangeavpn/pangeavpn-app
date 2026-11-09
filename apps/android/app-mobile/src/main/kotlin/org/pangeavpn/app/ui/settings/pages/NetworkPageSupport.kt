package org.pangeavpn.app.ui.settings.pages

import android.content.res.Resources
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.SwitchColors
import androidx.compose.material3.SwitchDefaults
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.DisposableEffect
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.settingsErrorText

/** Loads the core settings when a page opens and drops a failed save's note when it closes; returns that note. */
@Composable
internal fun networkPageError(): String? {
    LaunchedEffect(Unit) { SettingsRepository.load() }
    DisposableEffect(Unit) {
        onDispose { SettingsRepository.clearError() }
    }
    val error by SettingsRepository.error.collectAsState()
    return error?.let { settingsErrorText(it) }
}

internal fun reportNetworkSave(resources: Resources, saved: Boolean, success: String) {
    if (saved) {
        ToastCenter.show(success, ToastStyle.SUCCESS)
    } else {
        ToastCenter.show(resources.getString(R.string.network_update_failed), ToastStyle.WARNING)
    }
}

/** The muted line under a control, drawn as CommittedField draws its hint. */
@Composable
internal fun NetworkHint(text: String) {
    Text(
        text = text,
        modifier = Modifier.padding(horizontal = 4.dp),
        color = MaterialTheme.colorScheme.onSurfaceVariant,
        fontSize = 12.sp,
        lineHeight = 16.sp,
    )
}

/** SwitchRow's colours, for rows that carry more than a title and a hint. */
@Composable
internal fun networkSwitchColors(): SwitchColors = SwitchDefaults.colors(
    checkedThumbColor = MaterialTheme.colorScheme.onPrimary,
    checkedTrackColor = MaterialTheme.colorScheme.primary,
    uncheckedThumbColor = MaterialTheme.colorScheme.onSurfaceVariant,
    uncheckedTrackColor = MaterialTheme.colorScheme.surfaceVariant,
    uncheckedBorderColor = MaterialTheme.colorScheme.outline,
)

/** A left-to-right isolate, so an address list keeps its order inside Arabic or Persian text. */
internal fun ltrIsolated(text: String): String = "${Char(0x2066)}$text${Char(0x2069)}"
