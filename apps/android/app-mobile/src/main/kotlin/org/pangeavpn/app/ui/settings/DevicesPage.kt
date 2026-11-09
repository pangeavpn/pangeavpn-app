package org.pangeavpn.app.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
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
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.viewmodel.compose.viewModel
import kotlinx.coroutines.launch
import org.pangeavpn.core.model.Device
import org.pangeavpn.core.session.DeviceRename
import org.pangeavpn.core.session.shownName
import org.pangeavpn.core.viewmodel.DevicesViewModel
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ConfirmDialog
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.PangeaDialog
import org.pangeavpn.ui.components.PillBadge
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SettingsField
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.deviceErrorText
import org.pangeavpn.ui.text.mediumDate
import org.pangeavpn.ui.theme.LocalTextSecondary

/** The account's devices, current first: rename any, remove any but this one, one operation at a time. */
@Composable
internal fun DevicesPage(onBack: () -> Unit) {
    val model: DevicesViewModel = viewModel(key = "settings-devices")
    val state by model.state.collectAsState()
    val scope = rememberCoroutineScope()
    val resources = LocalContext.current.resources
    var renaming by remember { mutableStateOf<Device?>(null) }
    var draft by remember { mutableStateOf("") }
    var removing by remember { mutableStateOf<Device?>(null) }
    val renamedText = stringResource(R.string.devices_renamed)
    val removedText = stringResource(R.string.devices_removed)

    LaunchedEffect(Unit) { model.load() }

    // A refusal also stays in the model's error, which the page shows above the list.
    fun showFailure() {
        val error = model.state.value.error
        val text = if (error != null) deviceErrorText(resources, error) else resources.getString(R.string.network_update_failed)
        ToastCenter.show(text, ToastStyle.WARNING)
    }

    fun save(device: Device) {
        val name = DeviceRename.validated(draft) ?: return
        renaming = null
        scope.launch {
            if (model.rename(device.id, name)) ToastCenter.show(renamedText, ToastStyle.SUCCESS) else showFailure()
        }
    }

    // The current device leaves only by signing out, which also frees its slot.
    fun remove(device: Device) {
        if (state.busyId != null || device.current) return
        scope.launch {
            if (model.remove(device.id)) ToastCenter.show(removedText, ToastStyle.SUCCESS) else showFailure()
        }
    }

    SettingsPage(
        title = stringResource(R.string.devices_title),
        onBack = onBack,
        description = stringResource(R.string.devices_subtitle),
    ) {
        state.error?.let { error ->
            InfoNote(text = deviceErrorText(error), tone = NoteTone.ERROR)
            GhostButton(
                label = stringResource(R.string.restore_failed_retry),
                onClick = { scope.launch { model.load() } },
            )
        }
        val devices = state.devices
        when {
            devices.isEmpty() && (state.loading || !state.loaded) -> Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 40.dp),
                contentAlignment = Alignment.Center,
            ) {
                CircularProgressIndicator(color = MaterialTheme.colorScheme.primary)
            }
            devices.isEmpty() && state.error == null -> Text(
                text = stringResource(R.string.devices_none),
                modifier = Modifier
                    .fillMaxWidth()
                    .padding(vertical = 40.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 15.sp,
                textAlign = TextAlign.Center,
            )
            devices.isNotEmpty() -> Column(verticalArrangement = Arrangement.spacedBy(10.dp)) {
                devices.forEach { device ->
                    DeviceCard(
                        device = device,
                        busy = state.busyId == device.id,
                        enabled = state.busyId == null,
                        onRename = {
                            draft = DeviceRename.clamped(device.friendlyName.orEmpty())
                            renaming = device
                        },
                        onRemove = { removing = device },
                    )
                }
            }
        }
    }

    renaming?.let { device ->
        RenameDialog(
            draft = draft,
            onDraftChange = { draft = DeviceRename.clamped(it) },
            onSave = { save(device) },
            onDismiss = { renaming = null },
        )
    }
    removing?.let { device ->
        ConfirmDialog(
            title = stringResource(R.string.devices_remove_title, device.shownName ?: stringResource(R.string.devices_unnamed)),
            message = stringResource(R.string.devices_remove_body),
            confirmLabel = stringResource(R.string.devices_remove),
            onConfirm = { remove(device) },
            onDismiss = { removing = null },
            destructive = true,
            cancelLabel = stringResource(R.string.devices_rename_cancel),
        )
    }
}

@OptIn(ExperimentalLayoutApi::class)
@Composable
private fun DeviceCard(
    device: Device,
    busy: Boolean,
    enabled: Boolean,
    onRename: () -> Unit,
    onRemove: () -> Unit,
) {
    PangeaCard(padding = PaddingValues(16.dp)) {
        Column(
            modifier = Modifier.semantics(mergeDescendants = true) {},
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Row(verticalAlignment = Alignment.CenterVertically, horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                Text(
                    text = device.shownName ?: stringResource(R.string.devices_unnamed),
                    modifier = Modifier.weight(1f, fill = false),
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                    maxLines = 2,
                    overflow = TextOverflow.Ellipsis,
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
        FlowRow(
            modifier = Modifier.padding(top = 12.dp),
            horizontalArrangement = Arrangement.spacedBy(8.dp),
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            PillButton(
                label = stringResource(R.string.devices_rename),
                onClick = onRename,
                enabled = enabled,
                compact = true,
            )
            GhostButton(
                label = stringResource(R.string.devices_remove),
                onClick = onRemove,
                enabled = enabled && !device.current,
                destructive = true,
                compact = true,
            )
            if (busy) {
                Box(modifier = Modifier.size(48.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(20.dp),
                        color = LocalTextSecondary.current,
                        strokeWidth = 2.dp,
                    )
                }
            }
        }
        if (device.current) {
            InfoNote(text = stringResource(R.string.devices_current_note), modifier = Modifier.padding(top = 12.dp))
        }
    }
}

@Composable
private fun RenameDialog(
    draft: String,
    onDraftChange: (String) -> Unit,
    onSave: () -> Unit,
    onDismiss: () -> Unit,
) {
    val valid = DeviceRename.validated(draft) != null
    val label = stringResource(R.string.devices_rename_label)
    val focus = remember { FocusRequester() }

    PangeaDialog(title = stringResource(R.string.devices_rename), onDismiss = onDismiss) {
        // In the dialog's own composition, so the field is attached before it is asked to take focus.
        LaunchedEffect(Unit) { focus.requestFocus() }
        SettingsField(
            value = draft,
            onValueChange = onDraftChange,
            placeholder = label,
            label = label,
            keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Words, imeAction = ImeAction.Done),
            keyboardActions = KeyboardActions(onDone = { if (valid) onSave() }),
            modifier = Modifier.focusRequester(focus),
        )
        PillButton(label = stringResource(R.string.devices_rename_save), onClick = onSave, enabled = valid)
        GhostButton(label = stringResource(R.string.devices_rename_cancel), onClick = onDismiss)
    }
}
