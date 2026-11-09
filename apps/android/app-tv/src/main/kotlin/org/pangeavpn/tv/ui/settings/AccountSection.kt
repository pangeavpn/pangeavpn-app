package org.pangeavpn.tv.ui.settings

import android.app.Activity
import android.app.KeyguardManager
import android.content.Context
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Logout
import androidx.compose.material.icons.outlined.Delete
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import java.util.concurrent.atomic.AtomicReference
import kotlinx.coroutines.CompletableDeferred
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.session.DeviceRename
import org.pangeavpn.core.session.RenameError
import org.pangeavpn.core.session.shownName
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.core.util.runCatchingCancellable
import org.pangeavpn.core.viewmodel.DevicesViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.deviceErrorText
import org.pangeavpn.ui.text.mediumDate
import org.pangeavpn.ui.text.renameErrorText
import org.pangeavpn.ui.text.subscriptionLineText
import org.pangeavpn.ui.theme.StateConnecting
import org.pangeavpn.ui.theme.StateError

private const val MASK = "••••-••••-••••-••••-••••-••••"

@Composable
internal fun AccountSection(
    devices: DevicesViewModel,
    refreshingSubscription: Boolean,
    screenScope: CoroutineScope,
    onClose: () -> Unit,
) {
    AccountCard(refreshingSubscription)
    DevicesGroup(devices)
    AccountActions(screenScope, onClose)
}

/** The device-owner check before the number shows. TVs often have no lock at all, and then
 *  nothing is asked; a cancel or any failure is false. */
@Composable
private fun rememberOwnerCheck(): suspend () -> Boolean {
    val context = LocalContext.current
    val title = stringResource(R.string.settings_account_number)
    val reason = stringResource(R.string.session_reveal_reason)
    val answer = remember { AtomicReference<CompletableDeferred<Boolean>?>(null) }
    val launcher = rememberLauncherForActivityResult(ActivityResultContracts.StartActivityForResult()) {
        answer.get()?.complete(it.resultCode == Activity.RESULT_OK)
    }
    return remember(context, title, reason) {
        suspend {
            val keyguard = context.getSystemService(Context.KEYGUARD_SERVICE) as KeyguardManager
            @Suppress("DEPRECATION")
            val intent = if (keyguard.isDeviceSecure) keyguard.createConfirmDeviceCredentialIntent(title, reason) else null
            if (intent == null) {
                true
            } else {
                val result = CompletableDeferred<Boolean>()
                answer.set(result)
                if (runCatching { launcher.launch(intent) }.isSuccess) result.await() else false
            }
        }
    }
}

@Composable
private fun AccountCard(refreshing: Boolean) {
    val scope = rememberCoroutineScope()
    val ownerCheck = rememberOwnerCheck()
    val subscription by SessionRepository.subscription.collectAsState()
    var revealed by remember { mutableStateOf<String?>(null) }
    var revealing by remember { mutableStateOf(false) }
    // Bumped on every hide, so an unlock that finishes after it cannot show the number again.
    var generation by remember { mutableIntStateOf(0) }
    val hidden = stringResource(R.string.settings_account_hidden)
    val line = subscriptionLineText(subscription)
    val loading = refreshing && subscription == null

    fun hide() {
        generation++
        revealed = null
    }

    fun reveal() {
        if (revealing) return
        revealing = true
        val started = generation
        scope.launch {
            val number = if (ownerCheck()) runCatchingCancellable { SessionRepository.accountNumber() }.getOrNull() else null
            revealing = false
            if (started == generation && number != null) revealed = formatAccountNumberInput(number)
        }
    }

    // The credential screen pauses the activity itself, so only an idle card hides then.
    LifecycleEventEffect(Lifecycle.Event.ON_PAUSE) { if (!revealing) hide() }

    Column(
        modifier = Modifier.fillMaxWidth().tvCard().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(16.dp)) {
            Column(modifier = Modifier.weight(1f), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                Text(
                    text = stringResource(R.string.settings_account_number),
                    modifier = Modifier.semantics { heading() },
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                Text(
                    text = revealed ?: MASK,
                    // One character at a time, so TalkBack spells the number out.
                    modifier = Modifier.semantics {
                        contentDescription = revealed?.toList()?.joinToString(" ") ?: hidden
                    },
                    style = ltrStyle(),
                    color = if (revealed == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 17.sp,
                    fontWeight = if (revealed == null) FontWeight.Normal else FontWeight.Medium,
                    maxLines = 1,
                )
            }
            GhostButton(
                label = stringResource(if (revealed == null) R.string.settings_account_show else R.string.settings_account_hide),
                onClick = { if (revealed == null) reveal() else hide() },
                loading = revealing,
                compact = true,
            )
        }
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(1.dp)
                .background(MaterialTheme.colorScheme.outlineVariant),
        )
        Column(
            modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(
                text = stringResource(R.string.settings_account_subscription),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 15.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = if (loading) stringResource(R.string.settings_account_loading) else line.text,
                color = if (line.warn && !loading) StateConnecting else MaterialTheme.colorScheme.onSurfaceVariant,
                fontFamily = FontFamily.Monospace,
                fontSize = 14.sp,
                lineHeight = 20.sp,
            )
        }
    }
}

@Composable
private fun DevicesGroup(model: DevicesViewModel) {
    val state by model.state.collectAsState()
    val resources = LocalContext.current.resources
    var renaming by remember { mutableStateOf<Device?>(null) }
    var renameInvalid by remember { mutableStateOf(false) }
    var removing by remember { mutableStateOf<Device?>(null) }
    val renamed = stringResource(R.string.devices_renamed)
    val removed = stringResource(R.string.devices_removed)
    val failed = stringResource(R.string.network_update_failed)

    // A refusal also stays in the model's error, which the group shows above the list.
    fun showFailure() {
        val error = model.state.value.error
        ToastCenter.show(error?.let { deviceErrorText(resources, it) } ?: failed, ToastStyle.WARNING)
    }

    TvGroupLabel(stringResource(R.string.devices_title))
    TvParagraph(stringResource(R.string.devices_subtitle))
    state.error?.let { error ->
        InfoNote(text = deviceErrorText(error), tone = NoteTone.ERROR)
        GhostButton(
            label = stringResource(R.string.restore_failed_retry),
            onClick = { settingsScope.launch { model.load() } },
            compact = true,
        )
    }
    when {
        state.devices.isEmpty() && (state.loading || !state.loaded) ->
            Box(modifier = Modifier.fillMaxWidth().padding(vertical = 24.dp), contentAlignment = Alignment.Center) {
                CircularProgressIndicator(modifier = Modifier.size(28.dp), color = MaterialTheme.colorScheme.primary)
            }
        state.devices.isEmpty() ->
            if (state.error == null) TvParagraph(stringResource(R.string.devices_none))
        else -> state.devices.forEach { device ->
            key(device.id) {
                DeviceCard(
                    device = device,
                    busy = state.busyId == device.id,
                    locked = state.busyId != null,
                    onRename = {
                        renameInvalid = false
                        renaming = device
                    },
                    onRemove = { removing = device },
                )
            }
        }
    }

    renaming?.let { device ->
        TvEditDialog(
            title = stringResource(R.string.devices_rename),
            initial = DeviceRename.clamped(device.friendlyName.orEmpty()),
            placeholder = stringResource(R.string.devices_rename_label),
            error = if (renameInvalid) renameErrorText(RenameError.LENGTH) else null,
            transform = DeviceRename::clamped,
            ltr = false,
            onDismiss = { renaming = null },
            onSave = { name ->
                if (DeviceRename.validated(name) == null) {
                    renameInvalid = true
                } else {
                    renaming = null
                    settingsScope.launch {
                        if (model.rename(device.id, name)) ToastCenter.show(renamed, ToastStyle.SUCCESS) else showFailure()
                    }
                }
            },
        )
    }

    removing?.let { device ->
        TvConfirmDialog(
            title = stringResource(
                R.string.devices_remove_title,
                device.shownName ?: stringResource(R.string.devices_unnamed),
            ),
            message = stringResource(R.string.devices_remove_body),
            confirmLabel = stringResource(R.string.devices_remove),
            cancelLabel = stringResource(R.string.devices_rename_cancel),
            destructive = true,
            onDismiss = { removing = null },
            // The current device leaves only by signing out, which also frees its slot.
            onConfirm = {
                if (!device.current) {
                    settingsScope.launch {
                        if (model.remove(device.id)) ToastCenter.show(removed, ToastStyle.SUCCESS) else showFailure()
                    }
                }
            },
        )
    }
}

// The buttons stay focusable while another device is busy, so the remote never loses its place.
@Composable
private fun DeviceCard(device: Device, busy: Boolean, locked: Boolean, onRename: () -> Unit, onRemove: () -> Unit) {
    Column(
        modifier = Modifier.fillMaxWidth().tvCard().padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(12.dp),
    ) {
        Column(
            modifier = Modifier.semantics(mergeDescendants = true) {},
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp), verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = device.shownName ?: stringResource(R.string.devices_unnamed),
                    modifier = Modifier.weight(1f, fill = false),
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 2,
                )
                if (device.current) PillBadge(text = stringResource(R.string.devices_this_device))
            }
            mediumDate(device.createdAt)?.let { added ->
                Text(
                    text = stringResource(R.string.devices_added, added),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    fontSize = 13.sp,
                )
            }
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp), verticalAlignment = Alignment.CenterVertically) {
            PillButton(
                label = stringResource(R.string.devices_rename),
                onClick = { if (!locked) onRename() },
                compact = true,
            )
            GhostButton(
                label = stringResource(R.string.devices_remove),
                onClick = { if (!locked) onRemove() },
                enabled = !device.current,
                destructive = true,
                compact = true,
            )
            if (busy) {
                CircularProgressIndicator(
                    modifier = Modifier.size(20.dp),
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    strokeWidth = 2.dp,
                )
            }
        }
        if (device.current) InfoNote(text = stringResource(R.string.devices_current_note))
    }
}

@Composable
private fun AccountActions(screenScope: CoroutineScope, onClose: () -> Unit) {
    val signingOut by SessionRepository.signingOut.collectAsState()
    val deleting by SessionRepository.deletingAccount.collectAsState()
    var confirmingSignOut by remember { mutableStateOf(false) }
    var confirmingDelete by remember { mutableStateOf(false) }
    val signOutTitle = stringResource(R.string.settings_signout)
    val deleteTitle = stringResource(R.string.settings_delete_account)
    val deleted = stringResource(R.string.settings_delete_done)
    val deleteFailed = stringResource(R.string.settings_delete_failed)

    TvActionRow(
        icon = Icons.AutoMirrored.Outlined.Logout,
        title = signOutTitle,
        onClick = { confirmingSignOut = true },
        modifier = Modifier.padding(top = 8.dp),
        tint = StateError,
        loading = signingOut,
        enabled = !deleting,
    )
    TvActionRow(
        icon = Icons.Outlined.Delete,
        title = deleteTitle,
        onClick = { confirmingDelete = true },
        tint = StateError,
        loading = deleting,
        enabled = !signingOut,
    )

    if (confirmingSignOut) {
        TvConfirmDialog(
            title = stringResource(R.string.settings_home_signout_title),
            message = stringResource(R.string.settings_home_signout_body),
            confirmLabel = signOutTitle,
            destructive = true,
            onDismiss = { confirmingSignOut = false },
            // Sign-out clears the session, which also swaps the screen under settings to sign-in.
            onConfirm = {
                screenScope.launch {
                    SessionRepository.signOut()
                    onClose()
                }
            },
        )
    }
    if (confirmingDelete) {
        TvConfirmDialog(
            title = stringResource(R.string.settings_delete_title),
            message = stringResource(R.string.settings_delete_body_android),
            confirmLabel = deleteTitle,
            destructive = true,
            onDismiss = { confirmingDelete = false },
            onConfirm = {
                screenScope.launch {
                    val done = SessionRepository.deleteAccount()
                    if (done) ToastCenter.show(deleted, ToastStyle.SUCCESS) else ToastCenter.show(deleteFailed, ToastStyle.WARNING)
                    if (SessionRepository.session.value == null) onClose()
                }
            },
        )
    }
}
