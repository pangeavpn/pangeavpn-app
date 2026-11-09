package org.pangeavpn.tv.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.runtime.withFrameNanos
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.contentDescription
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
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.loginErrorText
import org.pangeavpn.ui.text.mediumDate
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateError

private enum class LimitNotice { LOAD_FAILED, REMOVE_FAILED, STILL_AT_LIMIT }

/** iOS DeviceLimitView: free a slot, then carry on with the number typed before. */
@Composable
fun TvDeviceLimitScreen() {
    val model: DevicesViewModel = viewModel()
    val devices by model.state.collectAsState()
    val loginError by SessionRepository.loginError.collectAsState()
    val loggingIn by SessionRepository.loggingIn.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    var removedAny by remember { mutableStateOf(false) }
    var retrying by remember { mutableStateOf(false) }
    var notice by remember { mutableStateOf<LimitNotice?>(null) }
    val scope = rememberCoroutineScope()
    val resources = LocalContext.current.resources
    val cancel = remember { FocusRequester() }
    val firstRemove = remember { FocusRequester() }
    val carryOn = remember { FocusRequester() }

    val busy = devices.busyId != null || retrying || loggingIn || signingOut
    val listEmpty = devices.loaded && !devices.loading && devices.devices.isEmpty() && devices.error == null

    // Focus goes to the next step: the first Remove, or Continue when nothing is left to remove.
    val reload: suspend () -> Unit = {
        model.load()
        val loaded = model.state.value
        notice = if (loaded.error != null) LimitNotice.LOAD_FAILED else null
        withFrameNanos {}
        when {
            loaded.error != null -> Unit
            loaded.devices.isNotEmpty() -> firstRemove.tryFocus()
            else -> carryOn.tryFocus()
        }
    }

    LaunchedEffect(Unit) {
        cancel.tryFocus()
        reload()
    }

    val remove: (Device) -> Unit = { device ->
        if (!busy) {
            notice = null
            SessionRepository.clearLoginError()
            scope.launch {
                if (model.remove(device.id)) {
                    removedAny = true
                    ToastCenter.show(resources.getString(R.string.devicelimit_removed), ToastStyle.SUCCESS)
                    // The removed row took the focus with it; Continue is the next step anyway.
                    withFrameNanos {}
                    carryOn.tryFocus()
                } else {
                    notice = LimitNotice.REMOVE_FAILED
                }
            }
        }
    }

    val continueSignIn: () -> Unit = {
        if (!busy) {
            retrying = true
            notice = null
            scope.launch {
                try {
                    if (SessionRepository.retryLogin() == DeviceLimitRetry.STILL_AT_LIMIT) {
                        notice = LimitNotice.STILL_AT_LIMIT
                        model.load()
                    }
                } finally {
                    retrying = false
                }
            }
        }
    }

    val shownNotice = notice
    val error = loginError
    val message: Pair<String, Boolean>? = when {
        shownNotice != null -> noticeText(shownNotice) to true
        error != null -> loginErrorText(error) to true
        listEmpty -> stringResource(R.string.devicelimit_none_left) to false
        else -> null
    }

    Row(modifier = Modifier.fillMaxSize().overscan()) {
        Column(modifier = Modifier.weight(0.42f).fillMaxHeight()) {
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
            message?.let { (text, isError) ->
                Text(
                    text = text,
                    modifier = Modifier
                        .padding(top = 14.dp)
                        .semantics { liveRegion = LiveRegionMode.Polite },
                    color = if (isError) StateError else LocalTextSecondary.current,
                    fontSize = 13.sp,
                    lineHeight = 18.sp,
                )
            }
            if (notice == LimitNotice.LOAD_FAILED) {
                GhostButton(
                    label = stringResource(R.string.support_try_again),
                    onClick = { if (!busy && !devices.loading) scope.launch { reload() } },
                    loading = devices.loading,
                    compact = true,
                    modifier = Modifier.padding(top = 10.dp),
                )
            }
            Spacer(modifier = Modifier.weight(1f))
            Column(
                modifier = Modifier.widthIn(max = 360.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                if (removedAny || listEmpty) {
                    PillButton(
                        label = stringResource(R.string.devicelimit_continue),
                        onClick = continueSignIn,
                        loading = retrying,
                        modifier = Modifier.focusRequester(carryOn),
                    )
                }
                GhostButton(
                    label = stringResource(R.string.devicelimit_cancel),
                    onClick = {
                        if (!busy) {
                            notice = null
                            scope.launch { SessionRepository.cancelDeviceLimit() }
                        }
                    },
                    loading = signingOut,
                    modifier = Modifier.focusRequester(cancel),
                )
            }
        }
        Spacer(modifier = Modifier.width(32.dp))
        Box(modifier = Modifier.weight(0.58f).fillMaxHeight()) {
            if (devices.loading && devices.devices.isEmpty()) {
                CircularProgressIndicator(
                    modifier = Modifier.align(Alignment.Center),
                    color = MaterialTheme.colorScheme.primary,
                )
            } else {
                LazyColumn(
                    modifier = Modifier.fillMaxSize(),
                    verticalArrangement = Arrangement.spacedBy(9.dp),
                    contentPadding = PaddingValues(vertical = 4.dp),
                ) {
                    itemsIndexed(devices.devices, key = { _, device -> device.id }) { index, device ->
                        DeviceRow(
                            device = device,
                            removing = devices.busyId == device.id,
                            enabled = !busy,
                            onRemove = { remove(device) },
                            buttonModifier = if (index == 0) Modifier.focusRequester(firstRemove) else Modifier,
                        )
                    }
                }
            }
        }
    }
}

@Composable
private fun noticeText(notice: LimitNotice): String = stringResource(
    when (notice) {
        LimitNotice.LOAD_FAILED -> R.string.devicelimit_load_failed
        LimitNotice.REMOVE_FAILED -> R.string.devicelimit_remove_failed
        LimitNotice.STILL_AT_LIMIT -> R.string.devicelimit_still_at_limit
    },
)

@Composable
private fun DeviceRow(
    device: Device,
    removing: Boolean,
    enabled: Boolean,
    onRemove: () -> Unit,
    buttonModifier: Modifier,
) {
    val name = device.shownName ?: stringResource(R.string.devicelimit_unnamed)
    val removeLabel = stringResource(if (removing) R.string.devicelimit_removing else R.string.devices_remove)
    val added = mediumDate(device.createdAt)?.let { stringResource(R.string.devicelimit_added, it) }

    Row(
        modifier = Modifier
            .fillMaxWidth()
            .tvCard(focused = false)
            .padding(start = 14.dp, end = 8.dp, top = 8.dp, bottom = 8.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Column(modifier = Modifier.weight(1f)) {
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
                    modifier = Modifier.padding(top = 2.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontSize = 12.sp,
                    maxLines = 1,
                )
            }
        }
        // Never disabled, so focus survives another row's removal; the handler ignores busy presses.
        GhostButton(
            label = removeLabel,
            onClick = onRemove,
            destructive = true,
            compact = true,
            modifier = buttonModifier
                .alpha(if (enabled || removing) 1f else 0.45f)
                .semantics { contentDescription = "$removeLabel, $name" },
        )
    }
}
