package org.pangeavpn.app.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
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
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.PanTool
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material.icons.rounded.Close
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.core.session.DataNotice
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.theme.LocalTextSecondary

private const val PRIVACY_URL = "https://pangeavpn.org/legal/privacy"
private val Gutter = 20.dp
private val ReadableWidth = 560.dp

/** What the service keeps and why. First launch ends on Continue; review mode (from Support) has a close button. */
@Composable
fun DataNoticeScreen(review: Boolean, onDone: () -> Unit) {
    if (review) BackHandler(onBack = onDone)

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        if (review) {
            Row(
                modifier = Modifier.fillMaxWidth().padding(horizontal = 8.dp).padding(top = 8.dp),
                horizontalArrangement = Arrangement.End,
            ) {
                IconAction(
                    icon = Icons.Rounded.Close,
                    label = stringResource(R.string.settings_close),
                    onClick = onDone,
                    tint = MaterialTheme.colorScheme.onSurface,
                )
            }
        }
        Column(
            modifier = Modifier
                .weight(1f)
                .fillMaxWidth()
                .verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.CenterHorizontally,
        ) {
            NoticeContent(review)
        }
        if (!review) {
            PillButton(
                label = stringResource(R.string.data_notice_continue),
                onClick = {
                    DataNotice.accept()
                    onDone()
                },
                modifier = Modifier
                    .widthIn(max = ReadableWidth)
                    .padding(horizontal = Gutter)
                    .padding(top = 10.dp, bottom = 16.dp),
            )
        }
    }
}

@Composable
private fun NoticeContent(review: Boolean) {
    val uriHandler = LocalUriHandler.current

    Column(
        modifier = Modifier
            .widthIn(max = ReadableWidth)
            .fillMaxWidth()
            .padding(horizontal = Gutter)
            .padding(top = if (review) 4.dp else 40.dp, bottom = 12.dp),
    ) {
        SectionLabel(stringResource(R.string.data_notice_kicker))
        Text(
            text = stringResource(R.string.data_notice_title),
            modifier = Modifier.padding(top = 6.dp).semantics { heading() },
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 30.sp,
            lineHeight = 36.sp,
            fontWeight = FontWeight.Light,
            letterSpacing = (-0.8).sp,
        )
        Text(
            text = stringResource(R.string.data_notice_intro),
            modifier = Modifier.padding(top = 8.dp),
            color = LocalTextSecondary.current,
            fontSize = 14.sp,
            lineHeight = 20.sp,
        )
        Column(
            modifier = Modifier.padding(top = 24.dp),
            verticalArrangement = Arrangement.spacedBy(20.dp),
        ) {
            NoticeRow(Icons.Outlined.Inbox, R.string.data_notice_keep_title, R.string.data_notice_keep_body_android)
            NoticeRow(Icons.Outlined.VisibilityOff, R.string.data_notice_never_title, R.string.data_notice_never_body)
            NoticeRow(Icons.AutoMirrored.Outlined.Send, R.string.data_notice_reports_title, R.string.data_notice_reports_body)
            NoticeRow(Icons.Outlined.PanTool, R.string.data_notice_unsold_title, R.string.data_notice_unsold_body)
        }
        Box(
            modifier = Modifier
                .padding(top = 14.dp)
                .heightIn(min = 48.dp)
                .clickable(role = Role.Button) { runCatching { uriHandler.openUri(PRIVACY_URL) } },
            contentAlignment = Alignment.CenterStart,
        ) {
            Text(
                text = stringResource(R.string.data_notice_privacy_link),
                color = MaterialTheme.colorScheme.primary,
                fontSize = 13.sp,
                fontWeight = FontWeight.SemiBold,
            )
        }
    }
}

@Composable
private fun NoticeRow(icon: ImageVector, title: Int, detail: Int) {
    Row(
        modifier = Modifier.fillMaxWidth().semantics(mergeDescendants = true) {},
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Box(
            modifier = Modifier
                .size(36.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(MaterialTheme.colorScheme.primary.copy(alpha = 0.12f)),
            contentAlignment = Alignment.Center,
        ) {
            Icon(
                imageVector = icon,
                contentDescription = null,
                modifier = Modifier.size(18.dp),
                tint = MaterialTheme.colorScheme.primary,
            )
        }
        Column(
            modifier = Modifier.weight(1f),
            verticalArrangement = Arrangement.spacedBy(4.dp),
        ) {
            Text(
                text = stringResource(title),
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = stringResource(detail),
                color = LocalTextSecondary.current,
                fontSize = 14.sp,
                lineHeight = 20.sp,
            )
        }
    }
}
