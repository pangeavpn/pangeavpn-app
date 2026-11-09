package org.pangeavpn.core.connection

import org.pangeavpn.core.REGION_SLOT_COUNT

/** Main-screen sizes in dp, as plain numbers so the rules stay free of Compose. */
object MapSizing {
    const val MINIMUM = 80f
    const val MAXIMUM = 288f

    const val WIDE_SHARE = 0.4f
    const val WIDE_COLUMN_MAXIMUM = 500f
    const val WIDE_SCALE = 1.25f
    const val WIDE_BLEED = 0.1875f

    /** The map gives way first, so the rest fits before the screen has to scroll. */
    fun side(viewport: Float, content: Float, width: Float): Float =
        (viewport - content).coerceIn(MINIMUM, MAXIMUM).coerceAtMost(width).coerceAtLeast(0f)

    /** Spare height goes around the map, so a tall screen keeps the hero below it, not floating. */
    fun band(viewport: Float, content: Float, side: Float): Float = maxOf(side, viewport - content)

    fun wideColumn(width: Float): Float = (width * WIDE_SHARE).coerceAtMost(WIDE_COLUMN_MAXIMUM).coerceAtLeast(0f)

    /** A short stage shrinks the map rather than cropping it. */
    fun wideSide(column: Float, height: Float): Float = (column * WIDE_SCALE).coerceAtMost(height).coerceAtLeast(0f)
}

/** The parts of the region section that stay put however many rows it lists. */
data class SlotMetrics(val label: Float = 0f, val row: Float = 0f, val door: Float = 0f) {
    val measured: Boolean get() = label > 0 && row > 0 && door > 0
    val chrome: Float get() = label + door + SlotBudget.SPACING * 2
    val pitch: Float get() = row + SlotBudget.SPACING
}

/** With the facts gone, an idle screen lists as many regions as it can while the map stays readable. */
object SlotBudget {
    const val SPACING = 8f
    const val IDLE_MAXIMUM = 5
    const val COMFORTABLE_MAP = 120f
    const val WIDE_MAXIMUM = 8

    fun idleRows(viewport: Float, fixed: Float, metrics: SlotMetrics): Int {
        val base = REGION_SLOT_COUNT
        if (!metrics.measured) return base
        val map = { rows: Int -> viewport - fixed - metrics.chrome - rows * metrics.pitch }
        (base + 1..IDLE_MAXIMUM).lastOrNull { map(it) >= COMFORTABLE_MAP }?.let { return it }
        return if (map(base + 1) >= MapSizing.MINIMUM) base + 1 else base
    }

    /** The wide hero column has no map to keep, so its list takes every whole row it has room for. */
    fun wideRows(viewport: Float, fixed: Float, metrics: SlotMetrics): Int {
        val base = REGION_SLOT_COUNT
        if (!metrics.measured) return base
        val room = viewport - fixed - metrics.chrome
        return (base..WIDE_MAXIMUM).lastOrNull { it * metrics.pitch <= room } ?: base
    }
}
