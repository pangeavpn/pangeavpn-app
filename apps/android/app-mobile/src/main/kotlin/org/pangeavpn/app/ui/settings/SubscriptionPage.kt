package org.pangeavpn.app.ui.settings

import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.size
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.CalendarMonth
import androidx.compose.material.icons.outlined.Tune
import androidx.compose.material3.Icon
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.Text
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.PangeaCard
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsNavRow
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.text.subscriptionLineText
import org.pangeavpn.ui.theme.LocalTextSecondary
import org.pangeavpn.ui.theme.StateConnecting

/** The account's plan; the website's billing page only in builds that may link to it (never Play). */
@Composable
internal fun SubscriptionPage(onBack: () -> Unit) {
    val context = LocalContext.current
    val subscription by SessionRepository.subscription.collectAsState()
    val line = subscriptionLineText(subscription)

    SettingsPage(
        title = stringResource(R.string.settings_account_subscription),
        onBack = onBack,
        description = stringResource(R.string.subscription_description_android),
    ) {
        SettingsGroup(title = stringResource(R.string.subscription_plan_card)) {
            PangeaCard(padding = PaddingValues(horizontal = 14.dp, vertical = 12.dp)) {
                Row(
                    modifier = Modifier.heightIn(min = 32.dp),
                    verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(14.dp),
                ) {
                    val tint = if (line.warn) StateConnecting else null
                    Icon(
                        imageVector = Icons.Outlined.CalendarMonth,
                        contentDescription = null,
                        modifier = Modifier.size(22.dp),
                        tint = tint ?: LocalTextSecondary.current,
                    )
                    Text(
                        text = line.text,
                        color = tint ?: MaterialTheme.colorScheme.onSurface,
                        fontSize = 16.sp,
                        fontWeight = FontWeight.SemiBold,
                    )
                }
            }
        }
        if (BuildConfig.WEB_BILLING) {
            SettingsGroup(title = stringResource(R.string.subscription_billing_card)) {
                SettingsNavRow(
                    icon = Icons.Outlined.Tune,
                    title = stringResource(R.string.subscription_manage),
                    onClick = { openLink(context, BILLING_URL) },
                )
                InfoNote(text = stringResource(R.string.subscription_manage_web_hint))
            }
        }
    }
}
