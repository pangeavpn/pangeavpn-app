package org.pangeavpn.app.ui.settings.pages

import android.Manifest
import android.app.Activity
import android.app.NotificationManager
import android.content.Context
import android.content.ContextWrapper
import android.content.Intent
import android.content.pm.PackageManager
import android.net.Uri
import android.os.Build
import android.provider.Settings
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material.icons.outlined.NotificationsOff
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.stringResource
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.compose.LifecycleEventEffect
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.ui.R
import org.pangeavpn.ui.components.GhostButton
import org.pangeavpn.ui.components.InfoNote
import org.pangeavpn.ui.components.NoteTone
import org.pangeavpn.ui.components.SettingsGroup
import org.pangeavpn.ui.components.SettingsPage
import org.pangeavpn.ui.components.SwitchRow
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle

private enum class NotificationAccess { GRANTED, NOT_ASKED, DENIED }

// Android reports "never asked" and "refused for good" alike, so a refusal the system no longer
// offers to undo is remembered for the rest of the run.
private var refusedForGood = false

@Composable
fun NotificationsPage(onBack: () -> Unit) {
    val context = LocalContext.current
    val prefs by AppPrefs.state.collectAsState()
    var access by remember { mutableStateOf(notificationAccess(context)) }
    val onText = stringResource(R.string.notifications_toast_on)
    val offText = stringResource(R.string.notifications_toast_off)
    val deniedText = stringResource(R.string.notifications_android_denied)
    val openFailedText = stringResource(R.string.notifications_android_open_failed)

    val permission = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { granted ->
        val rationale = context.findActivity()?.shouldShowRequestPermissionRationale(Manifest.permission.POST_NOTIFICATIONS)
        if (!granted && rationale == false) refusedForGood = true
        access = notificationAccess(context)
        if (granted) ToastCenter.show(onText, ToastStyle.SUCCESS) else ToastCenter.show(deniedText, ToastStyle.WARNING)
    }

    // A refusal leaves the preference on; the page then shows how to allow alerts in Settings.
    fun ask() {
        access = notificationAccess(context)
        when (access) {
            NotificationAccess.GRANTED -> ToastCenter.show(onText, ToastStyle.SUCCESS)
            NotificationAccess.NOT_ASKED -> permission.launch(Manifest.permission.POST_NOTIFICATIONS)
            NotificationAccess.DENIED -> ToastCenter.show(deniedText, ToastStyle.WARNING)
        }
    }

    fun setAlerts(enabled: Boolean) {
        if (enabled == AppPrefs.value.connectionAlerts) return
        AppPrefs.update { it.copy(connectionAlerts = enabled) }
        if (enabled) ask() else ToastCenter.show(offText, ToastStyle.INFO)
    }

    // The user may have changed the permission in Settings meanwhile.
    LifecycleEventEffect(Lifecycle.Event.ON_RESUME) { access = notificationAccess(context) }

    SettingsPage(
        title = stringResource(R.string.settings_section_notifications),
        onBack = onBack,
        description = stringResource(R.string.notifications_description),
    ) {
        SettingsGroup {
            SwitchRow(
                title = stringResource(R.string.notifications_alerts_title),
                checked = prefs.connectionAlerts,
                onCheckedChange = { setAlerts(it) },
                hint = stringResource(R.string.notifications_alerts_hint),
            )
            if (prefs.connectionAlerts) {
                when (access) {
                    NotificationAccess.DENIED -> {
                        InfoNote(text = deniedText, tone = NoteTone.ERROR, icon = Icons.Outlined.NotificationsOff)
                        GhostButton(
                            label = stringResource(R.string.notifications_android_open_settings),
                            onClick = {
                                if (!openNotificationSettings(context)) ToastCenter.show(openFailedText, ToastStyle.WARNING)
                            },
                        )
                    }
                    NotificationAccess.NOT_ASKED -> {
                        InfoNote(
                            text = stringResource(R.string.notifications_not_asked),
                            icon = Icons.Outlined.NotificationsActive,
                        )
                        GhostButton(label = stringResource(R.string.notifications_allow), onClick = { ask() })
                    }
                    NotificationAccess.GRANTED -> Unit
                }
            }
        }
    }
}

// Below Android 13 there is no permission to ask for, only notifications blocked in Settings.
private fun notificationAccess(context: Context): NotificationAccess {
    if (context.getSystemService(NotificationManager::class.java).areNotificationsEnabled()) {
        return NotificationAccess.GRANTED
    }
    if (Build.VERSION.SDK_INT < Build.VERSION_CODES.TIRAMISU) return NotificationAccess.DENIED
    val permission = Manifest.permission.POST_NOTIFICATIONS
    val askable = context.findActivity()?.shouldShowRequestPermissionRationale(permission) == true
    val permitted = context.checkSelfPermission(permission) == PackageManager.PERMISSION_GRANTED
    return when {
        askable -> NotificationAccess.NOT_ASKED
        permitted || refusedForGood -> NotificationAccess.DENIED
        else -> NotificationAccess.NOT_ASKED
    }
}

private fun openNotificationSettings(context: Context): Boolean {
    val intents = listOfNotNull(
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Intent(Settings.ACTION_APP_NOTIFICATION_SETTINGS).putExtra(Settings.EXTRA_APP_PACKAGE, context.packageName)
        } else {
            null
        },
        Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS, Uri.fromParts("package", context.packageName, null)),
    )
    return intents.any { intent -> runCatching { context.startActivity(intent) }.isSuccess }
}

private tailrec fun Context.findActivity(): Activity? = when (this) {
    is Activity -> this
    is ContextWrapper -> baseContext.findActivity()
    else -> null
}
