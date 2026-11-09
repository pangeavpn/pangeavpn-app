package org.pangeavpn.core.diagnostics

import android.content.Context
import android.os.Build
import androidx.core.content.pm.PackageInfoCompat
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.add
import kotlinx.serialization.json.buildJsonObject
import kotlinx.serialization.json.encodeToJsonElement
import kotlinx.serialization.json.put
import kotlinx.serialization.json.putJsonArray
import kotlinx.serialization.json.putJsonObject
import org.pangeavpn.core.Prefs
import org.pangeavpn.core.model.AppSettings
import org.pangeavpn.core.model.TunnelStatus

/** The build and OS a copied diagnostics summary names. */
data class DiagnosticsClient(val appVersion: String, val osVersion: String) {
    companion object {
        @Suppress("DEPRECATION")
        fun current(context: Context): DiagnosticsClient {
            val info = context.packageManager.getPackageInfo(context.packageName, 0)
            val version = info.versionName.orEmpty().ifEmpty { "unknown" }
            val build = PackageInfoCompat.getLongVersionCode(info)
            return DiagnosticsClient("$version ($build)", Build.VERSION.RELEASE)
        }
    }
}

/** Desktop words a refused report differently from one that never arrived. */
enum class ReportFailure { UNREACHABLE, REJECTED }

sealed interface ReportPhase {
    data object Compose : ReportPhase
    data object Sending : ReportPhase
    data class Sent(val code: String) : ReportPhase
    data class Failed(val reason: ReportFailure) : ReportPhase
}

object DiagnosticsReport {
    const val NOTE_LIMIT = 500
    const val TAIL_LINES = 200

    private const val REJECTED_MARKER = "DIAGNOSTICS_REJECTED"

    private val pretty = Json {
        prettyPrint = true
        encodeDefaults = true
    }

    /** The note's length as the limit counts it, for the n/500 counter. */
    fun noteLength(note: String): Int = note.codePointCount(0, note.length)

    /** The note cut at the limit without splitting a character, for the field as it is typed. */
    fun limited(note: String): String =
        if (noteLength(note) <= NOTE_LIMIT) note else note.substring(0, note.offsetByCodePoints(0, NOTE_LIMIT))

    /** What is sent: trimmed, then capped. */
    fun cappedNote(note: String): String = limited(note.trim())

    fun failure(error: Throwable): ReportFailure =
        if (error.message.orEmpty().contains(REJECTED_MARKER)) ReportFailure.REJECTED else ReportFailure.UNREACHABLE

    fun tail(text: String, lines: Int = TAIL_LINES): List<String> =
        text.lineSequence().filter { it.isNotEmpty() }.toList().takeLast(lines)

    /** Pretty-printed with sorted keys. Named fields only, so a secret added to a model later never reaches the clipboard. */
    fun json(
        client: DiagnosticsClient,
        status: TunnelStatus,
        settings: AppSettings,
        prefs: Prefs,
        appLog: String,
        at: Date = Date(),
    ): String {
        val stamp = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss.SSS'Z'", Locale.US)
        stamp.timeZone = TimeZone.getTimeZone("UTC")
        val snapshot = buildJsonObject {
            put("generatedAtUtc", stamp.format(at))
            put("timezone", TimeZone.getDefault().id)
            put("appVersion", client.appVersion)
            put("platform", "android")
            put("osVersion", client.osVersion)
            put("status", pretty.encodeToJsonElement(status))
            putJsonObject("settings") {
                put("preferredTransport", settings.preferredTransport)
                putJsonArray("customDns") { settings.customDns.forEach { add(it) } }
                put("mtu", settings.mtu)
                put("allowLan", settings.allowLan)
                put("autoConnect", settings.autoConnect)
                put("deadDrop", settings.deadDrop)
                put("postQuantum", settings.postQuantum)
                put("lastServerId", settings.lastServerId)
                put("hubMethods", pretty.encodeToJsonElement(settings.hubMethods))
            }
            putJsonObject("tunnelProfile") {
                put("autoConnect", prefs.autoConnect)
                put("lockdown", prefs.lockdown)
                put("splitTunnelEnabled", prefs.splitTunnelEnabled)
                put("multihopEnabled", prefs.multihopEnabled)
            }
            putJsonArray("appLogTail") { tail(appLog).forEach { add(it) } }
        }
        return pretty.encodeToString(JsonElement.serializer(), sorted(snapshot))
    }

    private fun sorted(element: JsonElement): JsonElement = when (element) {
        is JsonObject -> JsonObject(element.toSortedMap().mapValues { sorted(it.value) })
        is JsonArray -> JsonArray(element.map { sorted(it) })
        else -> element
    }
}
