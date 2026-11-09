package org.pangeavpn.app.ui.settings

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.selection.SelectionContainer
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material.icons.rounded.Refresh
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.LocalTextStyle
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
import androidx.compose.ui.draw.clip
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.diagnostics.DiagnosticsStore
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.PangeaCardShape
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.theme.LocalTextSecondary

private val LogBoxHeight = 240.dp

/** The hidden debugging page. The service runs in this process, so there is one log where iOS has two. */
@Composable
internal fun DeveloperPage(onBack: () -> Unit) {
    val context = LocalContext.current
    val clipboard = LocalClipboardManager.current
    val scope = rememberCoroutineScope()
    val log by DiagnosticsStore.appLog.collectAsState()
    var loading by remember { mutableStateOf(false) }
    val copiedText = stringResource(R.string.support_copied)
    val noLogsText = stringResource(R.string.developer_no_logs)
    val offText = stringResource(R.string.settings_home_developer_off)

    suspend fun refresh() {
        loading = true
        try {
            DiagnosticsStore.refreshLog()
        } finally {
            loading = false
        }
    }

    LaunchedEffect(Unit) { refresh() }

    SettingsPage(
        title = stringResource(R.string.settings_section_developer),
        onBack = onBack,
        description = stringResource(R.string.developer_description),
    ) {
        LogPanel(
            title = stringResource(R.string.developer_app_log),
            text = log,
            emptyText = stringResource(R.string.developer_log_empty),
            loading = loading,
            onRefresh = { scope.launch { refresh() } },
            onCopy = {
                if (log.isEmpty()) {
                    ToastCenter.show(noLogsText, ToastStyle.WARNING)
                } else {
                    clipboard.setText(AnnotatedString(log))
                    ToastCenter.show(copiedText, ToastStyle.SUCCESS)
                }
            },
        )
        SettingsGroup {
            GhostButton(
                label = stringResource(R.string.developer_copy_diagnostics),
                onClick = {
                    scope.launch {
                        clipboard.setText(AnnotatedString(DiagnosticsStore.diagnosticsJson(context)))
                        ToastCenter.show(copiedText, ToastStyle.SUCCESS)
                    }
                },
            )
            InfoNote(text = stringResource(R.string.developer_copy_diagnostics_hint))
        }
        SettingsGroup {
            // Settings leaves this page itself once the mode is off, in either layout.
            GhostButton(
                label = stringResource(R.string.developer_turn_off),
                onClick = {
                    AppPrefs.update { it.copy(developerMode = false) }
                    ToastCenter.show(offText)
                },
            )
        }
    }
}

/** A fixed-height, selectable, monospaced log that opens on its newest line. */
@Composable
private fun LogPanel(
    title: String,
    text: String,
    emptyText: String,
    loading: Boolean,
    onRefresh: () -> Unit,
    onCopy: () -> Unit,
) {
    SettingsGroup {
        Row(verticalAlignment = Alignment.CenterVertically) {
            SectionLabel(text = title, modifier = Modifier.padding(start = 4.dp).weight(1f))
            if (loading) {
                Box(modifier = Modifier.size(48.dp), contentAlignment = Alignment.Center) {
                    CircularProgressIndicator(
                        modifier = Modifier.size(18.dp),
                        color = LocalTextSecondary.current,
                        strokeWidth = 2.dp,
                    )
                }
            } else {
                IconAction(
                    icon = Icons.Rounded.Refresh,
                    label = stringResource(R.string.developer_refresh),
                    onClick = onRefresh,
                    tint = LocalTextSecondary.current,
                )
            }
            IconAction(
                icon = Icons.Outlined.ContentCopy,
                label = stringResource(R.string.developer_copy_logs),
                onClick = onCopy,
                enabled = text.isNotEmpty(),
                tint = LocalTextSecondary.current,
            )
        }
        // Reversed, so a long log opens at its end while a short one still sits at the top.
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(LogBoxHeight)
                .clip(PangeaCardShape)
                .background(MaterialTheme.colorScheme.surface)
                .border(width = 1.dp, color = MaterialTheme.colorScheme.outlineVariant, shape = PangeaCardShape)
                .verticalScroll(rememberScrollState(), reverseScrolling = true),
        ) {
            SelectionContainer {
                Text(
                    text = text.ifEmpty { emptyText },
                    modifier = Modifier
                        .fillMaxWidth()
                        .padding(12.dp),
                    color = if (text.isEmpty()) MaterialTheme.colorScheme.onSurfaceVariant else LocalTextSecondary.current,
                    fontFamily = FontFamily.Monospace,
                    fontSize = 11.sp,
                    lineHeight = 15.sp,
                    style = LocalTextStyle.current.copy(textDirection = TextDirection.Ltr),
                )
            }
        }
    }
}
