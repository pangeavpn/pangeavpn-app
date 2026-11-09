package org.pangeavpn.tv.ui.settings

import android.app.Activity
import android.content.Context
import android.content.ContextWrapper
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.selection.selectableGroup
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.AppLocale
import org.pangeavpn.core.ThemeChoice
import org.pangeavpn.core.diagnostics.DiagnosticsReport
import org.pangeavpn.core.diagnostics.DiagnosticsStore
import org.pangeavpn.core.diagnostics.ReportPhase
import org.pangeavpn.core.settings.AppLanguages
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.AccentSwatches
import org.pangeavpn.ui.components.KeyValueRow
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.text.reportFailureText
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateError
import org.pangeavpn.ui.theme.themeChoiceTitle

// The address desktop ships as its support contact; shown as text, since a TV has no mail app.
private const val SUPPORT_EMAIL = "support@pangeavpn.org"

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}

@Composable
internal fun LanguageSection() {
    val context = LocalContext.current
    // null follows the system; a tag the app has no strings for reads as English, as on iOS.
    var selected by remember { mutableStateOf(AppLocale.current(context)?.let { AppLanguages.forTag(it).code }) }

    fun choose(code: String?) {
        if (code == selected) return
        val activity = context.findActivity() ?: return
        selected = code
        AppLocale.set(activity, code)
    }

    Column(modifier = Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        TvChoiceRow(
            label = stringResource(R.string.language_android_system),
            selected = selected == null,
            onClick = { choose(null) },
        )
        AppLanguages.supported.forEach { language ->
            TvChoiceRow(
                label = language.name,
                selected = selected == language.code,
                onClick = { choose(language.code) },
            )
        }
    }
}

@Composable
internal fun AppearanceSection() {
    val appearance = LocalAppearance.current

    TvGroupLabel(stringResource(R.string.appearance_theme_heading))
    Column(modifier = Modifier.selectableGroup(), verticalArrangement = Arrangement.spacedBy(8.dp)) {
        ThemeChoice.entries.forEach { choice ->
            TvChoiceRow(
                label = themeChoiceTitle(choice),
                selected = appearance.theme == choice,
                onClick = { appearance.set(choice) },
            )
        }
    }
    TvGroupLabel(stringResource(R.string.appearance_accent_heading))
    AccentSwatches(selected = appearance.accent, onSelect = { appearance.setAccent(it) })
}

/** Send Report in place rather than in a sheet: compose, then the code or why it failed. */
@Composable
internal fun SupportSection() {
    val phase by DiagnosticsStore.report.collectAsState()
    var note by rememberSaveable { mutableStateOf("") }
    var editing by remember { mutableStateOf(false) }
    val noteLabel = stringResource(R.string.support_note_label)
    val sending = phase == ReportPhase.Sending

    when (val current = phase) {
        ReportPhase.Compose, ReportPhase.Sending -> {
            TvParagraph(stringResource(R.string.support_report_body))
            TvParagraph(stringResource(R.string.support_report_anonymous))
            TvFieldRow(
                label = noteLabel,
                value = note.ifEmpty { stringResource(R.string.support_note_placeholder) },
                onClick = { if (!sending) editing = true },
                muted = note.isEmpty(),
                ltr = false,
            )
        }
        is ReportPhase.Sent -> {
            OutcomeHeading(stringResource(R.string.support_sent_title), Icons.Rounded.CheckCircle, StateConnected)
            Text(
                text = current.code,
                modifier = Modifier.fillMaxWidth().tvCard().padding(vertical = 18.dp, horizontal = 12.dp),
                style = ltrStyle(),
                color = MaterialTheme.colorScheme.onSurface,
                fontFamily = FontFamily.Monospace,
                fontSize = 30.sp,
                fontWeight = FontWeight.SemiBold,
                textAlign = TextAlign.Center,
                maxLines = 1,
            )
            TvParagraph(stringResource(R.string.support_sent_body))
            Column(modifier = Modifier.fillMaxWidth().tvCard()) {
                KeyValueRow(label = stringResource(R.string.support_contact), value = SUPPORT_EMAIL)
            }
        }
        is ReportPhase.Failed -> {
            OutcomeHeading(stringResource(R.string.support_failed_title), Icons.Rounded.Warning, StateError)
            TvParagraph(reportFailureText(current.reason))
        }
    }
    // One button in one place for every phase, so the remote's focus stays on it as the phase moves on.
    PillButton(
        label = stringResource(
            when (phase) {
                ReportPhase.Compose -> R.string.support_send
                ReportPhase.Sending -> R.string.support_sending
                is ReportPhase.Sent -> R.string.settings_done
                is ReportPhase.Failed -> R.string.support_try_again
            },
        ),
        onClick = {
            when (phase) {
                ReportPhase.Compose -> DiagnosticsStore.sendReport(note)
                ReportPhase.Sending -> Unit
                is ReportPhase.Sent -> {
                    note = ""
                    DiagnosticsStore.resetReport()
                }
                is ReportPhase.Failed -> DiagnosticsStore.resetReport()
            }
        },
        loading = sending,
        modifier = Modifier.padding(top = 4.dp),
    )

    if (editing) {
        TvEditDialog(
            title = noteLabel,
            initial = note,
            onSave = {
                note = it
                editing = false
            },
            onDismiss = { editing = false },
            placeholder = stringResource(R.string.support_note_placeholder),
            transform = DiagnosticsReport::limited,
            counter = { "${DiagnosticsReport.noteLength(it)}/${DiagnosticsReport.NOTE_LIMIT}" },
            ltr = false,
        )
    }
}

@Composable
private fun OutcomeHeading(text: String, icon: ImageVector, tint: Color) {
    Row(
        modifier = Modifier.padding(horizontal = 4.dp, vertical = 4.dp),
        verticalAlignment = Alignment.CenterVertically,
        horizontalArrangement = Arrangement.spacedBy(10.dp),
    ) {
        Icon(imageVector = icon, contentDescription = null, modifier = Modifier.size(24.dp), tint = tint)
        Text(
            text = text,
            modifier = Modifier.semantics { heading() },
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 20.sp,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
internal fun AboutSection(build: AppBuild) {
    TvGroupLabel(stringResource(R.string.update_installed_heading))
    Column(modifier = Modifier.fillMaxWidth().tvCard()) {
        KeyValueRow(label = stringResource(R.string.update_version), value = build.version)
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(1.dp)
                .background(MaterialTheme.colorScheme.outlineVariant),
        )
        KeyValueRow(label = stringResource(R.string.update_build), value = build.build)
    }
}
