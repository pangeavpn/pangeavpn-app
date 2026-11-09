package org.pangeavpn.tv.ui

import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.focusable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.Send
import androidx.compose.material.icons.outlined.Inbox
import androidx.compose.material.icons.outlined.PanTool
import androidx.compose.material.icons.outlined.VisibilityOff
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.focus.onFocusChanged
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.theme.LocalTextSecondary

private const val PRIVACY_ADDRESS = "pangeavpn.org/legal/privacy"

/** What the service keeps, before sign-in on first launch; review is the copy settings opens, which closes instead. */
@Composable
fun TvDataNoticeScreen(review: Boolean, onDone: () -> Unit) {
    val done = remember { FocusRequester() }
    LaunchedEffect(Unit) { done.tryFocus() }
    BackHandler(enabled = review, onBack = onDone)

    Row(
        modifier = Modifier.fillMaxSize().overscan(),
        horizontalArrangement = Arrangement.spacedBy(40.dp),
    ) {
        Column(modifier = Modifier.weight(0.4f).fillMaxHeight()) {
            SectionLabel(text = stringResource(R.string.data_notice_kicker))
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
            PrivacyLink(modifier = Modifier.padding(top = 16.dp))
            Spacer(modifier = Modifier.weight(1f))
            PillButton(
                label = stringResource(if (review) R.string.settings_close else R.string.data_notice_continue),
                onClick = onDone,
                modifier = Modifier.padding(top = 12.dp).widthIn(max = 320.dp).focusRequester(done),
            )
        }
        Column(
            modifier = Modifier
                .weight(0.6f)
                .fillMaxHeight()
                .verticalScroll(rememberScrollState()),
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            NoticeRow(
                icon = Icons.Outlined.Inbox,
                title = stringResource(R.string.data_notice_keep_title),
                body = stringResource(R.string.data_notice_keep_body_android),
            )
            NoticeRow(
                icon = Icons.Outlined.VisibilityOff,
                title = stringResource(R.string.data_notice_never_title),
                body = stringResource(R.string.data_notice_never_body),
            )
            NoticeRow(
                icon = Icons.AutoMirrored.Outlined.Send,
                title = stringResource(R.string.data_notice_reports_title),
                body = stringResource(R.string.data_notice_reports_body),
            )
            NoticeRow(
                icon = Icons.Outlined.PanTool,
                title = stringResource(R.string.data_notice_unsold_title),
                body = stringResource(R.string.data_notice_unsold_body),
            )
        }
    }
}

// Focusable although it does nothing on press: it is how a remote scrolls a column that runs long.
@Composable
private fun NoticeRow(icon: ImageVector, title: String, body: String) {
    var focused by remember { mutableStateOf(false) }
    val accent = MaterialTheme.colorScheme.primary
    Row(
        modifier = Modifier
            .fillMaxWidth()
            .tvCard(focused = focused)
            .onFocusChanged { focused = it.isFocused }
            .focusable()
            .semantics(mergeDescendants = true) {}
            .padding(14.dp),
        horizontalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Icon(
            imageVector = icon,
            contentDescription = null,
            modifier = Modifier
                .size(36.dp)
                .clip(RoundedCornerShape(10.dp))
                .background(accent.copy(alpha = 0.12f))
                .padding(8.dp),
            tint = accent,
        )
        Column(modifier = Modifier.weight(1f)) {
            Text(
                text = title,
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 16.sp,
                fontWeight = FontWeight.SemiBold,
            )
            Text(
                text = body,
                modifier = Modifier.padding(top = 4.dp),
                color = LocalTextSecondary.current,
                fontSize = 14.sp,
                lineHeight = 20.sp,
            )
        }
    }
}

// A button where a browser can open the page; elsewhere the address, to visit on another device.
@Composable
private fun PrivacyLink(modifier: Modifier = Modifier) {
    val context = LocalContext.current
    val intent = remember(context) { browserIntent(context, PRIVACY_URL) }
    if (intent != null) {
        GhostButton(
            label = stringResource(R.string.data_notice_privacy_link),
            onClick = { openIntent(context, intent) },
            compact = true,
            modifier = modifier,
        )
        return
    }
    Column(modifier = modifier) {
        Text(
            text = stringResource(R.string.data_notice_privacy_link),
            color = MaterialTheme.colorScheme.primary,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = PRIVACY_ADDRESS,
            modifier = Modifier.padding(top = 2.dp),
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp, textDirection = TextDirection.Ltr),
        )
    }
}
