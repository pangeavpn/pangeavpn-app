package org.pangeavpn.ui.components

/** Formats a byte count as B/KB/MB/GB/TB with one decimal place. */
fun humanBytes(n: Long): String {
    if (n < 1024) return "$n B"
    val units = arrayOf("KB", "MB", "GB", "TB")
    var value = n / 1024.0
    var unitIndex = 0
    while (value >= 1024 && unitIndex < units.lastIndex) {
        value /= 1024
        unitIndex++
    }
    return "%.1f %s".format(value, units[unitIndex])
}
