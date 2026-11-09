package org.pangeavpn.tv.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.widthIn
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
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.BrandLogo
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.theme.LocalTextSecondary

/** A saved sign-in the hub could not confirm at launch: a retry, never a blank sign-in form. */
@Composable
fun TvRestoreFailedScreen() {
    val restoring by SessionRepository.restoring.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    var retrying by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val retry = remember { FocusRequester() }
    val busy = retrying || restoring || signingOut

    LaunchedEffect(Unit) { retry.tryFocus() }

    Box(modifier = Modifier.fillMaxSize().overscan(), contentAlignment = Alignment.Center) {
        Column(
            modifier = Modifier.widthIn(max = 420.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.spacedBy(10.dp),
        ) {
            BrandLogo(size = 56.dp)
            Text(
                text = stringResource(R.string.restore_failed_title),
                modifier = Modifier.padding(top = 8.dp).semantics { heading() },
                color = MaterialTheme.colorScheme.onSurface,
                fontSize = 24.sp,
                fontWeight = FontWeight.SemiBold,
                letterSpacing = (-0.6).sp,
                textAlign = TextAlign.Center,
            )
            Text(
                text = stringResource(R.string.restore_failed_body),
                color = LocalTextSecondary.current,
                fontSize = 14.sp,
                lineHeight = 20.sp,
                textAlign = TextAlign.Center,
            )
            // Busy taps are ignored rather than disabled, so the remote's focus stays where it is.
            PillButton(
                label = stringResource(R.string.restore_failed_retry),
                onClick = {
                    if (!busy) {
                        retrying = true
                        scope.launch {
                            try {
                                SessionRepository.retryRestore()
                            } finally {
                                retrying = false
                            }
                        }
                    }
                },
                loading = retrying || restoring,
                modifier = Modifier.padding(top = 14.dp).focusRequester(retry),
            )
            GhostButton(
                label = stringResource(R.string.settings_signout),
                onClick = { if (!busy) scope.launch { SessionRepository.signOut() } },
                loading = signingOut,
            )
        }
    }
}
