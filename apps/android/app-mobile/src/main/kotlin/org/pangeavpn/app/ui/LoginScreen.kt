package org.pangeavpn.app.ui

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawingPadding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.platform.LocalUriHandler
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.text.style.TextDecoration
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.core.viewmodel.LoginUiState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.TokenField
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.LocalThemeToggle
import org.pangeavpn.ui.theme.StateError

private const val ACCOUNT_URL = "https://pangeavpn.org/account"

/** Account-number sign-in, on the same rhythm as [MainScreen] so the app looks like
 *  itself before anyone signs in. The map is drawn apart, which is the state of things. */

/** The [DeviceLimitScreen] affordance travels with any login error: the hub's
 *  device-limit rejection carries no distinct marker to pattern-match. */
@Composable
fun LoginScreen(
    token: String,
    onTokenChange: (String) -> Unit,
    uiState: LoginUiState,
    onSignIn: () -> Unit,
    onManageDevices: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val tokenFocus = remember { FocusRequester() }
    // Keyed on the field being enabled: the session restore that runs on launch disables it,
    // and a disabled field cannot take focus.
    LaunchedEffect(uiState.loading) { if (!uiState.loading) tokenFocus.requestFocus() }

    Surface(modifier = modifier.fillMaxSize(), color = MaterialTheme.colorScheme.background) {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .safeDrawingPadding(),
        ) {
            PangeaHeader(
                version = "v${BuildConfig.VERSION_NAME}",
                onToggleTheme = LocalThemeToggle.current,
            )

            BoxWithConstraints(modifier = Modifier.fillMaxWidth().weight(1f)) {
                val viewport = maxHeight
                val available = maxWidth - 40.dp
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState())
                        // Fills the viewport so the short form sits centred, and scrolls
                        // instead once the keyboard leaves it less room than it needs.
                        .heightIn(min = viewport)
                        .padding(horizontal = 20.dp),
                    verticalArrangement = Arrangement.Center,
                ) {
                    // Sized off the viewport so the keyboard squeezes the map rather than
                    // pushing everything below it off the screen.
                    Box(modifier = Modifier.fillMaxWidth(), contentAlignment = Alignment.Center) {
                        DriftMap(
                            state = ConnState.DISCONNECTED,
                            modifier = Modifier.size(minOf(available, viewport * 0.38f, 236.dp)),
                        )
                    }

                    SectionLabel(text = stringResource(R.string.login_title))
                    HeroHeadline(state = ConnState.DISCONNECTED, modifier = Modifier.padding(top = 6.dp))
                    HeroDetail(
                        text = stringResource(R.string.login_subtitle),
                        modifier = Modifier.padding(top = 6.dp),
                    )

                    SectionLabel(
                        text = stringResource(R.string.login_account_label),
                        modifier = Modifier.padding(top = 20.dp, bottom = 9.dp),
                    )
                    TokenField(
                        value = token,
                        onValueChange = { onTokenChange(formatAccountNumberInput(it)) },
                        placeholder = stringResource(R.string.login_token_placeholder),
                        enabled = !uiState.loading,
                        isError = uiState.error != null,
                        onSubmit = onSignIn,
                        modifier = Modifier.focusRequester(tokenFocus),
                    )

                    uiState.error?.let { error ->
                        LoginError(
                            message = error,
                            onManageDevices = onManageDevices,
                            modifier = Modifier.padding(top = 14.dp),
                        )
                    }

                    PillButton(
                        label = stringResource(R.string.login_signin),
                        onClick = onSignIn,
                        enabled = token.isNotBlank() && !uiState.loading,
                        loading = uiState.loading,
                        modifier = Modifier.padding(top = 14.dp, bottom = 12.dp),
                    )
                }
            }

            val uriHandler = LocalUriHandler.current
            Text(
                text = stringResource(R.string.login_hint),
                modifier = Modifier
                    .fillMaxWidth()
                    .clickable(role = Role.Button) { uriHandler.openUri(ACCOUNT_URL) }
                    .padding(horizontal = 20.dp, vertical = 14.dp),
                color = MaterialTheme.colorScheme.onSurfaceVariant,
                fontSize = 12.sp,
                textAlign = TextAlign.Center,
            )
        }
    }
}

/** Whatever the hub said, plus the one thing the user can do about the common cause. */
@Composable
private fun LoginError(
    message: String,
    onManageDevices: () -> Unit,
    modifier: Modifier = Modifier,
) {
    val shape = RoundedCornerShape(12.dp)
    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(StateError.copy(alpha = 0.1f))
            .border(width = 1.dp, color = StateError.copy(alpha = 0.35f), shape = shape)
            .padding(horizontal = 14.dp, vertical = 12.dp),
    ) {
        Text(
            text = message,
            color = StateError,
            fontSize = 13.sp,
            lineHeight = 18.sp,
        )
        Text(
            text = stringResource(R.string.devicelimit_title),
            modifier = Modifier
                .padding(top = 8.dp)
                .clickable(role = Role.Button, onClick = onManageDevices),
            color = LocalTextSecondary.current,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
            textDecoration = TextDecoration.Underline,
        )
    }
}
