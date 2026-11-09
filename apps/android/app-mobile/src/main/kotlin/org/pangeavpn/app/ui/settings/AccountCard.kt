package org.pangeavpn.app.ui.settings

import android.content.ClipData
import android.content.ClipDescription
import android.content.ClipboardManager
import android.content.Context
import android.os.Build
import android.os.PersistableBundle
import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ContentCopy
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.LocalTextStyle
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.material3.minimumInteractiveComponentSize
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableIntStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberCoroutineScope
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.semantics.Role
import androidx.compose.ui.semantics.contentDescription
import androidx.compose.ui.semantics.heading
import androidx.compose.ui.semantics.semantics
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextDirection
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.IconAction
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.text.subscriptionLineText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnecting

private const val MASK = "••••-••••-••••-••••-••••-••••"
private val CardShape = RoundedCornerShape(16.dp)
private val PillShape = RoundedCornerShape(percent = 50)

/** Desktop's account bar: the account number behind the device-owner check, and the subscription line.
 *  refreshing is the once-per-visit subscription read, which shows "Loading..." while nothing is known. */
@Composable
internal fun AccountCard(
    refreshing: Boolean,
    modifier: Modifier = Modifier,
    fill: Color = MaterialTheme.colorScheme.surface,
) {
    val context = LocalContext.current
    val scope = rememberCoroutineScope()
    val ownerCheck = rememberOwnerCheck()
    val subscription by SessionRepository.subscription.collectAsState()
    val label = stringResource(R.string.settings_account_number)
    val copiedText = stringResource(R.string.settings_account_copied)
    var revealed by remember { mutableStateOf<String?>(null) }
    var revealing by remember { mutableStateOf(false) }
    // Bumped on every hide, so an unlock that finishes after it cannot show the number again.
    var generation by remember { mutableIntStateOf(0) }

    fun hide() {
        generation++
        revealed = null
    }

    fun reveal() {
        if (revealing) return
        revealing = true
        val started = generation
        scope.launch {
            val number = if (ownerCheck()) SessionRepository.accountNumber() else null
            revealing = false
            if (started == generation && number != null) revealed = formatAccountNumberInput(number)
        }
    }

    // The credential screen below API 29 pauses the activity itself, so only an idle card hides then.
    LifecycleEventEffect(Lifecycle.Event.ON_PAUSE) { if (!revealing) hide() }

    Column(
        modifier = modifier
            .fillMaxWidth()
            .clip(CardShape)
            .background(fill)
            .border(width = 1.dp, color = MaterialTheme.colorScheme.outlineVariant, shape = CardShape)
            .padding(16.dp),
        verticalArrangement = Arrangement.spacedBy(14.dp),
    ) {
        Column(verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    text = label,
                    modifier = Modifier.weight(1f).semantics { heading() },
                    color = MaterialTheme.colorScheme.onSurface,
                    fontSize = 16.sp,
                    fontWeight = FontWeight.SemiBold,
                )
                revealed?.let { number ->
                    IconAction(
                        icon = Icons.Outlined.ContentCopy,
                        label = stringResource(R.string.settings_account_copy),
                        onClick = {
                            AccountClipboard.copy(context, label, number)
                            ToastCenter.show(copiedText, ToastStyle.SUCCESS)
                        },
                        tint = LocalTextSecondary.current,
                    )
                }
                ShowButton(
                    shown = revealed != null,
                    busy = revealing,
                    onClick = { if (revealed == null) reveal() else hide() },
                )
            }
            NumberText(revealed)
        }
        Box(
            modifier = Modifier
                .fillMaxWidth()
                .height(1.dp)
                .background(MaterialTheme.colorScheme.outlineVariant),
        )
        SubscriptionSection(subscription = subscription, loading = refreshing && subscription == null)
    }
}

@Composable
private fun NumberText(revealed: String?) {
    val hidden = stringResource(R.string.settings_account_hidden)
    Text(
        text = revealed ?: MASK,
        // One character at a time, so TalkBack spells the number out instead of reading it as words.
        modifier = Modifier.semantics { contentDescription = revealed?.toList()?.joinToString(" ") ?: hidden },
        color = if (revealed == null) MaterialTheme.colorScheme.onSurfaceVariant else MaterialTheme.colorScheme.onSurface,
        fontFamily = FontFamily.Monospace,
        fontSize = 15.sp,
        fontWeight = if (revealed == null) FontWeight.Normal else FontWeight.Medium,
        style = LocalTextStyle.current.copy(textDirection = TextDirection.Ltr),
    )
}

@Composable
private fun ShowButton(shown: Boolean, busy: Boolean, onClick: () -> Unit) {
    val secondary = LocalTextSecondary.current
    Box(
        modifier = Modifier
            .minimumInteractiveComponentSize()
            .clip(PillShape)
            .background(MaterialTheme.colorScheme.surfaceVariant)
            .border(width = 1.dp, color = MaterialTheme.colorScheme.outline, shape = PillShape)
            .clickable(enabled = !busy, role = Role.Button, onClick = onClick)
            .padding(horizontal = 14.dp, vertical = 7.dp),
        contentAlignment = Alignment.Center,
    ) {
        // Kept in the layout while busy, so the pill holds its width and TalkBack its name.
        Text(
            text = stringResource(if (shown) R.string.settings_account_hide else R.string.settings_account_show),
            modifier = Modifier.alpha(if (busy) 0f else 1f),
            color = secondary,
            fontSize = 13.sp,
            fontWeight = FontWeight.SemiBold,
            maxLines = 1,
        )
        if (busy) {
            CircularProgressIndicator(modifier = Modifier.size(16.dp), color = secondary, strokeWidth = 2.dp)
        }
    }
}

@Composable
private fun SubscriptionSection(subscription: Subscription?, loading: Boolean) {
    val line = subscriptionLineText(subscription)
    Column(
        modifier = Modifier
            .fillMaxWidth()
            .semantics(mergeDescendants = true) {},
        verticalArrangement = Arrangement.spacedBy(4.dp),
    ) {
        Text(
            text = stringResource(R.string.settings_account_subscription),
            color = MaterialTheme.colorScheme.onSurface,
            fontSize = 15.sp,
            fontWeight = FontWeight.SemiBold,
        )
        Text(
            text = if (loading) stringResource(R.string.settings_account_loading) else line.text,
            color = if (line.warn && !loading) StateConnecting else MaterialTheme.colorScheme.onSurfaceVariant,
            fontFamily = FontFamily.Monospace,
            fontSize = 13.sp,
            lineHeight = 18.sp,
        )
    }
}

/** The account number on the clipboard: marked sensitive, and cleared after two minutes unless
 *  something else was copied since. Call [sweep] whenever the app's window gains focus. */
object AccountClipboard {
    private const val LIFETIME_MS = 120_000L
    private const val MARKER = "org.pangeavpn.accountNumber"

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)
    private var expiry: Job? = null
    private var due = false

    fun copy(context: Context, label: String, number: String) {
        val app = context.applicationContext
        val clip = ClipData.newPlainText(label, number)
        clip.description.extras = PersistableBundle().apply {
            putBoolean(MARKER, true)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.TIRAMISU) putBoolean(ClipDescription.EXTRA_IS_SENSITIVE, true)
        }
        clipboard(app).setPrimaryClip(clip)
        due = false
        expiry?.cancel()
        expiry = scope.launch {
            delay(LIFETIME_MS)
            due = true
            sweep(app)
        }
    }

    // Android 10+ hides the clipboard from a background app, so a clear that falls due then waits for focus.
    fun sweep(context: Context) {
        if (!due) return
        val clipboard = clipboard(context)
        val description = clipboard.primaryClipDescription ?: return
        if (description.extras?.getBoolean(MARKER) == true) {
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) clipboard.clearPrimaryClip()
            else clipboard.setPrimaryClip(ClipData.newPlainText("", ""))
        }
        due = false
    }

    private fun clipboard(context: Context): ClipboardManager =
        context.getSystemService(Context.CLIPBOARD_SERVICE) as ClipboardManager
}
