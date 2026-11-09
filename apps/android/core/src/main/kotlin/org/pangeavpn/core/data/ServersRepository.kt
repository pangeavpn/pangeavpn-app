package org.pangeavpn.core.data

import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.flow.combine
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import org.pangeavpn.core.AUTO_TRANSPORT
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.REGION_SLOT_COUNT
import org.pangeavpn.core.Region
import org.pangeavpn.core.TunnelBridge
import org.pangeavpn.core.groupRegions
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session
import org.pangeavpn.core.orderByRecent
import org.pangeavpn.core.promoteRecent
import org.pangeavpn.core.regionKeyOf
import org.pangeavpn.core.regionOfServer
import org.pangeavpn.core.regionSlots
import org.pangeavpn.core.regionsSupporting
import org.pangeavpn.core.resolveSelection
import org.pangeavpn.core.retryOrder
import org.pangeavpn.core.servers.Rotation
import org.pangeavpn.core.servers.RotationPlan
import org.pangeavpn.core.session.LoginErrorKind
import org.pangeavpn.core.session.LoginErrors
import org.pangeavpn.core.session.coreErrorText
import org.pangeavpn.core.startCandidates
import org.pangeavpn.core.util.runCatchingCancellable

data class ServersState(
    val regions: List<Region> = emptyList(),
    val selected: Server? = null,
    val loading: Boolean = false,
    // Why the last load or save failed, already in its general form; `ui.text.generalErrorText` words it.
    val error: LoginErrorKind? = null,
    // Set only when a node was chosen by hand; otherwise each region connects to its lightest node.
    val pinnedServerId: String? = null,
    val recentRegionKeys: List<String> = emptyList(),
    val rotationVisited: List<String> = emptyList(),
) {
    val servers: List<Server> get() = regions.flatMap { it.servers }

    val selectedRegionKey: String? get() = selected?.let { regionOfServer(regions, it.id)?.key }

    fun server(id: String): Server? = servers.firstOrNull { it.id == id }

    fun regionsFor(transport: String): List<Region> = regionsSupporting(transport, regions)

    /** Regions that carry the transport, recents first, the selected one always leading. */
    fun slots(transport: String, count: Int = REGION_SLOT_COUNT): List<Region> =
        regionSlots(orderByRecent(regionsFor(transport), recentRegionKeys), selectedRegionKey, count)

    /** A fixed transport stays on the chosen server; auto falls back as desktop does. */
    fun candidates(transport: String): List<String> {
        val chosen = selected ?: return emptyList()
        if (transport != AUTO_TRANSPORT) return listOf(chosen.id)
        return startCandidates(chosen.id, visible(transport))
    }

    fun rotation(transport: String): RotationPlan {
        val current = selected?.id ?: return RotationPlan(emptyList(), rotationVisited)
        return Rotation.plan(retryOrder(current, visible(transport)), current, rotationVisited)
    }

    private fun visible(transport: String): List<Server> = servers.filter { it.supports(transport) }
}

/** The server list, the pick and its history. Main-thread only, apart from [reset]. */
object ServersRepository {
    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.Main.immediate)

    private val _state = MutableStateFlow(ServersState())
    val state: StateFlow<ServersState> = _state.asStateFlow()

    // Bumped by reset, so a refresh still in flight for the old account is dropped.
    @Volatile private var generation = 0

    // A server the tunnel reported before the list had loaded; the next load selects it.
    @Volatile private var pendingFollow: String? = null

    fun candidates(transport: String): List<String> = _state.value.candidates(transport)

    fun slots(transport: String, count: Int = REGION_SLOT_COUNT): List<Region> = _state.value.slots(transport, count)

    fun rotation(transport: String): RotationPlan = _state.value.rotation(transport)

    /** Asks the hub for the list; a call that overlaps one in flight is ignored. */
    fun refresh() {
        scope.launch { load() }
    }

    /** The list a restore or sign-in just fetched, so launch does not ask the hub for it a second time. */
    suspend fun use(listed: List<Server>) {
        if (_state.value.loading) return
        val started = generation
        val last = lastServerId()
        if (started == generation) show(listed, last)
    }

    /** Sign-out, account switch, wipe: the next account never sees this one's servers or history. */
    fun reset() {
        generation++
        pendingFollow = null
        AppPrefs.update { it.copy(recentRegions = emptyList(), rotationVisited = emptyList(), pinnedServerId = null) }
        _state.value = ServersState()
    }

    /** Takes the region's lightest node and drops any pin. */
    suspend fun select(region: Region) {
        if (region.servers.isEmpty()) return
        remember(region.key)
        adopt(region.best, pinnedId = null)
    }

    suspend fun select(server: Server, pinned: Boolean) {
        remember(regionKeyOf(server))
        adopt(server, pinnedId = if (pinned) server.id else null)
    }

    /** The tunnel landed elsewhere, on a fallback candidate; a pin naming another node is stale. */
    suspend fun follow(serverId: String) {
        val landed = _state.value.server(serverId)
        if (landed == null) {
            pendingFollow = serverId
            return
        }
        pendingFollow = null
        if (landed.id == _state.value.selected?.id) return
        adopt(landed, pinnedId = serverId.takeIf { AppPrefs.value.pinnedServerId == it })
    }

    /** Also saved as lastServerId, so the tile, boot and always-on starts land on the same server. */
    suspend fun adopt(server: Server, pinnedId: String?) {
        AppPrefs.update { it.copy(pinnedServerId = pinnedId) }
        publish { it.copy(selected = server) }
        runCatchingCancellable { SettingsRepository.edit { it.copy(lastServerId = server.id) } }
            .onFailure { failure -> publish { it.copy(error = general(failure)) } }
    }

    fun recordRotation(rotation: RotationPlan, leaving: String, landedId: String?) {
        val next = Rotation.record(rotation.visited, leaving, rotation.plan, landedId)
        AppPrefs.update { it.copy(rotationVisited = next) }
        publish()
    }

    private suspend fun load() {
        if (_state.value.loading) return
        publish { it.copy(loading = true) }
        val started = generation
        val loaded = runCatchingCancellable { TunnelBridge.listServers() to lastServerId() }
        if (started != generation) return
        loaded.onSuccess { (listed, last) -> show(listed, last) }
            .onFailure { failure -> publish { it.copy(error = general(failure)) } }
        publish { it.copy(loading = false) }
    }

    private fun show(listed: List<Server>, lastServerId: String?) {
        val preferred = listOfNotNull(pendingFollow, _state.value.selected?.id)
            .firstOrNull { id -> listed.any { it.id == id } }
        val id = resolveSelection(listed, preferred.orEmpty(), lastServerId)
        pendingFollow = null
        publish {
            it.copy(
                regions = groupRegions(listed),
                selected = if (id.isEmpty()) null else listed.firstOrNull { server -> server.id == id },
                error = null,
            )
        }
    }

    private fun remember(regionKey: String) {
        AppPrefs.update { it.copy(recentRegions = promoteRecent(it.recentRegions, regionKey)) }
        publish()
    }

    // AppPrefs holds the pin and the history; every publish copies them in so one flow has it all.
    private fun publish(change: (ServersState) -> ServersState = { it }) {
        val prefs = AppPrefs.value
        _state.update {
            change(it).copy(
                pinnedServerId = prefs.pinnedServerId,
                recentRegionKeys = prefs.recentRegions,
                rotationVisited = prefs.rotationVisited,
            )
        }
    }

    private fun general(failure: Throwable): LoginErrorKind = LoginErrors.general(coreErrorText(failure))

    private suspend fun lastServerId(): String? =
        runCatchingCancellable { SettingsRepository.ensureLoaded() }
            .map { SettingsRepository.settings.value.lastServerId }
            .getOrNull()

    // Posted, not run inline: the collector reaches into other repositories, which must not happen
    // while this one is still being constructed.
    init {
        scope.launch(Dispatchers.Main) {
            var wasSignedIn = false
            combine(SessionRepository.session, SessionRepository.restored) { session, restored -> session to restored }
                .collect { (session, restored) ->
                    when {
                        // As on iOS, a launch that restores no session also forgets the last one's history.
                        session == null -> if (restored || wasSignedIn) reset()
                        usesFetchedList(session) -> scope.launch { use(session.servers) }
                        !wasSignedIn -> refresh()
                    }
                    wasSignedIn = session != null
                }
        }
    }
}

/** Restore and sign-in already fetched the list; only an empty or stored one is fetched again. */
internal fun usesFetchedList(session: Session): Boolean = session.servers.isNotEmpty() && !session.cached
