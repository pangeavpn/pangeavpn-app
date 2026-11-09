package org.pangeavpn.app.ui.settings

import android.content.Intent
import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.slideInVertically
import androidx.compose.animation.slideOutVertically
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Description
import androidx.compose.material.icons.outlined.Email
import androidx.compose.material.icons.outlined.PanTool
import androidx.compose.material.icons.outlined.Policy
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.input.pointer.pointerInput
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import org.pangeavpn.app.ui.DataNoticeScreen
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.theme.Motion

@Composable
internal fun SupportPage(onBack: () -> Unit) {
    val context = LocalContext.current
    var showReport by rememberSaveable { mutableStateOf(false) }
    var showNotice by rememberSaveable { mutableStateOf(false) }
    val noMailApp = stringResource(R.string.support_no_mail_app, SUPPORT_EMAIL)

    Box(modifier = Modifier.fillMaxSize()) {
        SettingsPage(
            title = stringResource(R.string.settings_section_support),
            onBack = onBack,
            description = stringResource(R.string.support_description),
        ) {
            SettingsGroup {
                SettingsNavRow(
                    icon = Icons.AutoMirrored.Outlined.Send,
                    title = stringResource(R.string.support_send_report),
                    onClick = { showReport = true },
                )
                SettingsNavRow(
                    icon = Icons.Outlined.Email,
                    title = stringResource(R.string.support_contact),
                    summary = SUPPORT_EMAIL,
                    onClick = {
                        if (!openLink(context, "mailto:$SUPPORT_EMAIL", Intent.ACTION_SENDTO)) {
                            ToastCenter.show(noMailApp, ToastStyle.WARNING)
                        }
                    },
                )
            }
            SettingsGroup {
                SettingsNavRow(
                    icon = Icons.Outlined.PanTool,
                    title = stringResource(R.string.support_privacy_data),
                    onClick = { showNotice = true },
                )
                SettingsNavRow(
                    icon = Icons.Outlined.Policy,
                    title = stringResource(R.string.support_privacy_policy),
                    onClick = { openLink(context, PRIVACY_URL) },
                )
                SettingsNavRow(
                    icon = Icons.Outlined.Description,
                    title = stringResource(R.string.support_terms),
                    onClick = { openLink(context, TERMS_URL) },
                )
            }
        }

        AnimatedVisibility(
            visible = showReport,
            enter = slideInVertically(Motion.screen()) { it / 3 } + fadeIn(Motion.screen()),
            exit = slideOutVertically(Motion.screen()) { it / 3 } + fadeOut(Motion.screen()),
        ) {
            SendReportSheet(onClose = { showReport = false })
        }

        if (showNotice) {
            // Touch-absorbing, so nothing on the page beneath it can be reached; the notice handles back.
            Box(
                modifier = Modifier
                    .fillMaxSize()
                    .pointerInput(Unit) {},
            ) {
                DataNoticeScreen(review = true, onDone = { showNotice = false })
            }
        }
    }
}
