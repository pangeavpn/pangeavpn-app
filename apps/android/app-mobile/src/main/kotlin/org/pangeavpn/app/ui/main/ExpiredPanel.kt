package org.pangeavpn.app.ui.main

import android.os.SystemClock
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.HourglassEmpty
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.session.EXPIRED_CHECK_HOLD_MS
import org.pangeavpn.core.session.ExpiredVerdict
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.expiredRanOutText
import org.pangeavpn.ui.text.expiredVerdictText
import org.pangeavpn.ui.theme.LocalAppearance
import org.pangeavpn.ui.theme.LocalTextSecondary

private const val BILLING_URL = "https://pangeavpn.org/app/billing"

// A restored plan removes the panel mid-check; the verdict toast must still land.
private val panelScope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

/** Takes the hero's place once the hub says the plan is over. Play builds carry no billing link. */
@Composable
internal fun ExpiredPanel(modifier: Modifier = Modifier) {
    val subscription by SessionRepository.subscription.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    var checking by remember { mutableStateOf(false) }
    val busy = checking || signingOut
    val resources = LocalContext.current.resources
    val uriHandler = LocalUriHandler.current
    val accent = MaterialTheme.colorScheme.primary
    // Desktop's amber; light mode needs the darker shade to stay legible on cream.
    val warning = if (LocalAppearance.current.dark) Color(0xFFF59E0B) else Color(0xFFB45309)

    val recheck = {
        if (!busy) {
            checking = true
            panelScope.launch {
                val started = SystemClock.elapsedRealtime()
                val verdict = SessionRepository.checkSubscription()
                val remaining = EXPIRED_CHECK_HOLD_MS - (SystemClock.elapsedRealtime() - started)
                if (remaining > 0) delay(remaining)
                checking = false
                ToastCenter.show(expiredVerdictText(resources, verdict), verdict.toastStyle())
            }
        }
    }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .padding(vertical = 12.dp),
        horizontalAlignment = Alignment.CenterHorizontally,
    ) {
        Box(
            modifier = Modifier
                .size(48.dp)
                .background(accent.copy(alpha = 0.14f), CircleShape),
            contentAlignment = Alignment.Center,
        ) {
            Icon(Icons.Rounded.HourglassEmpty, contentDescription = null, modifier = Modifier.size(22.dp), tint = accent)
        }
        Row(
            modifier = Modifier.padding(top = 12.dp),
            verticalAlignment = Alignment.CenterVertically,
            horizontalArrangement = Arrangement.spacedBy(8.dp),
        ) {
            Box(Modifier.size(7.dp).background(warning, CircleShape))
            Text(
                text = stringResource(R.string.subscription_none).uppercase(),
                color = warning,
                fontSize = 11.sp,
                lineHeight = 14.sp,
                fontWeight = FontWeight.Bold,
                letterSpacing = 1.6.sp,
            )
        }
        Text(
            text = stringResource(R.string.expired_title),
            modifier = Modifier
                .padding(top = 8.dp)
                .semantics { heading() },
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 24.sp,
            lineHeight = 30.sp,
            fontWeight = FontWeight.SemiBold,
            letterSpacing = (-0.6).sp,
            textAlign = TextAlign.Center,
        )
        Text(
            text = stringResource(R.string.expired_body),
            modifier = Modifier.padding(top = 8.dp),
            color = LocalTextSecondary.current,
            fontSize = 14.sp,
            lineHeight = 20.sp,
            textAlign = TextAlign.Center,
        )
        expiredRanOutText(subscription?.expiresAt)?.let {
            Text(
                text = it,
                modifier = Modifier.padding(top = 8.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                lineHeight = 16.sp,
                textAlign = TextAlign.Center,
            )
        }
        Column(
            modifier = Modifier
                .padding(top = 18.dp)
                .fillMaxWidth(),
            verticalArrangement = Arrangement.spacedBy(6.dp),
        ) {
            val checkLabel = stringResource(R.string.expired_check_again)
            if (BuildConfig.WEB_BILLING) {
                PillButton(
                    label = stringResource(R.string.main_expired_billing),
                    onClick = { runCatching { uriHandler.openUri(BILLING_URL) } },
                    enabled = !busy,
                )
                GhostButton(
                    label = checkLabel,
                    onClick = recheck,
                    enabled = !busy,
                    loading = checking,
                    modifier = Modifier.padding(top = 4.dp),
                )
            } else {
                PillButton(label = checkLabel, onClick = recheck, enabled = !busy, loading = checking)
            }
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = 48.dp)
                    .alpha(if (signingOut) 0.5f else 1f)
                    .clip(CircleShape)
                    .clickable(enabled = !busy, role = Role.Button) { panelScope.launch { SessionRepository.signOut() } },
                contentAlignment = Alignment.Center,
            ) {
                Text(
                    text = stringResource(R.string.expired_sign_out),
                    color = LocalTextSecondary.current,
                    fontSize = 15.sp,
                    lineHeight = 20.sp,
                    fontWeight = FontWeight.SemiBold,
                )
            }
        }
    }
}

private fun ExpiredVerdict.toastStyle(): ToastStyle = when (this) {
    ExpiredVerdict.RESTORED -> ToastStyle.SUCCESS
    ExpiredVerdict.STILL_EXPIRED -> ToastStyle.INFO
    ExpiredVerdict.CHECK_FAILED -> ToastStyle.WARNING
}
