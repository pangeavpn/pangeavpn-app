package org.pangeavpn.core.session

/** Why a sign-in failed. [Other] carries scrubbed core text, empty when nothing safe was left. */
sealed interface LoginErrorKind {
    data object EmptyAccount : LoginErrorKind
    data object SignInNotSaved : LoginErrorKind
    data object InvalidAccount : LoginErrorKind
    data object SubscriptionExpired : LoginErrorKind
    data object DeviceLimit : LoginErrorKind
    data object RateLimited : LoginErrorKind
    data object ServerError : LoginErrorKind
    data object HubUnreachable : LoginErrorKind
    data object Timeout : LoginErrorKind
    data object RegistrationFailed : LoginErrorKind
    data class Other(val text: String) : LoginErrorKind
}

enum class DeviceLimitRetry { SIGNED_IN, STILL_AT_LIMIT, FAILED }

private const val GO_PREFIX = "mobile: "

private val SIGNED_OUT_MARKERS = listOf("no stored session", "no device identity")

/** The core's own wording for a failed call, without Go's package prefix. */
fun coreErrorText(error: Throwable): String = error.message.orEmpty().removePrefix(GO_PREFIX)

/** The core holds no session, as opposed to one it could not check just now. */
fun isSignedOutRestore(coreText: String): Boolean = SIGNED_OUT_MARKERS.any { it in coreText }

// The repository reports a repeat device limit only by staying on that screen with no error.
fun deviceLimitRetry(signedIn: Boolean, showingLimit: Boolean, hasLoginError: Boolean): DeviceLimitRetry = when {
    signedIn -> DeviceLimitRetry.SIGNED_IN
    showingLimit && !hasLoginError -> DeviceLimitRetry.STILL_AT_LIMIT
    else -> DeviceLimitRetry.FAILED
}

// Core text carries hub bodies, addresses and dial details, so only a mapped or scrubbed line is shown.
object LoginErrors {
    const val MAX_LENGTH = 160

    private const val ELLIPSIS = "…"

    private val expiredMarkers = listOf("SUBSCRIPTION_EXPIRED", "NO_SUBSCRIPTION", "NO_ACTIVE_SUBSCRIPTION")
    private val invalidMarkers = listOf("invalid account", "invalid token", "account not found")
    private val timeoutMarkers = listOf("timeout", "timed out", "deadline exceeded")
    private val unreachableMarkers = listOf(
        "hub unreachable", "hub request failed", "connection method", "network is unreachable",
        "network unreachable", "connection refused", "connection reset", "no such host",
        "no route to host", "not connected to the internet", "network connection was lost",
        "tls handshake", "certificate",
    )
    private val lineBreaks = Regex("""[\n\r\u000B\u000C\u0085  ]+""")
    private val redactions = listOf(
        """[a-z][a-z0-9+.-]*://[^\s"']+""",
        """\[[0-9a-f:.%]+\](:[0-9]+)?""",
        """\b[0-9]{1,3}(\.[0-9]{1,3}){3}(:[0-9]+)?\b""",
        """\b([a-z0-9-]+\.)+[a-z]{2,}(:[0-9]+)?\b""",
        """\b([0-9a-f]{0,4}:){2,7}[0-9a-f]{1,4}\b""",
    ).map { Regex(it, RegexOption.IGNORE_CASE) }

    fun isDeviceLimit(message: String): Boolean =
        message.contains("device limit", ignoreCase = true) || message.contains("device_limit", ignoreCase = true)

    // Hub answers come first: their bodies can quote words the transport markers look for.
    fun kind(raw: String): LoginErrorKind {
        val text = raw.trim()
        if (isDeviceLimit(text)) return LoginErrorKind.DeviceLimit
        if (expiredMarkers.any { it in text }) return LoginErrorKind.SubscriptionExpired
        status("token login failed", text)?.let { return rejected(it, LoginErrorKind.InvalidAccount) }
        (status("hub auth error", text) ?: status("hub error", text))?.let {
            return rejected(it, LoginErrorKind.RegistrationFailed)
        }
        if (mentions(text, invalidMarkers)) return LoginErrorKind.InvalidAccount
        if (mentions(text, timeoutMarkers)) return LoginErrorKind.Timeout
        if (mentions(text, unreachableMarkers)) return LoginErrorKind.HubUnreachable
        return LoginErrorKind.Other(sanitize(text))
    }

    /** The same classes for loads outside sign-in, so no sign-in wording leaks into them. */
    fun general(kind: LoginErrorKind): LoginErrorKind = when (kind) {
        LoginErrorKind.HubUnreachable, LoginErrorKind.Timeout -> LoginErrorKind.HubUnreachable
        LoginErrorKind.InvalidAccount, LoginErrorKind.SubscriptionExpired, LoginErrorKind.DeviceLimit -> kind
        is LoginErrorKind.Other -> if (kind.text.isEmpty()) LoginErrorKind.ServerError else kind
        else -> LoginErrorKind.ServerError
    }

    fun general(raw: String): LoginErrorKind = general(kind(raw))

    fun sanitize(raw: String): String {
        val firstLine = raw.split(lineBreaks).firstOrNull { it.isNotEmpty() }.orEmpty()
        val redacted = redactions.fold(firstLine.substringBefore('{')) { text, pattern ->
            pattern.replace(text, ELLIPSIS)
        }
        val trimmed = redacted.trim { it.isWhitespace() || it in ":;," }
        return if (trimmed.length > MAX_LENGTH) trimmed.take(MAX_LENGTH) + ELLIPSIS else trimmed
    }

    private fun rejected(status: Int, otherwise: LoginErrorKind): LoginErrorKind = when {
        status == 429 -> LoginErrorKind.RateLimited
        status >= 500 -> LoginErrorKind.ServerError
        else -> otherwise
    }

    private fun status(prefix: String, text: String): Int? {
        val marker = "$prefix ("
        val at = text.indexOf(marker)
        return if (at < 0) null else text.substring(at + marker.length).take(3).toIntOrNull()
    }

    private fun mentions(text: String, markers: List<String>): Boolean =
        markers.any { text.contains(it, ignoreCase = true) }
}
