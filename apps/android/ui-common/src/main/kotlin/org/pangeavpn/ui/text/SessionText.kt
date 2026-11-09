package org.pangeavpn.ui.text

import android.content.res.Resources
import android.icu.text.DateFormat
import androidx.compose.runtime.Composable
import androidx.compose.runtime.ReadOnlyComposable
import androidx.compose.ui.platform.LocalConfiguration
import androidx.compose.ui.platform.LocalContext
import java.util.Date
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.session.DeviceError
import org.pangeavpn.core.session.ExpiredVerdict
import org.pangeavpn.core.session.HubDate
import org.pangeavpn.core.session.LoginErrorKind
import org.pangeavpn.core.session.LoginErrors
import org.pangeavpn.core.session.RenameError
import org.pangeavpn.core.session.SubscriptionLineKind
import org.pangeavpn.core.session.subscriptionBannerWarns
import org.pangeavpn.core.session.subscriptionLine
import org.pangeavpn.core.R as CoreR
import org.pangeavpn.ui.R

// Each text has two forms: a composable one, and one taking Resources for a toast raised from a coroutine.

/** A subscription line and whether it is drawn in the warning colour. */
data class LineText(val text: String, val warn: Boolean)

// Reads the configuration so a language change recomposes, as stringResource does.
@Composable
@ReadOnlyComposable
private fun resources(): Resources {
    LocalConfiguration.current
    return LocalContext.current.resources
}

@Composable
fun loginErrorText(kind: LoginErrorKind): String = loginErrorText(resources(), kind)

fun loginErrorText(resources: Resources, kind: LoginErrorKind): String = when (kind) {
    LoginErrorKind.EmptyAccount -> resources.getString(R.string.login_empty_account)
    LoginErrorKind.SignInNotSaved -> resources.getString(CoreR.string.error_signin_not_saved)
    LoginErrorKind.InvalidAccount -> resources.getString(R.string.login_error_invalid_account)
    LoginErrorKind.SubscriptionExpired -> resources.getString(R.string.login_error_expired)
    LoginErrorKind.DeviceLimit -> resources.getString(R.string.login_error_device_limit)
    LoginErrorKind.RateLimited -> resources.getString(R.string.login_error_rate_limited)
    LoginErrorKind.ServerError -> resources.getString(R.string.login_error_server)
    LoginErrorKind.HubUnreachable -> resources.getString(R.string.login_error_unreachable)
    LoginErrorKind.Timeout -> resources.getString(R.string.login_error_timeout)
    LoginErrorKind.RegistrationFailed -> resources.getString(R.string.login_error_registration)
    is LoginErrorKind.Other -> kind.text.ifEmpty { resources.getString(R.string.login_error_unknown) }
}

/** For loads outside sign-in (regions, devices): the same scrubbed classes without sign-in wording. */
@Composable
fun generalErrorText(kind: LoginErrorKind): String = generalErrorText(resources(), kind)

fun generalErrorText(resources: Resources, kind: LoginErrorKind): String =
    loginErrorText(resources, LoginErrors.general(kind))

@Composable
fun renameErrorText(error: RenameError): String = renameErrorText(resources(), error)

fun renameErrorText(resources: Resources, error: RenameError): String = resources.getString(
    when (error) {
        RenameError.NOT_FOUND -> R.string.session_rename_not_found
        RenameError.UNAVAILABLE -> R.string.session_rename_unavailable
        RenameError.LENGTH -> R.string.session_rename_length
        RenameError.ACCOUNT_NUMBER -> R.string.session_rename_account_number
        RenameError.FAILED -> R.string.session_rename_failed
    },
)

@Composable
fun deviceErrorText(error: DeviceError): String = deviceErrorText(resources(), error)

fun deviceErrorText(resources: Resources, error: DeviceError): String = when (error) {
    is DeviceError.Load -> generalErrorText(resources, error.kind)
    is DeviceError.Remove -> generalErrorText(resources, error.kind)
    is DeviceError.Rename -> renameErrorText(resources, error.kind)
}

/** The account card's line: desktop's wording, with "(offline)" on an answer served from the cache. */
@Composable
fun subscriptionLineText(sub: Subscription?): LineText = subscriptionLineText(resources(), sub)

fun subscriptionLineText(resources: Resources, sub: Subscription?): LineText {
    val line = subscriptionLine(sub)
    val date = line.date?.let { mediumDate(resources, it) }
    val base = when (line.kind) {
        SubscriptionLineKind.NONE -> resources.getString(R.string.subscription_none)
        SubscriptionLineKind.EXPIRED -> dashed(resources.getString(R.string.settings_sub_expired), date)
        SubscriptionLineKind.PAST_DUE -> dashed(resources.getString(R.string.settings_sub_past_due), date)
        SubscriptionLineKind.RENEWS -> dated(resources, R.string.settings_sub_renews, line.trial, date)
        SubscriptionLineKind.EXPIRES -> dated(resources, R.string.settings_sub_expires, line.trial, date)
    }
    val text = if (line.offline) "$base ${resources.getString(R.string.settings_sub_offline)}" else base
    return LineText(text, line.warn)
}

/** The main screen's banner, for a plan that is neither live nor lapsed. */
@Composable
fun subscriptionBannerText(sub: Subscription?): String = subscriptionBannerText(resources(), sub)

fun subscriptionBannerText(resources: Resources, sub: Subscription?): String {
    if (subscriptionBannerWarns(sub)) return subscriptionLineText(resources, sub).text
    val date = mediumDate(resources, sub?.expiresAt) ?: return resources.getString(R.string.subscription_none)
    return resources.getString(R.string.subscription_expires, date)
}

@Composable
fun expiredVerdictText(verdict: ExpiredVerdict): String = expiredVerdictText(resources(), verdict)

fun expiredVerdictText(resources: Resources, verdict: ExpiredVerdict): String = resources.getString(
    when (verdict) {
        ExpiredVerdict.RESTORED -> R.string.expired_restored
        ExpiredVerdict.STILL_EXPIRED -> R.string.expired_still
        ExpiredVerdict.CHECK_FAILED -> R.string.expired_check_failed
    },
)

/** "Ran out on <date>", or null when the hub gave no readable date. */
@Composable
fun expiredRanOutText(expiresAt: String?): String? = expiredRanOutText(resources(), expiresAt)

fun expiredRanOutText(resources: Resources, expiresAt: String?): String? =
    mediumDate(resources, expiresAt)?.let { resources.getString(R.string.expired_ran_out, it) }

/** A hub date as "Dec 26, 2026" in the app's language; null when absent or unreadable. */
@Composable
fun mediumDate(raw: String?): String? = mediumDate(resources(), raw)

fun mediumDate(resources: Resources, raw: String?): String? =
    raw?.let { HubDate.parse(it) }?.let { mediumDate(resources, it) }

// ICU's formatter, not java.text's: it follows the locale's own calendar, as the other apps do.
fun mediumDate(resources: Resources, date: Date): String =
    DateFormat.getDateInstance(DateFormat.MEDIUM, resources.configuration.locales[0]).format(date)

// The generated XML drops the prefix's trailing space, so it is put back here.
private fun dated(resources: Resources, verb: Int, trial: Boolean, date: String?): String {
    val prefix = if (trial) resources.getString(R.string.settings_sub_trial_prefix).trimEnd() + " " else ""
    return prefix + listOfNotNull(resources.getString(verb), date).joinToString(" ")
}

private fun dashed(text: String, date: String?): String = if (date == null) text else "$text — $date"
