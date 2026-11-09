package org.pangeavpn.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.clearAndSetSemantics
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

/** A saved sign-in the hub could not confirm at launch: offer a retry, never a blank sign-in form. */
@Composable
fun RestoreFailedScreen() {
    val restoring by SessionRepository.restoring.collectAsState()
    val signingOut by SessionRepository.signingOut.collectAsState()
    var retrying by remember { mutableStateOf(false) }
    val scope = rememberCoroutineScope()
    val busy = retrying || restoring || signingOut

    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        Column(
            modifier = Modifier
                .fillMaxWidth()
                .verticalScroll(rememberScrollState())
                .heightIn(min = maxHeight)
                .padding(horizontal = 24.dp),
            horizontalAlignment = Alignment.CenterHorizontally,
            verticalArrangement = Arrangement.Center,
        ) {
            Column(
                modifier = Modifier.widthIn(max = 360.dp),
                horizontalAlignment = Alignment.CenterHorizontally,
                verticalArrangement = Arrangement.spacedBy(10.dp),
            ) {
                BrandLogo(size = 56.dp, modifier = Modifier.clearAndSetSemantics {})
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
                    lineHeight = 21.sp,
                    textAlign = TextAlign.Center,
                )
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
                    enabled = !busy,
                    loading = retrying || restoring,
                    modifier = Modifier.padding(top = 14.dp),
                )
                GhostButton(
                    label = stringResource(R.string.settings_signout),
                    onClick = { scope.launch { SessionRepository.signOut() } },
                    enabled = !busy,
                )
            }
        }
    }
}
