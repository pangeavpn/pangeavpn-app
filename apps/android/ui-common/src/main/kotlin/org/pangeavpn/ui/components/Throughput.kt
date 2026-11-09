package org.pangeavpn.ui.components

import java.util.Locale

/** B below 1024, else one decimal in 1024-based KB/MB/GB/TB; a dot and ASCII digits in every language, as on iOS. */
fun humanBytes(n: Long): String {
    if (n < 1024) return "$n B"
    val units = arrayOf("KB", "MB", "GB", "TB")
    var value = n / 1024.0
    var unitIndex = 0
    while (value >= 1024 && unitIndex < units.lastIndex) {
        value /= 1024
        unitIndex++
    }
    return String.format(Locale.ROOT, "%.1f %s", value, units[unitIndex])
}
