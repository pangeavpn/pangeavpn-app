package org.pangeavpn.tv.ui

import android.os.SystemClock
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.HourglassEmpty
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.TextStyle
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.MainScope
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.session.EXPIRED_CHECK_HOLD_MS
import org.pangeavpn.core.session.ExpiredVerdict
import org.pangeavpn.tv.BuildConfig
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.expiredRanOutText
import org.pangeavpn.ui.text.expiredVerdictText
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnecting

private const val BILLING_ADDRESS = "pangeavpn.org/app/billing"

// Desktop's amber; light mode needs the darker shade to stay legible on cream.
private val AmberLight = Color(0xFFB45309)

// Outlives the panel, as an iOS Task does: a restored account swaps the panel out before its verdict toast.
private val checkScope = MainScope()

/** iOS ExpiredPanel: no in-app purchase on Android, and a website link only in builds that allow one. */
@Composable
fun TvExpiredPanel(modifier: Modifier = Modifier) {
    val subscription by SessionRepository.subscription.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    var checking by remember { mutableStateOf(false) }
    val context = LocalContext.current
    val resources = context.resources
    val checkAgain = remember { FocusRequester() }
    val billing = remember(context) { if (BuildConfig.WEB_BILLING) browserIntent(context, BILLING_URL) else null }
    val amber = if (LocalAppearance.current.dark) StateConnecting else AmberLight
    val accent = MaterialTheme.colorScheme.primary
    val busy = checking || signingOut

    LaunchedEffect(Unit) { checkAgain.tryFocus() }

    Box(modifier = modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
        Column(
            modifier = Modifier.widthIn(max = 420.dp).verticalScroll(rememberScrollState()),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Icon(
                imageVector = Icons.Outlined.HourglassEmpty,
                contentDescription = null,
                modifier = Modifier
                    .size(48.dp)
                    .clip(CircleShape)
                    .background(accent.copy(alpha = 0.14f))
                    .padding(12.dp),
                tint = accent,
            )
            Row(
                modifier = Modifier.padding(top = 4.dp),
                verticalAlignment = Alignment.CenterVertically,
                horizontalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                Box(modifier = Modifier.size(7.dp).clip(CircleShape).background(amber))
                Text(
                    text = stringResource(R.string.subscription_none).uppercase(),
                    color = amber,
                    fontSize = 11.sp,
                    fontWeight = FontWeight.Bold,
                    letterSpacing = 1.6.sp,
                )
            }
            Text(
                text = stringResource(R.string.expired_title),
                modifier = Modifier.semantics { heading() },
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 24.sp,
                fontWeight = FontWeight.SemiBold,
                letterSpacing = (-0.6).sp,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(R.string.expired_body),
                color = LocalTextSecondary.current,
                fontSize = 14.sp,
                lineHeight = 20.sp,
                textAlign = TextAlign.Center,
            )
            expiredRanOutText(subscription?.expiresAt)?.let {
                Text(text = it, color = MaterialTheme.colorScheme.onSurfaceVariant, fontSize = 12.sp)
            }
            Column(
                modifier = Modifier.fillMaxWidth().padding(top = 10.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(8.dp),
            ) {
                if (BuildConfig.WEB_BILLING) {
                    if (billing != null) {
                        PillButton(
                            label = stringResource(R.string.main_expired_billing),
                            onClick = { if (!busy) openIntent(context, billing) },
                        )
                    } else {
                        Text(
                            text = BILLING_ADDRESS,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            style = TextStyle(fontFamily = FontFamily.Monospace, fontSize = 13.sp, textDirection = TextDirection.Ltr),
                        )
                    }
                }
                // Busy presses are ignored rather than disabled, so the remote's focus stays on the button.
                GhostButton(
                    label = stringResource(R.string.expired_check_again),
                    onClick = {
                        if (!busy) {
                            checking = true
                            checkScope.launch {
                                val started = SystemClock.elapsedRealtime()
                                val verdict = SessionRepository.checkSubscription()
                                val remaining = EXPIRED_CHECK_HOLD_MS - (SystemClock.elapsedRealtime() - started)
                                if (remaining > 0) delay(remaining)
                                checking = false
                                ToastCenter.show(expiredVerdictText(resources, verdict), verdictStyle(verdict))
                            }
                        }
                    },
                    loading = checking,
                    modifier = Modifier.focusRequester(checkAgain),
                )
                GhostButton(
                    label = stringResource(R.string.expired_sign_out),
                    onClick = { if (!busy) checkScope.launch { SessionRepository.signOut() } },
                    loading = signingOut,
                )
            }
        }
    }
}

private fun verdictStyle(verdict: ExpiredVerdict): ToastStyle = when (verdict) {
    ExpiredVerdict.RESTORED -> ToastStyle.SUCCESS
    ExpiredVerdict.STILL_EXPIRED -> ToastStyle.INFO
    ExpiredVerdict.CHECK_FAILED -> ToastStyle.WARNING
}
