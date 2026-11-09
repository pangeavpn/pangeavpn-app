package org.pangeavpn.app.ui.settings

import android.content.Context
import android.os.Build
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.KeyValueRow
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage

private const val PLAY_STORE = "com.android.vending"

/** The installed version and build. There is no update check: a Play install updates through Play. */
@Composable
internal fun UpdatePage(onBack: () -> Unit) {
    val context = LocalContext.current
    val fromPlay = remember(context) { installedFromPlay(context) }

    SettingsPage(
        title = stringResource(R.string.settings_section_update),
        onBack = onBack,
        description = stringResource(if (fromPlay) R.string.update_description_play else R.string.update_description),
    ) {
        SettingsGroup(title = stringResource(R.string.update_installed_heading)) {
            PangeaCard(padding = PaddingValues(0.dp)) {
                KeyValueRow(label = stringResource(R.string.update_version), value = BuildConfig.VERSION_NAME)
                HorizontalDivider(color = MaterialTheme.colorScheme.outlineVariant)
                KeyValueRow(label = stringResource(R.string.update_build), value = BuildConfig.VERSION_CODE.toString())
            }
        }
        if (fromPlay) {
            PillButton(label = stringResource(R.string.update_open_play), onClick = { openPlayListing(context) })
        }
    }
}

private fun installedFromPlay(context: Context): Boolean {
    val packages = context.packageManager
    val installer = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.R) {
        packages.getInstallSourceInfo(context.packageName).installingPackageName
    } else {
        @Suppress("DEPRECATION")
        packages.getInstallerPackageName(context.packageName)
    }
    return installer == PLAY_STORE
}

private fun openPlayListing(context: Context) {
    val id = context.packageName
    if (!openLink(context, "market://details?id=$id")) {
        openLink(context, "https://play.google.com/store/apps/details?id=$id")
    }
}
