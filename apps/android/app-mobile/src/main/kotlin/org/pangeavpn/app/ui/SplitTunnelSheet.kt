package org.pangeavpn.app.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import org.pangeavpn.core.viewmodel.SplitTunnelViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.SwitchRow

/** Per-app tunnel bypass. A switched-on app is excluded from the VPN. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SplitTunnelSheet(
    onDismiss: () -> Unit,
    modifier: Modifier = Modifier,
    viewModel: SplitTunnelViewModel = viewModel(),
) {
    val state by viewModel.state.collectAsState()

    ModalBottomSheet(
        onDismissRequest = onDismiss,
        modifier = modifier,
        containerColor = MaterialTheme.colorScheme.background,
    ) {
        Column(modifier = Modifier.padding(horizontal = 20.dp)) {
            Text(
                text = stringResource(R.string.split_tunnel_title),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 26.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.6).sp,
            )
            Text(
                text = stringResource(R.string.split_tunnel_hint),
                modifier = Modifier.padding(top = 6.dp, bottom = 14.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                lineHeight = 17.sp,
            )

            if (state.loading) {
                Box(
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(vertical = 40.dp),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
                }
                return@Column
            }

            LazyColumn(
                verticalArrangement = Arrangement.spacedBy(9.dp),
                contentPadding = PaddingValues(bottom = 28.dp),
            ) {
                items(state.apps, key = { it.packageName }) { app ->
                    SwitchRow(
                        title = app.label,
                        checked = app.excluded,
                        onCheckedChange = { viewModel.toggle(app.packageName, it) },
                    )
                }
            }
        }
    }
}
