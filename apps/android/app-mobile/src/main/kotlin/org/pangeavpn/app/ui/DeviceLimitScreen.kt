package org.pangeavpn.app.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.app.viewmodel.DevicesViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.theme.StateError

private val RowShape = RoundedCornerShape(14.dp)

/** Shown when the hub rejects login for exceeding the account's device limit.
 *  Lets the user free a slot, then retry the sign-in that got here. */
@Composable
fun DeviceLimitScreen(
    onRetry: () -> Unit,
    onBack: () -> Unit,
    modifier: Modifier = Modifier,
    devicesViewModel: DevicesViewModel = viewModel(),
) {
    BackHandler(onBack = onBack)

    val state by devicesViewModel.state.collectAsState()

    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .padding(horizontal = 20.dp)
                .padding(top = 32.dp, bottom = 24.dp),
        ) {
            SectionLabel(text = stringResource(R.string.devicelimit_title))
            Text(
                text = stringResource(R.string.devicelimit_subtitle),
                modifier = Modifier.padding(top = 8.dp),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 22.sp,
                lineHeight = 28.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.5).sp,
            )

            Spacer(Modifier.height(22.dp))

            if (state.loading && state.devices.isEmpty()) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .weight(1f),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
                }
            } else {
                LazyColumn(
                    modifier = Modifier.weight(1f),
                    verticalArrangement = Arrangement.spacedBy(9.dp),
                    contentPadding = PaddingValues(bottom = 12.dp),
                ) {
                    items(state.devices, key = { it.id }) { device ->
                        DeviceRow(
                            name = device.friendlyName ?: device.id,
                            identifier = device.id,
                            onRemove = { devicesViewModel.remove(device.id) },
                        )
                    }
                }
            }

            state.error?.let {
                Text(
                    text = it,
                    modifier = Modifier.padding(vertical = 10.dp),
                    color = StateError,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
            }

            PillButton(
                label = stringResource(R.string.login_signin),
                onClick = onRetry,
                modifier = Modifier.padding(top = 6.dp),
            )
            GhostButton(
                label = stringResource(R.string.devicelimit_back),
                onClick = onBack,
                modifier = Modifier.padding(top = 10.dp),
            )
        }
    }
}

@Composable
private fun DeviceRow(name: String, identifier: String, onRemove: () -> Unit) {
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .clip(RowShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(1.dp, MaterialTheme.colorScheme.outlineVariant, RowShape)
            .padding(start = 14.dp, end = 6.dp, top = 10.dp, bottom = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = name,
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            if (identifier != name) {
                Text(
                    text = identifier,
                    modifier = Modifier.padding(top = 1.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 11.sp,
                    maxLines = 1,
                    overflow = TextOverflow.Ellipsis,
                )
            }
        }
        Text(
            text = stringResource(R.string.devices_remove),
            modifier = Modifier
                .clickable(role = Role.Button, onClick = onRemove)
                .padding(horizontal = 12.dp, vertical = 8.dp),
            color = StateError,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
        )
    }
}
