package org.pangeavpn.app.ui.settings

import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri

internal const val SUPPORT_EMAIL = "support@pangeavpn.org"
internal const val PRIVACY_URL = "https://pangeavpn.org/legal/privacy"
internal const val TERMS_URL = "https://pangeavpn.org/legal/terms"
internal const val BILLING_URL = "https://pangeavpn.org/app/billing"

/** False when nothing on the device can open it. */
internal fun openLink(context: Context, uri: String, action: String = Intent.ACTION_VIEW): Boolean =
    try {
        context.startActivity(Intent(action, Uri.parse(uri)))
        true
    } catch (e: ActivityNotFoundException) {
        false
    }
