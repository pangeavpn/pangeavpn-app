package org.pangeavpn.app.ui.settings.pages

import android.util.LruCache
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.selection.toggleable
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.rounded.ArrowBack
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.produceState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.ImageBitmap
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.platform.LocalFocusManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.core.graphics.drawable.toBitmap
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.viewmodel.InstalledApp
import org.pangeavpn.core.viewmodel.SplitTunnelViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SettingsField

private val AppIconSize = 36.dp

// Decoded icons outlive a scroll, so a row coming back into view never flashes blank.
private val appIcons = LruCache<String, ImageBitmap>(64)

/** The launchable apps, excluded ones first, each with a switch; a switched-on app bypasses the VPN. */
@Composable
internal fun SplitTunnelAppList(onBack: () -> Unit) {
    val viewModel: SplitTunnelViewModel = viewModel()
    val state by viewModel.state.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val focusManager = LocalFocusManager.current
    val search = stringResource(R.string.split_tunnel_apps_search)

    LaunchedEffect(viewModel) {
        viewModel.setQuery("")
        // A new view model is still on its first load; a kept one reloads so excluded apps lead again.
        if (!viewModel.state.value.loading) viewModel.load()
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 56.dp)
                .padding(start = 4.dp, end = 16.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            IconAction(
                icon = Icons.AutoMirrored.Rounded.ArrowBack,
                label = stringResource(R.string.devicelimit_back),
                onClick = onBack,
                tint = MaterialTheme.colorScheme.onSurface,
            )
            Text(
                text = stringResource(R.string.split_tunnel_apps_title),
                modifier = Modifier.padding(start = 4.dp).semantics { heading() },
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 22.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.4).sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
        }
        LazyColumn(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .wrapContentWidth()
                .widthIn(max = 640.dp)
                .fillMaxWidth(),
            contentPadding = PaddingValues(start = 16.dp, top = 8.dp, end = 16.dp, bottom = 32.dp),
            verticalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            item(key = "description") {
                Text(
                    text = stringResource(R.string.split_tunnel_hint),
                    modifier = Modifier.padding(bottom = 5.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontSize = 14.sp,
                    lineHeight = 20.sp,
                )
            }
            if (!prefs.splitTunnelEnabled) {
                item(key = "off") {
                    InfoNote(text = stringResource(R.string.split_tunnel_apps_off_note))
                }
            }
            item(key = "search") {
                SettingsField(
                    value = state.query,
                    onValueChange = viewModel::setQuery,
                    placeholder = search,
                    label = search,
                    keyboardOptions = KeyboardOptions(imeAction = ImeAction.Search),
                    keyboardActions = KeyboardActions(onSearch = { focusManager.clearFocus() }),
                )
            }
            when {
                state.loading -> item(key = "loading") {
                    Box(
                        modifier = Modifier
                            .fillMaxWidth()
                            .padding(vertical = 40.dp),
                        contentAlignment = Alignment.Center,
                    ) {
                        CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
                    }
                }
                state.apps.isEmpty() -> item(key = "empty") {
                    val query = state.query.trim()
                    Text(
                        text = if (query.isEmpty()) {
                            stringResource(R.string.split_tunnel_apps_none)
                        } else {
                            stringResource(R.string.split_tunnel_apps_no_match, query)
                        },
                        modifier = Modifier.padding(horizontal = 4.dp, vertical = 12.dp),
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        fontSize = 14.sp,
                        lineHeight = 20.sp,
                    )
                }
                else -> items(state.apps, key = { it.packageName }) { app ->
                    AppRow(app = app, onToggle = { viewModel.toggle(app.packageName, it) })
                }
            }
        }
    }
}

@Composable
private fun AppRow(app: InstalledApp, onToggle: (Boolean) -> Unit) {
    PangeaCard(padding = PaddingValues(0.dp)) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .toggleable(value = app.excluded, role = Role.Switch, onValueChange = onToggle)
                .heightIn(min = 56.dp)
                .padding(start = 12.dp, end = 10.dp, top = 8.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(12.dp),
        ) {
            AppIcon(app.packageName)
            Text(
                text = app.label,
                modifier = Modifier.weight(1f),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
                maxLines = 2,
                overflow = TextOverflow.Ellipsis,
            )
            Switch(checked = app.excluded, onCheckedChange = null, colors = networkSwitchColors())
        }
    }
}

@Composable
private fun AppIcon(packageName: String) {
    val packageManager = LocalContext.current.packageManager
    val sizePx = with(LocalDensity.current) { AppIconSize.roundToPx() }
    val icon by produceState<ImageBitmap?>(appIcons.get(packageName), packageName) {
        if (value != null) return@produceState
        value = withContext(Dispatchers.IO) {
            runCatching {
                packageManager.getApplicationIcon(packageName).toBitmap(sizePx, sizePx).asImageBitmap()
            }.getOrNull()
        }?.also { appIcons.put(packageName, it) }
    }

    Box(modifier = Modifier.size(AppIconSize)) {
        icon?.let { Image(bitmap = it, contentDescription = null, modifier = Modifier.fillMaxSize()) }
    }
}
