package org.pangeavpn.core.diagnostics

import android.content.Context
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.data.SettingsRepository
import org.pangeavpn.core.util.runCatchingCancellable

/** The core's log and the Send Report flow. The service shares this process, so there is one log, not two. */
object DiagnosticsStore {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val _appLog = MutableStateFlow("")
    /** The log as last read: redacted by the core, newest line last. */
    val appLog: StateFlow<String> = _appLog.asStateFlow()

    private val _report = MutableStateFlow<ReportPhase>(ReportPhase.Compose)
    /** Where the Send Report sheet stands. Kept here so a code survives the sheet being torn down. */
    val report: StateFlow<ReportPhase> = _report.asStateFlow()

    suspend fun refreshLog(): String = TunnelBridge.logs().also { _appLog.value = it }

    /** Uploads the report with the note trimmed and capped. Ignored unless the sheet is composing. */
    fun sendReport(note: String) {
        if (!_report.compareAndSet(ReportPhase.Compose, ReportPhase.Sending)) return
        scope.launch {
            val sent = runCatchingCancellable { TunnelBridge.sendDiagnostics(DiagnosticsReport.cappedNote(note), "") }
            val phase: ReportPhase = sent.fold(
                onSuccess = { ReportPhase.Sent(it) },
                onFailure = { ReportPhase.Failed(DiagnosticsReport.failure(it)) },
            )
            _report.value = phase
        }
    }

    /** Try Again, and the sheet closing: back to the note, unless a send is still running. */
    fun resetReport() {
        _report.update { if (it == ReportPhase.Sending) it else ReportPhase.Compose }
    }

    /** The Copy Diagnostics summary, read fresh. The account number is never part of it. */
    suspend fun diagnosticsJson(context: Context): String {
        val log = refreshLog()
        runCatchingCancellable { SettingsRepository.ensureLoaded() }
        return DiagnosticsReport.json(
            client = DiagnosticsClient.current(context),
            status = TunnelBridge.status.value,
            settings = SettingsRepository.settings.value,
            prefs = AppPrefs.value,
            appLog = log,
        )
    }

    /** After a wipe, so no log read before it stays on screen. */
    fun reset() {
        _appLog.value = ""
        resetReport()
    }
}
