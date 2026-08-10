package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.core.viewmodel.SplitTunnelViewModel
import org.pangeavpn.ui.R

/** Per-app tunnel bypass. A switched-on app is excluded from the VPN. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SplitTunnelSheet(
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    viewModel: SplitTunnelViewModel = viewModel(),
) {
    val state by viewModel.state.collectAsState()

    ModalBottomSheet(onDismissRequest = onDismiss, modifier = modifier) {
        Column(modifier = Modifier.padding(horizontal = 24.dp, vertical = 16.dp)) {
            Text(
                text = stringResource(R.string.split_tunnel_title),
                style = MaterialTheme.typography.headlineSmall,
            )
            Spacer(Modifier.height(4.dp))
            Text(
                text = stringResource(R.string.split_tunnel_hint),
                style = MaterialTheme.typography.bodySmall,
                color = MaterialTheme.colorScheme.onSurfaceVariant,
            )
            Spacer(Modifier.height(12.dp))

            if (state.loading) {
                CircularProgressIndicator()
                return@Column
            }

            LazyColumn {
                items(state.apps, key = { it.packageName }) { app ->
                    Row(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 8.dp),
                        verticalAlignment = Alignment.CenterVertically,
                    ) {
                        Text(
                            text = app.label,
                            modifier = Modifier
                                .weight(1f)
                                .padding(end = 12.dp),
                            style = MaterialTheme.typography.bodyMedium,
                        )
                        Switch(
                            checked = app.excluded,
                            onCheckedChange = { viewModel.toggle(app.packageName, it) },
                        )
                    }
                }
            }
        }
    }
}
