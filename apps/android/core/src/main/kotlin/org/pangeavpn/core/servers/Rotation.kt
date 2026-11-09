package org.pangeavpn.core.servers

data class RotationPlan(
    // Servers to try in order: this cycle's fresh ones, then the visited ones as a fallback.
    val plan: List<String>,
    // The visited list to carry forward; cleared once every alternative has had its turn.
    val visited: List<String>,
)

/** "Switch to another server" walks the retry order without landing twice on one server per cycle. */
object Rotation {
    fun plan(order: List<String>, currentId: String, visited: List<String>): RotationPlan {
        val alternatives = order.filter { it != currentId }
        val stillListed = visited.filter { it in alternatives }
        val fresh = alternatives.filter { it !in stillListed }
        if (fresh.isEmpty()) return RotationPlan(alternatives, emptyList())
        return RotationPlan(fresh + alternatives.filter { it in stillListed }, stillListed)
    }

    /** The server left, every attempt before the landing, and the landing (or every attempt on failure). */
    fun record(visited: List<String>, currentId: String, plan: List<String>, landedId: String?): List<String> {
        val landedAt = landedId?.let { plan.indexOf(it) } ?: -1
        val attempted = if (landedAt >= 0) plan.take(landedAt + 1) else plan
        return (visited + currentId + attempted).distinct()
    }
}
