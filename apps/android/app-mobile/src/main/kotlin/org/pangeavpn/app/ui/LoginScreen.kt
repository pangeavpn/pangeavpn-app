package org.pangeavpn.app.ui

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.animateContentSize
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
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
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.rounded.KeyboardArrowDown
import androidx.compose.material.icons.rounded.KeyboardArrowUp
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.LiveRegionMode
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.liveRegion
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.launch
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.PanicWiper
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.FadeSwap
import org.pangeavpn.ui.components.HeroDetail
import org.pangeavpn.ui.components.HeroHeadline
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.PillButton
import org.pangeavpn.ui.components.SectionLabel
import org.pangeavpn.ui.components.TokenField
import org.pangeavpn.ui.text.loginErrorText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.StateError

private val LoginMaxWidth = 520.dp
private val LoginWideWidth = 700.dp
private val LoginGutter = 20.dp
private val LoginMaxMap = 236.dp

// Memory only, never saved state: what was typed survives the device-limit detour, until sign-in or a wipe.
private object LoginDraft {
    var token by mutableStateOf("")

    init {
        val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main)
        scope.launch { SessionRepository.session.collect { if (it != null) token = "" } }
        scope.launch { PanicWiper.wiping.collect { if (it) token = "" } }
    }
}

/** Account-number sign-in on the main screen's rhythm; the menu opens settings without the account. */
@Composable
fun LoginScreen(onOpenSettings: () -> Unit) {
    val loggingIn by SessionRepository.loggingIn.collectAsState()
    val loginError by SessionRepository.loginError.collectAsState()
    val scope = rememberCoroutineScope()
    var submitting by remember { mutableStateOf(false) }
    var showHelp by rememberSaveable { mutableStateOf(false) }
    fun canSubmit(): Boolean = LoginDraft.token.isNotBlank() && !loggingIn && !submitting

    // The repository also drops a second tap, which would register the device twice.
    fun submit() {
        if (!canSubmit()) return
        val raw = LoginDraft.token
        submitting = true
        scope.launch {
            try {
                SessionRepository.login(raw)
            } finally {
                submitting = false
            }
        }
    }

    Column(
        modifier = Modifier
            .fillMaxSize()
            .background(MaterialTheme.colorScheme.background)
            .windowInsetsPadding(WindowInsets.safeDrawing),
    ) {
        PangeaHeader(version = "v${BuildConfig.VERSION_NAME}", onMenu = onOpenSettings)

        BoxWithConstraints(modifier = Modifier.fillMaxWidth().weight(1f)) {
            val wide = maxWidth >= LoginWideWidth
            val viewport = maxHeight
            // Sized off what the keyboard leaves, so the map shrinks before the form is pushed off screen.
            val mapSize = minOf(minOf(maxWidth, LoginMaxWidth) - LoginGutter * 2, viewport * 0.38f, LoginMaxMap)
                .coerceAtLeast(0.dp)

            Column(
                modifier = Modifier
                    .fillMaxWidth()
                    .verticalScroll(rememberScrollState())
                    .heightIn(min = viewport)
                    .wrapContentWidth()
                    .widthIn(max = LoginMaxWidth)
                    .fillMaxWidth()
                    .padding(horizontal = LoginGutter),
                verticalArrangement = if (wide) Arrangement.Center else Arrangement.Top,
            ) {
                Box(
                    modifier = Modifier.fillMaxWidth().clearAndSetSemantics {},
                    contentAlignment = Alignment.Center,
                ) {
                    DriftMap(state = ConnState.DISCONNECTED, modifier = Modifier.size(mapSize))
                }

                SectionLabel(
                    text = stringResource(R.string.login_title),
                    modifier = Modifier.semantics { heading() },
                )
                HeroHeadline(state = ConnState.DISCONNECTED, modifier = Modifier.padding(top = 6.dp))
                HeroDetail(text = stringResource(R.string.login_subtitle), modifier = Modifier.padding(top = 6.dp))

                AccountHelpToggle(expanded = showHelp, onToggle = { showHelp = !showHelp })
                AnimatedVisibility(
                    visible = showHelp,
                    enter = fadeIn(Motion.quick()) + expandVertically(Motion.quick()),
                    exit = fadeOut(Motion.quick()) + shrinkVertically(Motion.quick()),
                ) {
                    Text(
                        text = stringResource(R.string.login_find_account_help),
                        color = LocalTextSecondary.current,
                        fontSize = 13.sp,
                        lineHeight = 18.sp,
                    )
                }

                SectionLabel(
                    text = stringResource(R.string.login_account_label),
                    modifier = Modifier.padding(top = 14.dp, bottom = 9.dp),
                )
                TokenField(
                    value = LoginDraft.token,
                    onValueChange = { LoginDraft.token = it },
                    placeholder = stringResource(R.string.login_token_placeholder),
                    enabled = !loggingIn,
                    isError = loginError != null,
                    onSubmit = { submit() },
                )

                // A container that stays put, so TalkBack hears each new error as it lands.
                FadeSwap(
                    value = loginError,
                    modifier = Modifier
                        .fillMaxWidth()
                        .animateContentSize(Motion.quick())
                        .semantics(mergeDescendants = true) { liveRegion = LiveRegionMode.Polite },
                ) { kind ->
                    if (kind != null) ErrorBox(message = loginErrorText(kind), modifier = Modifier.padding(top = 14.dp))
                }

                PillButton(
                    label = stringResource(R.string.login_signin),
                    onClick = { submit() },
                    enabled = canSubmit(),
                    loading = loggingIn || submitting,
                    modifier = Modifier.padding(top = 14.dp, bottom = 18.dp),
                )
            }
        }
    }
}

// Explained in place rather than linked: the website's account pages also sell plans.
@Composable
private fun AccountHelpToggle(expanded: Boolean, onToggle: () -> Unit) {
    val accent = MaterialTheme.colorScheme.primary
    Row(
        modifier = Modifier
            .heightIn(min = 48.dp)
            .clickable(role = Role.Button, onClick = onToggle),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        Text(
            text = stringResource(R.string.login_find_account),
            color = accent,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Icon(
            imageVector = if (expanded) Icons.Rounded.KeyboardArrowUp else Icons.Rounded.KeyboardArrowDown,
            contentDescription = null,
            modifier = Modifier.padding(start = 2.dp).size(18.dp),
            tint = accent,
        )
    }
}

@Composable
private fun ErrorBox(message: String, modifier: Modifier = Modifier) {
    val shape = RoundedCornerShape(12.dp)
    Text(
        text = message,
        modifier = modifier
            .fillMaxWidth()
            .clip(shape)
            .background(StateError.copy(alpha = 0.1f))
            .border(width = 1.dp, color = StateError.copy(alpha = 0.35f), shape = shape)
            .padding(horizontal = 14.dp, vertical = 12.dp),
        color = StateError,
        fontSize = 13.sp,
        lineHeight = 18.sp,
    )
}
