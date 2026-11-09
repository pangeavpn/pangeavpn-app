package org.pangeavpn.app.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.sizeIn
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.session.DeviceLimitRetry
import org.pangeavpn.core.session.shownName
import org.pangeavpn.core.viewmodel.DevicesViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.loginErrorText
import org.pangeavpn.ui.text.mediumDate
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateError

private val LimitMaxWidth = 520.dp
private val LimitWideWidth = 700.dp

// On a wide window the list hugs its rows so the buttons sit under it; the floor keeps pull to refresh usable.
private val WideListMin = 120.dp
private val WideListMax = 440.dp

private enum class DeviceLimitNotice(val text: Int) {
    LOAD_FAILED(R.string.devicelimit_load_failed),
    REMOVE_FAILED(R.string.devicelimit_remove_failed),
    STILL_AT_LIMIT(R.string.devicelimit_still_at_limit),
}

/** Sign-in hit the account's device limit: free a slot, then continue the sign-in, or cancel it. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun DeviceLimitScreen() {
    val model: DevicesViewModel = viewModel()
    val state by model.state.collectAsState()
    val loginError by SessionRepository.loginError.collectAsState()
    val loggingIn by SessionRepository.loggingIn.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    val scope = rememberCoroutineScope()
    val removedText = stringResource(R.string.devicelimit_removed)

    // Saved so a rotation keeps them; set by this screen's own first load, so an older list never shows.
    var loaded by rememberSaveable { mutableStateOf(false) }
    var removedAny by rememberSaveable { mutableStateOf(false) }
    var notice by rememberSaveable { mutableStateOf<DeviceLimitNotice?>(null) }
    var removingId by remember { mutableStateOf<String?>(null) }
    var retrying by remember { mutableStateOf(false) }
    var refreshing by remember { mutableStateOf(false) }

    fun busy(): Boolean = removingId != null || state.busyId != null || retrying || loggingIn || signingOut

    val listEmpty = loaded && !state.loading && state.devices.isEmpty() && state.error == null

    suspend fun load() {
        model.load()
        loaded = true
        if (model.state.value.error != null) notice = DeviceLimitNotice.LOAD_FAILED
    }

    fun refresh() {
        refreshing = true
        scope.launch {
            try {
                load()
            } finally {
                refreshing = false
            }
        }
    }

    fun remove(device: Device) {
        if (busy()) return
        removingId = device.id
        notice = null
        SessionRepository.clearLoginError()
        scope.launch {
            try {
                if (model.remove(device.id)) {
                    removedAny = true
                    ToastCenter.show(removedText, ToastStyle.SUCCESS)
                } else {
                    notice = DeviceLimitNotice.REMOVE_FAILED
                }
            } finally {
                removingId = null
            }
        }
    }

    // A failed retry stays on this screen and shows its error here; a repeat limit only says so.
    fun continueSignIn() {
        if (busy()) return
        retrying = true
        notice = null
        scope.launch {
            try {
                if (SessionRepository.retryLogin() == DeviceLimitRetry.STILL_AT_LIMIT) {
                    notice = DeviceLimitNotice.STILL_AT_LIMIT
                    model.load()
                }
            } finally {
                retrying = false
            }
        }
    }

    fun cancel() {
        if (busy()) return
        notice = null
        scope.launch { SessionRepository.cancelDeviceLimit() }
    }

    LaunchedEffect(Unit) { load() }
    BackHandler { cancel() }

    val shownNotice = notice
    val shownError = loginError
    val message: Pair<String, Boolean>? = when {
        shownNotice != null -> stringResource(shownNotice.text) to true
        shownError != null -> loginErrorText(shownError) to true
        listEmpty -> stringResource(R.string.devicelimit_none_left) to false
        else -> null
    }

    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        val wide = maxWidth >= LimitWideWidth
        val scroll = rememberScrollState()

        Column(
            modifier = Modifier
                .fillMaxWidth()
                .then(if (wide) Modifier.verticalScroll(scroll).heightIn(min = maxHeight) else Modifier.fillMaxHeight())
                .wrapContentWidth()
                .widthIn(max = LimitMaxWidth)
                .fillMaxWidth()
                .padding(start = 20.dp, top = 32.dp, end = 20.dp, bottom = 24.dp),
            verticalArrangement = if (wide) Arrangement.Center else Arrangement.Top,
        ) {
            SectionLabel(
                text = stringResource(R.string.devicelimit_title),
                modifier = Modifier.semantics { heading() },
            )
            Text(
                text = stringResource(R.string.devicelimit_subtitle),
                modifier = Modifier.padding(top = 8.dp),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 22.sp,
                lineHeight = 28.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.5).sp,
            )

            val area = if (wide) Modifier.padding(top = 22.dp) else Modifier.weight(1f).padding(top = 22.dp)
            if (!loaded || (state.loading && state.devices.isEmpty())) {
                Box(
                    modifier = area.fillMaxWidth().then(if (wide) Modifier.height(WideListMin) else Modifier.fillMaxHeight()),
                    contentAlignment = Alignment.Center,
                ) {
                    CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
                }
            } else {
                PullToRefreshBox(
                    isRefreshing = refreshing,
                    onRefresh = { refresh() },
                    modifier = area.fillMaxWidth(),
                ) {
                    LazyColumn(
                        modifier = if (wide) {
                            Modifier.fillMaxWidth().heightIn(min = WideListMin, max = WideListMax)
                        } else {
                            Modifier.fillMaxSize()
                        },
                        verticalArrangement = Arrangement.spacedBy(9.dp),
                        contentPadding = PaddingValues(bottom = 12.dp),
                    ) {
                        items(state.devices, key = { it.id }) { device ->
                            DeviceLimitRow(
                                device = device,
                                removing = removingId == device.id,
                                enabled = !busy(),
                                onRemove = { remove(device) },
                            )
                        }
                    }
                }
            }

            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
            ) {
                if (message != null) {
                    Text(
                        text = message.first,
                        modifier = Modifier.padding(vertical = 10.dp),
                        color = if (message.second) StateError else LocalTextSecondary.current,
                        fontSize = 13.sp,
                        lineHeight = 18.sp,
                    )
                }
            }

            Column(
                modifier = Modifier.padding(top = 6.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (removedAny || listEmpty) {
                    PillButton(
                        label = stringResource(R.string.devicelimit_continue),
                        onClick = { continueSignIn() },
                        enabled = !busy() || retrying,
                        loading = retrying,
                    )
                }
                GhostButton(
                    label = stringResource(R.string.devicelimit_cancel),
                    onClick = { cancel() },
                    enabled = !busy(),
                )
            }
        }
    }
}

@Composable
private fun DeviceLimitRow(device: Device, removing: Boolean, enabled: Boolean, onRemove: () -> Unit) {
    val name = device.shownName ?: stringResource(R.string.devicelimit_unnamed)
    val removeLabel = stringResource(if (removing) R.string.devicelimit_removing else R.string.devices_remove)
    val added = mediumDate(device.createdAt)?.let { stringResource(R.string.devicelimit_added, it) }

    PangeaCard(padding = PaddingValues(start = 14.dp, end = 4.dp, top = 6.dp, bottom = 6.dp)) {
        Row(
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            Column(
                modifier = Modifier.weight(1f).semantics(mergeDescendants = true) {},
                verticalArrangement = Arrangement.spacedBy(2.dp),
            ) {
                Row(
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(6.dp),
                ) {
                    Text(
                        text = name,
                        modifier = Modifier.weight(1f, fill = false),
                        color = MaterialTheme.colorScheme.onSurface,
                        fontSize = 15.sp,
                        fontWeight = FontWeight.SemiBold,
                        maxLines = 1,
                        overflow = TextOverflow.Ellipsis,
                    )
                    if (device.current) PillBadge(text = stringResource(R.string.devicelimit_this_device))
                }
                if (added != null) {
                    Text(
                        text = added,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        fontSize = 12.sp,
                        maxLines = 1,
                    )
                }
            }
            // Every row has a Remove button, so TalkBack also hears which device it removes.
            Box(
                modifier = Modifier
                    .sizeIn(minWidth = 48.dp, minHeight = 48.dp)
                    .clip(RoundedCornerShape(10.dp))
                    .clickable(enabled = enabled, role = Role.Button, onClick = onRemove)
                    .semantics { contentDescription = "$removeLabel, $name" }
                    .padding(horizontal = 12.dp),
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = removeLabel,
                    modifier = Modifier.clearAndSetSemantics {},
                    color = StateError.copy(alpha = if (enabled || removing) 1f else 0.45f),
                    fontSize = 13.sp,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
    }
}
