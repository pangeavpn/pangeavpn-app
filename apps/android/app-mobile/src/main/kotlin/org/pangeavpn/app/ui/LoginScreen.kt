package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material3.Button
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import org.pangeavpn.core.viewmodel.LoginUiState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.BrandLogo

/** Token sign-in. On error, offers a way into [DeviceLimitScreen] since the hub error text for a
 * device-limit rejection isn't a distinct, guaranteed marker — the affordance is always shown
 * alongside any login error rather than trying to pattern-match the message. */
@Composable
fun LoginScreen(
    token: String,
    onTokenChange: (String) -> Unit,
    uiState: LoginUiState,
    onSignIn: () -> Unit,
    onManageDevices: () -> Unit,
    modifier: Modifier = Modifier,
) {
    Column(
        modifier = modifier
            .fillMaxSize()
            .padding(32.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
        verticalArrangement = Arrangement.Center,
    ) {
        BrandLogo(size = 56.dp)
        Spacer(Modifier.height(8.dp))
        Text(
            text = stringResource(R.string.login_subtitle),
            style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(32.dp))
        OutlinedTextField(
            value = token,
            onValueChange = onTokenChange,
            placeholder = { Text(stringResource(R.string.login_token_placeholder)) },
            singleLine = true,
            modifier = Modifier.fillMaxWidth(),
        )
        Spacer(Modifier.height(16.dp))
        Button(
            onClick = onSignIn,
            enabled = !uiState.loading,
            modifier = Modifier.fillMaxWidth(),
        ) {
            if (uiState.loading) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    color = MaterialTheme.colorScheme.onPrimary,
                )
            } else {
                Text(stringResource(R.string.login_signin))
            }
        }
        uiState.error?.let { error ->
            Spacer(Modifier.height(16.dp))
            Text(text = error, color = MaterialTheme.colorScheme.error)
            TextButton(onClick = onManageDevices) {
                Text(stringResource(R.string.devicelimit_title))
            }
        }
    }
}
