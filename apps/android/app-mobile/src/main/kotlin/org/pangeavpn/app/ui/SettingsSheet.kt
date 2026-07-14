package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.viewmodel.SettingsViewModel
import org.pangeavpn.ui.R

/** Kill-switch guide, sign out, and app version. Signing out clears the session in
 * [org.pangeavpn.core.data.SessionRepository]; AppRoot reacts to that and swaps back to Login,
 * so this sheet doesn't need its own navigation callback for sign-out. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsSheet(
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    settingsViewModel: SettingsViewModel = viewModel(),
) {
    val context = LocalContext.current

    ModalBottomSheet(onDismissRequest = onDismiss, modifier = modifier) {
        Column(modifier = Modifier.padding(24.dp)) {
            Text(stringResource(R.string.killswitch_title), style = MaterialTheme.typography.titleMedium)
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.killswitch_body),
                style = MaterialTheme.typography.bodyMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))
            OutlinedButton(
                onClick = { context.startActivity(settingsViewModel.openVpnSettings()) },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.killswitch_open_settings))
            }

            Spacer(Modifier.height(24.dp))

            TextButton(
                onClick = { settingsViewModel.signOut() },
                modifier = Modifier.fillMaxWidth(),
            ) {
                Text(stringResource(R.string.settings_signout))
            }

            Spacer(Modifier.height(16.dp))
            Text(
                text = "v${BuildConfig.VERSION_NAME}",
                style = MaterialTheme.typography.labelSmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
        }
    }
}
