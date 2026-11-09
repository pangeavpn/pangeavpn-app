package org.pangeavpn.app.ui.settings

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.BasicTextField
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.CheckCircle
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material.icons.rounded.Warning
import androidx.compose.material3.Icon
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.SolidColor
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardCapitalization
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import org.pangeavpn.core.diagnostics.DiagnosticsReport
import org.pangeavpn.core.diagnostics.DiagnosticsStore
import org.pangeavpn.core.diagnostics.ReportFailure
import org.pangeavpn.core.diagnostics.ReportPhase
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.PangeaCardShape
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.text.reportFailureText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnected
import org.pangeavpn.ui.theme.StateError

// The Copy Code button itself confirms the copy, as on desktop.
private const val COPIED_FOR_MS = 1_600L

/** Desktop's Send Diagnostics dialog: explain, take an optional note, then show the code or why not.
 *  It cannot be closed while the report is sending. */
@Composable
internal fun SendReportSheet(onClose: () -> Unit) {
    val phase by DiagnosticsStore.report.collectAsState()
    var note by rememberSaveable { mutableStateOf("") }
    val sending = phase == ReportPhase.Sending
    val close = {
        DiagnosticsStore.resetReport()
        onClose()
    }

    BackHandler { if (!sending) close() }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .pointerInput(Unit) {}
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        Row(
            modifier = Modifier
                .fillMaxWidth()
                .heightIn(min = 56.dp)
                .padding(start = 16.dp, end = 4.dp),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text(
                text = stringResource(R.string.support_send_report),
                modifier = Modifier.weight(1f).semantics { heading() },
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 22.sp,
                fontWeight = FontWeight.Light,
                letterSpacing = (-0.4).sp,
                maxLines = 1,
                overflow = TextOverflow.Ellipsis,
            )
            IconAction(
                icon = Icons.Rounded.Close,
                label = stringResource(R.string.settings_close),
                onClick = close,
                enabled = !sending,
                tint = MaterialTheme.colorScheme.onSurface,
            )
        }
        Column(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .wrapContentWidth()
                .widthIn(max = 640.dp)
                .fillMaxWidth()
                .padding(start = 16.dp, top = 8.dp, end = 16.dp, bottom = 24.dp),
            verticalArrangement = Arrangement.spacedBy(16.dp),
        ) {
            when (val shown = phase) {
                ReportPhase.Compose, ReportPhase.Sending -> NoteStep(
                    note = note,
                    onNoteChange = { note = DiagnosticsReport.limited(it) },
                    sending = sending,
                    onSend = { DiagnosticsStore.sendReport(note) },
                    onCancel = close,
                )
                is ReportPhase.Sent -> SentStep(code = shown.code, onClose = close)
                is ReportPhase.Failed -> FailedStep(
                    reason = shown.reason,
                    onRetry = { DiagnosticsStore.resetReport() },
                    onClose = close,
                )
            }
        }
    }
}

@Composable
private fun NoteStep(
    note: String,
    onNoteChange: (String) -> Unit,
    sending: Boolean,
    onSend: () -> Unit,
    onCancel: () -> Unit,
) {
    val label = stringResource(R.string.support_note_label)
    Paragraph(stringResource(R.string.support_report_body))
    Paragraph(stringResource(R.string.support_report_anonymous))
    Column(verticalArrangement = Arrangement.spacedBy(6.dp)) {
        Text(
            text = label,
            color = LocalTextSecondary.current,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
        )
        NoteField(note = note, onNoteChange = onNoteChange, label = label, enabled = !sending)
        Text(
            text = "${DiagnosticsReport.noteLength(note)}/${DiagnosticsReport.NOTE_LIMIT}",
            modifier = Modifier.fillMaxWidth(),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            fontFamily = FontFamily.Monospace,
            fontSize = 12.sp,
            textAlign = TextAlign.End,
        )
    }
    Column(modifier = Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        PillButton(
            label = stringResource(if (sending) R.string.support_sending else R.string.support_send),
            onClick = onSend,
            enabled = !sending,
            loading = sending,
        )
        GhostButton(label = stringResource(R.string.support_cancel), onClick = onCancel, enabled = !sending)
    }
}

@Composable
private fun NoteField(note: String, onNoteChange: (String) -> Unit, label: String, enabled: Boolean) {
    val style = TextStyle(color = MaterialTheme.colorScheme.onSurface, fontSize = 15.sp, lineHeight = 21.sp)
    BasicTextField(
        value = note,
        onValueChange = onNoteChange,
        modifier = Modifier
            .fillMaxWidth()
            .clip(PangeaCardShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(width = 1.dp, color = MaterialTheme.colorScheme.outlineVariant, shape = PangeaCardShape)
            .semantics { contentDescription = label },
        enabled = enabled,
        textStyle = style,
        keyboardOptions = KeyboardOptions(capitalization = KeyboardCapitalization.Sentences),
        minLines = 3,
        maxLines = 6,
        cursorBrush = SolidColor(MaterialTheme.colorScheme.primary),
        decorationBox = { field ->
            Box(modifier = Modifier.padding(horizontal = 14.dp, vertical = 12.dp)) {
                if (note.isEmpty()) {
                    Text(
                        text = stringResource(R.string.support_note_placeholder),
                        style = style.copy(color = MaterialTheme.colorScheme.onSurfaceVariant),
                    )
                }
                field()
            }
        },
    )
}

@Composable
private fun SentStep(code: String, onClose: () -> Unit) {
    val clipboard = LocalClipboardManager.current
    var copied by remember { mutableStateOf(false) }
    LaunchedEffect(copied) {
        if (copied) {
            delay(COPIED_FOR_MS)
            copied = false
        }
    }

    Heading(stringResource(R.string.support_sent_title), Icons.Rounded.CheckCircle, StateConnected)
    SelectionContainer(
        modifier = Modifier
            .fillMaxWidth()
            .clip(PangeaCardShape)
            .background(MaterialTheme.colorScheme.surface)
            .border(width = 1.dp, color = MaterialTheme.colorScheme.outlineVariant, shape = PangeaCardShape)
            .padding(horizontal = 12.dp, vertical = 18.dp),
    ) {
        Text(
            text = code,
            modifier = Modifier.fillMaxWidth(),
            color = MaterialTheme.colorScheme.onSurface,
            fontFamily = FontFamily.Monospace,
            fontSize = 28.sp,
            fontWeight = FontWeight.SemiBold,
            textAlign = TextAlign.Center,
            maxLines = 1,
            style = LocalTextStyle.current.copy(textDirection = TextDirection.Ltr),
        )
    }
    Paragraph(stringResource(R.string.support_sent_body))
    Column(modifier = Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        PillButton(
            label = stringResource(if (copied) R.string.support_copied else R.string.support_copy_code),
            onClick = {
                clipboard.setText(AnnotatedString(code))
                copied = true
            },
            enabled = !copied,
        )
        GhostButton(label = stringResource(R.string.settings_close), onClick = onClose)
    }
}

@Composable
private fun FailedStep(reason: ReportFailure, onRetry: () -> Unit, onClose: () -> Unit) {
    Heading(stringResource(R.string.support_failed_title), Icons.Rounded.Warning, StateError)
    Paragraph(reportFailureText(reason))
    Column(modifier = Modifier.padding(top = 4.dp), verticalArrangement = Arrangement.spacedBy(10.dp)) {
        PillButton(label = stringResource(R.string.support_try_again), onClick = onRetry)
        GhostButton(label = stringResource(R.string.settings_close), onClick = onClose)
    }
}

@Composable
private fun Heading(text: String, icon: ImageVector, tint: Color) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier.size(26.dp),
            tint = tint,
        )
        Text(
            text = text,
            modifier = Modifier
                .padding(start = 10.dp)
                .semantics { heading() },
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 20.sp,
            fontWeight = FontWeight.Bold,
        )
    }
}

@Composable
private fun Paragraph(text: String) {
    Text(
        text = text,
        color = LocalTextSecondary.current,
        fontSize = 14.sp,
        lineHeight = 20.sp,
    )
}
