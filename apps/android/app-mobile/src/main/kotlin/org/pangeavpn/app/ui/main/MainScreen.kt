package org.pangeavpn.app.ui.main

import androidx.compose.animation.AnimatedVisibility
import androidx.compose.animation.expandVertically
import androidx.compose.animation.fadeIn
import androidx.compose.animation.fadeOut
import androidx.compose.animation.shrinkVertically
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.BoxWithConstraints
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.WindowInsets
import androidx.compose.foundation.layout.WindowInsetsSides
import androidx.compose.foundation.layout.fillMaxHeight
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.offset
import androidx.compose.foundation.layout.only
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.safeDrawing
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.layout.widthIn
import androidx.compose.foundation.layout.windowInsetsBottomHeight
import androidx.compose.foundation.layout.windowInsetsPadding
import androidx.compose.foundation.layout.wrapContentHeight
import androidx.compose.foundation.layout.wrapContentWidth
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.CompositionLocalProvider
import androidx.compose.runtime.LaunchedEffect
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableFloatStateOf
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.alpha
import androidx.compose.ui.draw.drawBehind
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.layout.Layout
import androidx.compose.ui.layout.onSizeChanged
import androidx.compose.ui.platform.LocalDensity
import androidx.compose.ui.semantics.clearAndSetSemantics
import androidx.compose.ui.unit.Constraints
import androidx.compose.ui.unit.Density
import androidx.compose.ui.unit.Dp
import androidx.compose.ui.unit.IntSize
import androidx.compose.ui.unit.coerceAtLeast
import androidx.compose.ui.unit.coerceIn
import androidx.compose.ui.unit.dp
import kotlinx.coroutines.flow.distinctUntilChanged
import kotlinx.coroutines.flow.drop
import kotlinx.coroutines.flow.map
import org.pangeavpn.app.BuildConfig
import org.pangeavpn.app.ui.settings.SettingsRoute
import org.pangeavpn.core.AppPrefs
import org.pangeavpn.core.REGION_SLOT_COUNT
import org.pangeavpn.core.connection.HeroRules
import org.pangeavpn.core.connection.MainAction
import org.pangeavpn.core.connection.MapSizing
import org.pangeavpn.core.connection.SlotBudget
import org.pangeavpn.core.connection.SlotMetrics
import org.pangeavpn.core.data.ConnectionRepository
import org.pangeavpn.core.data.SessionRepository
import org.pangeavpn.core.model.ConnState
import org.pangeavpn.core.model.Subscription
import org.pangeavpn.core.session.showsSubscriptionBanner
import org.pangeavpn.ui.components.DriftMap
import org.pangeavpn.ui.components.EM_DASH
import org.pangeavpn.ui.components.HeroFacts
import org.pangeavpn.ui.components.MainActionButton
import org.pangeavpn.ui.components.PangeaHeader
import org.pangeavpn.ui.components.ToastCenter
import org.pangeavpn.ui.components.ToastStyle
import org.pangeavpn.ui.theme.Motion
import org.pangeavpn.ui.theme.reducedMotion

private val WIDE_MIN_WIDTH = 700.dp
private val GUTTER = 20.dp
private val TOP_INSET = 2.dp
private val BOTTOM_INSET = 12.dp
private val SLOTS_TOP = 16.dp
private val FACTS_TOP = 14.dp
private val HERO_WIDTH = 560.dp
private val WIDE_GUTTER = 28.dp
private val WIDE_STAGE_INSET = 16.dp
private val WIDE_BANNER_GAP = 14.dp
private val EXPIRED_WIDTH = 400.dp

// Tall enough for eight idle rows at the default text size; it grows with the text.
private val WIDE_COLUMN_CAP = 820.dp

// iOS caps Dynamic Type at xxxLarge on this screen only, so the stack can still fit without scrolling.
private const val MAX_FONT_SCALE = 1.35f

private data class Stage(
    val display: ConnState,
    val reconnecting: Boolean,
    val entitled: Boolean,
    val subscription: Subscription?,
    val action: MainAction,
    val onAction: () -> Unit,
    val onOpenRegions: () -> Unit,
)

@Composable
fun MainScreen(onOpenSettings: (SettingsRoute?) -> Unit, onOpenRegions: () -> Unit) {
    val status by ConnectionRepository.status.collectAsState()
    val connection by ConnectionRepository.state.collectAsState()
    val prefs by AppPrefs.state.collectAsState()
    val subscription by SessionRepository.subscription.collectAsState()

    val entitled = subscription?.entitled != false
    val display = HeroRules.display(status.state, connection.busy, connection.switching)
    val reconnecting = status.reconnecting && status.state == ConnState.CONNECTING
    val action = MainAction.resolve(
        state = status.state,
        busy = connection.busy,
        switching = connection.switching,
        reconnecting = reconnecting,
        lockdownBlocks = HeroRules.lockdownBlocks(prefs.lockdown, status.state),
        entitled = entitled,
    )

    // The detail line carries an idle failure; one that lands while the tunnel is up needs a toast.
    val currentDisplay by rememberUpdatedState(display)
    LaunchedEffect(Unit) {
        ConnectionRepository.state.map { it.error }.distinctUntilChanged().drop(1).collect { message ->
            if (!message.isNullOrEmpty() && !HeroRules.carriesError(currentDisplay)) {
                ToastCenter.show(message, ToastStyle.WARNING)
            }
        }
    }

    val stage = Stage(
        display = display,
        reconnecting = reconnecting,
        entitled = entitled,
        subscription = subscription,
        action = action,
        // The Lockdown switch lives on the Startup page, so the note opens settings already there.
        onAction = {
            if (action == MainAction.LOCKDOWN) onOpenSettings(SettingsRoute.STARTUP) else ConnectionRepository.toggle()
        },
        onOpenRegions = onOpenRegions,
    )

    CappedFontScale {
        Column(
            modifier = Modifier
                .fillMaxSize()
                .background(MaterialTheme.colorScheme.background)
                .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Horizontal + WindowInsetsSides.Top)),
        ) {
            PangeaHeader(version = "v${BuildConfig.VERSION_NAME}", onMenu = { onOpenSettings(null) })
            BoxWithConstraints(modifier = Modifier.fillMaxWidth().weight(1f)) {
                if (maxWidth >= WIDE_MIN_WIDTH) WideStage(stage) else CompactStage(stage)
            }
        }
    }
}

@Composable
private fun CappedFontScale(content: @Composable () -> Unit) {
    val density = LocalDensity.current
    if (density.fontScale <= MAX_FONT_SCALE) {
        content()
    } else {
        CompositionLocalProvider(LocalDensity provides Density(density.density, MAX_FONT_SCALE), content = content)
    }
}

@Composable
private fun CompactStage(stage: Stage) {
    val density = LocalDensity.current
    var bannerHeight by remember { mutableFloatStateOf(0f) }
    var heroHeight by remember { mutableFloatStateOf(0f) }
    val slotMetrics = remember { mutableStateOf(SlotMetrics()) }
    val banner = showsSubscriptionBanner(stage.subscription)
    val showsFacts = HeroRules.showsFacts(stage.display)

    Column(modifier = Modifier.fillMaxSize()) {
        BoxWithConstraints(modifier = Modifier.fillMaxWidth().weight(1f)) {
            val viewport = (maxHeight - TOP_INSET - BOTTOM_INSET).coerceAtLeast(0.dp)
            val rows = if (showsFacts) {
                REGION_SLOT_COUNT
            } else {
                val fixed = (if (banner) bannerHeight else 0f) + heroHeight + SLOTS_TOP.value
                SlotBudget.idleRows(viewport.value, fixed, slotMetrics.value)
            }
            Column(
                modifier = Modifier
                    .fillMaxSize()
                    .verticalScroll(rememberScrollState()),
                horizontalAlignment = Alignment.CenterHorizontally,
            ) {
                MapStack(
                    viewport = viewport,
                    map = { DriftMap(stage.display) },
                    modifier = Modifier
                        .widthIn(max = HERO_WIDTH + GUTTER * 2)
                        .fillMaxWidth()
                        .padding(horizontal = GUTTER)
                        .padding(top = TOP_INSET, bottom = BOTTOM_INSET),
                ) {
                    Column(modifier = Modifier.fillMaxWidth()) {
                        Banner(stage.subscription, banner, Modifier.padding(top = 6.dp)) {
                            bannerHeight = density.heightDp(it)
                        }
                        if (stage.entitled) {
                            MainHero(
                                display = stage.display,
                                reconnecting = stage.reconnecting,
                                modifier = Modifier
                                    .onSizeChanged { heroHeight = density.heightDp(it) }
                                    .padding(top = 10.dp),
                            )
                            Facts(showsFacts)
                            RegionSlots(rows, slotMetrics, stage.onOpenRegions, Modifier.padding(top = SLOTS_TOP))
                        } else {
                            ExpiredPanel(Modifier.padding(top = 8.dp))
                        }
                    }
                }
            }
        }
        ActionBar(stage.action, stage.onAction, inset = GUTTER, aboveGestureBar = true)
    }
}

/** The map takes the height the rest leaves, between its bounds, then the stack scrolls. */
@Composable
private fun MapStack(
    viewport: Dp,
    map: @Composable () -> Unit,
    modifier: Modifier = Modifier,
    content: @Composable () -> Unit,
) {
    Layout(content = { map(); content() }, modifier = modifier) { measurables, constraints ->
        val width = constraints.maxWidth
        val body = measurables[1].measure(Constraints(minWidth = width, maxWidth = width))
        val side = MapSizing.side(viewport.value, body.height.toDp().value, width.toDp().value).dp.toPx().toInt()
        val band = maxOf(side, viewport.toPx().toInt() - body.height)
        val mapPlaceable = measurables[0].measure(Constraints.fixed(side, side))
        layout(width, body.height + band) {
            mapPlaceable.placeRelative((width - side) / 2, (band - side) / 2)
            body.placeRelative(0, band)
        }
    }
}

// Desktop's stage: the map bleeding off the start edge, a fixed-height hero column beside it.
@Composable
private fun WideStage(stage: Stage) {
    BoxWithConstraints(
        modifier = Modifier
            .fillMaxSize()
            .windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom)),
    ) {
        val column = MapSizing.wideColumn(maxWidth.value).dp
        val room = (maxHeight - WIDE_STAGE_INSET * 2).coerceAtLeast(0.dp)
        val side = MapSizing.wideSide(column.value, room.value).dp
        val columnHeight = room.coerceIn(0.dp, WIDE_COLUMN_CAP * LocalDensity.current.fontScale)

        Row(modifier = Modifier.fillMaxSize()) {
            Box(modifier = Modifier.width(column).fillMaxHeight(), contentAlignment = Alignment.CenterStart) {
                DriftMap(
                    state = stage.display,
                    modifier = Modifier
                        .wrapContentWidth(Alignment.Start, unbounded = true)
                        .offset(x = -side * MapSizing.WIDE_BLEED)
                        .size(side),
                )
            }
            Box(modifier = Modifier.weight(1f).fillMaxHeight(), contentAlignment = Alignment.Center) {
                Column(
                    modifier = Modifier
                        .widthIn(max = HERO_WIDTH + WIDE_GUTTER * 2)
                        .fillMaxWidth()
                        .height(columnHeight),
                ) {
                    BoxWithConstraints(modifier = Modifier.fillMaxWidth().weight(1f)) {
                        val viewport = (maxHeight - TOP_INSET - BOTTOM_INSET).coerceAtLeast(0.dp)
                        Column(modifier = Modifier.fillMaxSize().verticalScroll(rememberScrollState())) {
                            WideContent(stage, viewport)
                        }
                    }
                    ActionBar(stage.action, stage.onAction, inset = WIDE_GUTTER, aboveGestureBar = false)
                }
            }
        }
    }
}

@Composable
private fun WideContent(stage: Stage, viewport: Dp) {
    val density = LocalDensity.current
    var bannerHeight by remember { mutableFloatStateOf(0f) }
    var heroHeight by remember { mutableFloatStateOf(0f) }
    var factsHeight by remember { mutableFloatStateOf(0f) }
    val slotMetrics = remember { mutableStateOf(SlotMetrics()) }
    val banner = showsSubscriptionBanner(stage.subscription)
    val showsFacts = HeroRules.showsFacts(stage.display)
    val shownBanner = if (banner) bannerHeight else 0f
    val facts = if (showsFacts) FACTS_TOP.value + factsHeight else 0f
    val rows = SlotBudget.wideRows(viewport.value, shownBanner + heroHeight + facts + SLOTS_TOP.value, slotMetrics.value)

    Column(
        modifier = Modifier
            .fillMaxWidth()
            .padding(horizontal = WIDE_GUTTER)
            .padding(top = TOP_INSET, bottom = BOTTOM_INSET),
    ) {
        FactsProbe { factsHeight = it }
        Banner(stage.subscription, banner, Modifier.padding(bottom = WIDE_BANNER_GAP)) {
            bannerHeight = density.heightDp(it)
        }
        if (stage.entitled) {
            MainHero(
                display = stage.display,
                reconnecting = stage.reconnecting,
                modifier = Modifier.onSizeChanged { heroHeight = density.heightDp(it) },
            )
            Facts(showsFacts)
            RegionSlots(rows, slotMetrics, stage.onOpenRegions, Modifier.padding(top = SLOTS_TOP))
        } else {
            Box(
                modifier = Modifier
                    .fillMaxWidth()
                    .heightIn(min = (viewport - shownBanner.dp).coerceAtLeast(0.dp)),
                contentAlignment = Alignment.Center,
            ) {
                ExpiredPanel(Modifier.widthIn(max = EXPIRED_WIDTH))
            }
        }
    }
}

// Measured before the facts first show, so connecting trims the list in one move rather than two.
@Composable
private fun FactsProbe(onHeight: (Float) -> Unit) {
    val density = LocalDensity.current
    Box(modifier = Modifier.fillMaxWidth().height(0.dp).clearAndSetSemantics {}) {
        HeroFacts(
            session = EM_DASH,
            down = EM_DASH,
            up = EM_DASH,
            via = EM_DASH,
            modifier = Modifier
                .wrapContentHeight(unbounded = true)
                .alpha(0f)
                .onSizeChanged { onHeight(density.heightDp(it)) },
        )
    }
}

@Composable
private fun Banner(
    subscription: Subscription?,
    visible: Boolean,
    modifier: Modifier,
    onHeight: (IntSize) -> Unit,
) {
    val reduced = reducedMotion()
    AnimatedVisibility(
        visible = visible,
        enter = fadeIn(Motion.state(reduced)) + expandVertically(Motion.layout(reduced)),
        exit = fadeOut(Motion.quick()) + shrinkVertically(Motion.layout(reduced)),
        label = "banner",
    ) {
        SubscriptionBanner(subscription, Modifier.onSizeChanged(onHeight).then(modifier))
    }
}

// The facts leave quickly, so the regions sliding up never pass over a half-faded grid.
@Composable
private fun Facts(visible: Boolean) {
    val reduced = reducedMotion()
    AnimatedVisibility(
        visible = visible,
        enter = fadeIn(Motion.state(reduced)) + expandVertically(Motion.layout(reduced), expandFrom = Alignment.Top),
        exit = fadeOut(Motion.quick()) + shrinkVertically(Motion.layout(reduced), shrinkTowards = Alignment.Top),
        label = "facts",
    ) {
        MainFacts(Modifier.padding(top = FACTS_TOP))
    }
}

@Composable
private fun ActionBar(action: MainAction, onAction: () -> Unit, inset: Dp, aboveGestureBar: Boolean) {
    if (action == MainAction.HIDDEN) {
        if (aboveGestureBar) Spacer(Modifier.windowInsetsBottomHeight(WindowInsets.safeDrawing))
        return
    }
    val background = MaterialTheme.colorScheme.background
    Box(
        modifier = Modifier
            .fillMaxWidth()
            .drawBehind {
                val fade = 16.dp.toPx()
                drawRect(
                    brush = Brush.verticalGradient(listOf(background.copy(alpha = 0f), background), startY = -fade, endY = 0f),
                    topLeft = Offset(0f, -fade),
                    size = Size(size.width, fade),
                )
            }
            .background(background)
            .then(
                if (aboveGestureBar) Modifier.windowInsetsPadding(WindowInsets.safeDrawing.only(WindowInsetsSides.Bottom))
                else Modifier,
            )
            .padding(horizontal = inset, vertical = 10.dp),
        contentAlignment = Alignment.Center,
    ) {
        MainActionButton(action, onAction, Modifier.widthIn(max = HERO_WIDTH))
    }
}
