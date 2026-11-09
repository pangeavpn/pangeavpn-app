package org.pangeavpn.tv.ui

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.focus.FocusRequester
import androidx.compose.ui.focus.focusRequester
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.tv.BuildConfig
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.NoteCard
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.TokenField
import org.pangeavpn.ui.text.loginErrorText
import org.pangeavpn.ui.theme.LocalTextSecondary

/** iOS LoginView in its wide shape: the map beside the form. The draft lives in the root so the device-limit trip keeps it. */
@Composable
fun TvLoginScreen(token: String, onTokenChange: (String) -> Unit, onOpenSettings: () -> Unit) {
    val loggingIn by SessionRepository.loggingIn.collectAsState()
    val error by SessionRepository.loginError.collectAsState()
    val scope = rememberCoroutineScope()
    val field = remember { FocusRequester() }
    val signIn = remember { FocusRequester() }

    // Focus moves to the button first, so the field can go read-only without dropping it.
    val submit: () -> Unit = {
        if (token.isNotBlank() && !loggingIn) {
            signIn.tryFocus()
            scope.launch { SessionRepository.login(token) }
        }
    }

    LaunchedEffect(Unit) { field.tryFocus() }

    Column(modifier = Modifier.fillMaxSize().overscan()) {
        PangeaHeader(version = "v${BuildConfig.VERSION_NAME}", onMenu = onOpenSettings)
        Row(modifier = Modifier.fillMaxSize()) {
            BoxWithConstraints(
                modifier = Modifier.weight(0.42f).fillMaxHeight(),
                contentAlignment = Alignment.Center,
            ) {
                DriftMap(
                    state = ConnState.DISCONNECTED,
                    modifier = Modifier.size(minOf(maxWidth, maxHeight)).clearAndSetSemantics {},
                )
            }
            Spacer(modifier = Modifier.width(32.dp))
            BoxWithConstraints(modifier = Modifier.weight(0.58f).fillMaxHeight()) {
                Column(
                    modifier = Modifier
                        .fillMaxWidth()
                        .verticalScroll(rememberScrollState())
                        .heightIn(min = maxHeight)
                        .widthIn(max = 460.dp),
                    verticalArrangement = Arrangement.Center,
                ) {
                    SectionLabel(text = stringResource(R.string.login_title))
                    HeroHeadline(state = ConnState.DISCONNECTED, modifier = Modifier.padding(top = 6.dp))
                    HeroDetail(text = stringResource(R.string.login_subtitle), modifier = Modifier.padding(top = 6.dp))
                    Text(
                        text = stringResource(R.string.login_find_account),
                        modifier = Modifier.padding(top = 12.dp),
                        color = MaterialTheme.colorScheme.primary,
                        fontSize = 13.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                    Text(
                        text = stringResource(R.string.login_find_account_help),
                        modifier = Modifier.padding(top = 2.dp),
                        color = LocalTextSecondary.current,
                        fontSize = 13.sp,
                        lineHeight = 18.sp,
                    )
                    SectionLabel(
                        text = stringResource(R.string.login_account_label),
                        modifier = Modifier.padding(top = 16.dp, bottom = 9.dp),
                    )
                    TokenField(
                        value = token,
                        onValueChange = onTokenChange,
                        placeholder = stringResource(R.string.login_token_placeholder),
                        modifier = Modifier.focusRequester(field),
                        enabled = !loggingIn,
                        isError = error != null,
                        onSubmit = submit,
                    )
                    error?.let { kind ->
                        NoteCard(
                            text = loginErrorText(kind),
                            tone = NoteTone.ERROR,
                            modifier = Modifier
                                .padding(top = 12.dp)
                                .semantics { liveRegion = LiveRegionMode.Polite },
                        )
                    }
                    // Enabled through the sign-in itself: a disabled button would drop the remote's focus.
                    PillButton(
                        label = stringResource(R.string.login_signin),
                        onClick = submit,
                        enabled = token.isNotBlank(),
                        loading = loggingIn,
                        modifier = Modifier.padding(top = 14.dp).focusRequester(signIn),
                    )
                }
            }
        }
    }
}
