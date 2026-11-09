package org.pangeavpn.app.ui.settings.pages

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.settings.TransportChoices
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.ChoiceRow
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.transportChoiceDescription
import org.pangeavpn.ui.components.transportChoiceLabel

@Composable
fun TransportPage(onBack: () -> Unit) {
    val error = networkPageError()
    val settings by SettingsRepository.settings.collectAsState()
    val resources = LocalContext.current.resources
    val scope = rememberCoroutineScope()
    // Shows the tap at once, so the list never lags behind the finger while the core saves.
    var pending by remember { mutableStateOf<String?>(null) }
    val selected = pending ?: TransportChoices.selected(settings.preferredTransport)

    fun select(choice: String) {
        if (pending != null || choice == selected) return
        pending = choice
        scope.launch {
            val saved = SettingsRepository.save { it.copy(preferredTransport = choice) }
            pending = null
            reportNetworkSave(resources, saved, resources.getString(R.string.bypass_updated))
        }
    }

    SettingsPage(
        title = stringResource(R.string.settings_transport_heading),
        onBack = onBack,
        description = stringResource(R.string.settings_transport_description),
        error = error,
    ) {
        Column(
            modifier = Modifier.fillMaxWidth().selectableGroup(),
            verticalArrangement = Arrangement.spacedBy(9.dp),
        ) {
            TransportChoices.pickerOrder.forEach { choice ->
                ChoiceRow(
                    label = transportChoiceLabel(choice),
                    selected = choice == selected,
                    onClick = { select(choice) },
                    hint = transportChoiceDescription(choice),
                    enabled = pending == null,
                )
            }
        }
        InfoNote(text = stringResource(R.string.bypass_regions_note))
    }
}
