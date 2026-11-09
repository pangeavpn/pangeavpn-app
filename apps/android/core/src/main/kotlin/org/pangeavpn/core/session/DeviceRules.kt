package org.pangeavpn.core.session

import java.text.BreakIterator
import org.pangeavpn.core.model.Device

enum class RenameError { NOT_FOUND, UNAVAILABLE, LENGTH, ACCOUNT_NUMBER, FAILED }

/** What went wrong on the device list; the kinds inside are already safe to show. */
sealed interface DeviceError {
    data class Load(val kind: LoginErrorKind) : DeviceError
    data class Remove(val kind: LoginErrorKind) : DeviceError
    data class Rename(val kind: RenameError) : DeviceError
}

/** The current device first; the rest keep the hub's order. */
fun currentFirst(devices: List<Device>): List<Device> = devices.filter { it.current } + devices.filterNot { it.current }

/** The name to show, or null for the screen's "Unnamed device". */
val Device.shownName: String? get() = friendlyName?.trim()?.ifEmpty { null }

// String.prototype.trim's set: no U+0085, and the byte order mark counts.
private fun isJsWhitespace(c: Char): Boolean =
    c in '\u0009'..'\u000D' || c == ' ' || c == ' ' || c == ' ' || c in ' '..' ' ||
        c == ' ' || c == ' ' || c == ' ' || c == ' ' || c == '　' || c == '﻿'

object DeviceRename {
    const val MAX_UNITS = 40

    /** The trimmed name, or null when the core would refuse it; length is in UTF-16 units, as the hub counts. */
    fun validated(raw: String): String? = raw.trim { isJsWhitespace(it) }.takeIf { it.length in 1..MAX_UNITS }

    /** For typing: whole characters only, so an emoji is never cut in half at the hub's limit. */
    fun clamped(raw: String): String {
        if (raw.length <= MAX_UNITS) return raw
        val characters = BreakIterator.getCharacterInstance()
        characters.setText(raw)
        var end = 0
        var next = characters.next()
        while (next != BreakIterator.DONE && next <= MAX_UNITS) {
            end = next
            next = characters.next()
        }
        return raw.substring(0, end)
    }

    // Hub codes ride inside the core's error text; anything unrecognised gets the generic line.
    fun error(coreText: String): RenameError = when {
        "DEVICE_NOT_FOUND" in coreText -> RenameError.NOT_FOUND
        "DEVICE_NAMES_UNAVAILABLE" in coreText -> RenameError.UNAVAILABLE
        "1-40 characters" in coreText -> RenameError.LENGTH
        "Sign in with your account number" in coreText -> RenameError.ACCOUNT_NUMBER
        else -> RenameError.FAILED
    }
}
