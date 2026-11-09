package org.pangeavpn.app.ui.settings

import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.outlined.CallSplit
import androidx.compose.material.icons.outlined.Bolt
import androidx.compose.material.icons.outlined.Code
import androidx.compose.material.icons.outlined.CreditCard
import androidx.compose.material.icons.outlined.Devices
import androidx.compose.material.icons.outlined.Dns
import androidx.compose.material.icons.outlined.Notifications
import androidx.compose.material.icons.outlined.Palette
import androidx.compose.material.icons.outlined.Public
import androidx.compose.material.icons.outlined.Security
import androidx.compose.material.icons.outlined.Shield
import androidx.compose.material.icons.outlined.SupportAgent
import androidx.compose.material.icons.outlined.SystemUpdate
import androidx.compose.material.icons.outlined.Translate
import androidx.compose.material.icons.outlined.Warning
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.res.stringResource
import org.pangeavpn.ui.R

/** One entry per desktop settings rail item, plus the kill switch, devices and subscription pages. */
enum class SettingsRoute {
    TRANSPORT, PROVISIONING, NETWORK, KILL_SWITCH, SPLIT_TUNNEL, STARTUP, NOTIFICATIONS,
    PANIC, LANGUAGE, APPEARANCE, SUPPORT, UPDATE, DEVELOPER, DEVICES, SUBSCRIPTION;

    companion object {
        /** The home list in desktop's rail order; developer shows only once unlocked. */
        val home = listOf(
            TRANSPORT, PROVISIONING, NETWORK, KILL_SWITCH, SPLIT_TUNNEL, STARTUP,
            NOTIFICATIONS, PANIC, LANGUAGE, APPEARANCE, SUPPORT, UPDATE, DEVELOPER,
        )

        val account = listOf(SUBSCRIPTION, DEVICES)

        /** A wide window always shows a page: the path's first stop, else the first section. */
        fun selected(path: List<SettingsRoute>): SettingsRoute = path.firstOrNull() ?: home[0]

        fun withoutDeveloper(path: List<SettingsRoute>): List<SettingsRoute> = path.filter { it != DEVELOPER }
    }
}

@Composable
fun SettingsRoute.title(): String = stringResource(
    when (this) {
        SettingsRoute.TRANSPORT -> R.string.settings_transport_heading
        SettingsRoute.PROVISIONING -> R.string.settings_censorship_heading
        SettingsRoute.NETWORK -> R.string.settings_network_heading
        SettingsRoute.KILL_SWITCH -> R.string.settings_section_kill_switch
        SettingsRoute.SPLIT_TUNNEL -> R.string.settings_section_split_tunnel
        SettingsRoute.STARTUP -> R.string.settings_section_startup
        SettingsRoute.NOTIFICATIONS -> R.string.settings_section_notifications
        SettingsRoute.PANIC -> R.string.settings_section_panic
        SettingsRoute.LANGUAGE -> R.string.settings_section_language
        SettingsRoute.APPEARANCE -> R.string.settings_section_appearance
        SettingsRoute.SUPPORT -> R.string.settings_section_support
        SettingsRoute.UPDATE -> R.string.settings_section_update
        SettingsRoute.DEVELOPER -> R.string.settings_section_developer
        SettingsRoute.DEVICES -> R.string.devices_title
        SettingsRoute.SUBSCRIPTION -> R.string.settings_account_subscription
    },
)

val SettingsRoute.icon: ImageVector
    get() = when (this) {
        SettingsRoute.TRANSPORT -> Icons.Outlined.Shield
        SettingsRoute.PROVISIONING -> Icons.Outlined.Dns
        SettingsRoute.NETWORK -> Icons.Outlined.Public
        SettingsRoute.KILL_SWITCH -> Icons.Outlined.Security
        SettingsRoute.SPLIT_TUNNEL -> Icons.AutoMirrored.Outlined.CallSplit
        SettingsRoute.STARTUP -> Icons.Outlined.Bolt
        SettingsRoute.NOTIFICATIONS -> Icons.Outlined.Notifications
        SettingsRoute.PANIC -> Icons.Outlined.Warning
        SettingsRoute.LANGUAGE -> Icons.Outlined.Translate
        SettingsRoute.APPEARANCE -> Icons.Outlined.Palette
        SettingsRoute.SUPPORT -> Icons.Outlined.SupportAgent
        SettingsRoute.UPDATE -> Icons.Outlined.SystemUpdate
        SettingsRoute.DEVELOPER -> Icons.Outlined.Code
        SettingsRoute.DEVICES -> Icons.Outlined.Devices
        SettingsRoute.SUBSCRIPTION -> Icons.Outlined.CreditCard
    }
