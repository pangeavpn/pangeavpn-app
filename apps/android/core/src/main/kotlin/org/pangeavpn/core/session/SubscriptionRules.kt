package org.pangeavpn.core.session

import java.util.Date
import java.util.TimeZone
import org.pangeavpn.core.model.Subscription

enum class SubscriptionLineKind { NONE, EXPIRED, RENEWS, EXPIRES, PAST_DUE }

/** The account card's subscription line, before any wording. */
data class SubscriptionLine(
    val kind: SubscriptionLineKind,
    val trial: Boolean = false,
    val date: Date? = null,
    // Served from the last answer because the hub was unreachable.
    val offline: Boolean = false,
) {
    /** Lapsed or unpaid; drawn in the warning colour. */
    val warn: Boolean get() = kind == SubscriptionLineKind.EXPIRED || kind == SubscriptionLineKind.PAST_DUE
}

// Entitlement first: a lapsed prepaid plan still reads "active" and would quote a past date.
fun subscriptionLine(sub: Subscription?, timeZone: TimeZone = TimeZone.getDefault()): SubscriptionLine {
    if (sub == null) return SubscriptionLine(SubscriptionLineKind.NONE)
    val live = sub.status == "active" || sub.status == "trialing"
    val kind = when {
        sub.entitled == false -> SubscriptionLineKind.EXPIRED
        live && sub.renews -> SubscriptionLineKind.RENEWS
        live -> SubscriptionLineKind.EXPIRES
        sub.status == "past_due" -> SubscriptionLineKind.PAST_DUE
        else -> SubscriptionLineKind.NONE
    }
    return SubscriptionLine(
        kind = kind,
        trial = sub.entitled != false && sub.status == "trialing",
        date = sub.expiresAt?.let { HubDate.parse(it, timeZone) },
        offline = sub.cached,
    )
}

/** Unknown says nothing, and a lapsed plan gets the expired panel instead of a line. */
fun showsSubscriptionBanner(sub: Subscription?): Boolean =
    sub != null && sub.entitled != false && sub.status != "active" && sub.status != "trialing"

fun subscriptionBannerWarns(sub: Subscription?): Boolean = sub?.status == "past_due"

enum class ExpiredVerdict { RESTORED, STILL_EXPIRED, CHECK_FAILED }

/** How long "Check again" holds its spinner, so a fast answer settles instead of flashing past. */
const val EXPIRED_CHECK_HOLD_MS = 700L

// No answer, or a cached one, means the hub was not reached, so the old verdict proves nothing.
fun expiredVerdict(reached: Boolean, entitled: Boolean, cached: Boolean): ExpiredVerdict = when {
    entitled -> ExpiredVerdict.RESTORED
    reached && !cached -> ExpiredVerdict.STILL_EXPIRED
    else -> ExpiredVerdict.CHECK_FAILED
}

const val SUBSCRIPTION_INTERVAL_MS = 600_000L

// While expired the user is likely topping up elsewhere, so coming back checks sooner.
const val EXPIRED_INTERVAL_MS = 30_000L

fun subscriptionDue(lastCheckMs: Long?, nowMs: Long, entitled: Boolean): Boolean {
    if (lastCheckMs == null) return true
    val interval = if (entitled) SUBSCRIPTION_INTERVAL_MS else EXPIRED_INTERVAL_MS
    return nowMs - lastCheckMs >= interval
}
