package org.pangeavpn.core.session

import java.util.Calendar
import java.util.Date
import java.util.GregorianCalendar
import java.util.TimeZone

private val STAMP = Regex("""(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})""")
private val DAY = Regex("""(\d{4})-(\d{2})-(\d{2})""")

/** Hub timestamps: RFC 3339 with or without fractions, or a bare calendar date. */
object HubDate {
    fun parse(raw: String, timeZone: TimeZone = TimeZone.getDefault()): Date? {
        val text = raw.trim()
        STAMP.matchEntire(text)?.let { match ->
            val (year, month, day, hour, minute, second, fraction, offset) = match.destructured
            val zone = TimeZone.getTimeZone(if (offset == "Z") "UTC" else "GMT$offset")
            val millis = fraction.padEnd(3, '0').take(3).toInt()
            return instant(
                zone, year.toInt(), month.toInt(), day.toInt(), hour.toInt(), minute.toInt(), second.toInt(), millis,
            )
        }
        // A bare date means that day where the reader is, not midnight UTC.
        val (year, month, day) = DAY.matchEntire(text)?.destructured ?: return null
        return instant(timeZone, year.toInt(), month.toInt(), day.toInt())
    }

    // Lenient, so a midnight that a clock change skips still lands on its day;
    // an impossible date rolls over to another day and is refused.
    private fun instant(
        zone: TimeZone, year: Int, month: Int, day: Int,
        hour: Int = 0, minute: Int = 0, second: Int = 0, millis: Int = 0,
    ): Date? {
        if (hour > 23 || minute > 59 || second > 59) return null
        val calendar = GregorianCalendar(zone)
        calendar.clear()
        calendar.set(year, month - 1, day, hour, minute, second)
        calendar.set(Calendar.MILLISECOND, millis)
        val date = calendar.time
        val sameDay = calendar.get(Calendar.YEAR) == year &&
            calendar.get(Calendar.MONTH) == month - 1 &&
            calendar.get(Calendar.DAY_OF_MONTH) == day
        return date.takeIf { sameDay }
    }
}
