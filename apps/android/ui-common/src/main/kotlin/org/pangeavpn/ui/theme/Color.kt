package org.pangeavpn.ui.theme

import androidx.compose.ui.graphics.Color

// Ported from apps/desktop/src/renderer/styles.css custom properties.

// --accent / --accent-hover / --accent-text (identical in both themes)
val BrandOrange = Color(0xFFC3562B)
val BrandOrangeHover = Color(0xFFA84A25)
val BrandOrangeText = Color(0xFFFBF9F2)

// Dark theme — :root
val DarkBackground = Color(0xFF141312) // --bg-base
val DarkSurface = Color(0xFF1E1C1C) // --bg-surface
val DarkSurfaceElevated = Color(0xFF272424) // --bg-elevated
val DarkOnSurface = Color(0xFFFBF9F2) // --text-primary
val DarkOnSurfaceSecondary = Color(0xFFAEA299) // --text-secondary
val DarkOnSurfaceMuted = Color(0xFF84847C) // --text-muted
val DarkOutline = Color(0xFF332F2F) // --border
val DarkOutlineSubtle = Color(0xFF272424) // --border-subtle

// Light theme — body[data-theme="light"]
val LightBackground = Color(0xFFFBF9F2) // --bg-base
val LightSurface = Color(0xFFFFFFFF) // --bg-surface
val LightSurfaceElevated = Color(0xFFFFFFFF) // --bg-elevated
val LightOnSurface = Color(0xFF1E1C1C) // --text-primary
val LightOnSurfaceSecondary = Color(0xFF5F5B53) // --text-secondary
val LightOnSurfaceMuted = Color(0xFF6D685F) // --text-muted
val LightOutline = Color(0xFFDDD8CF) // --border
val LightOutlineSubtle = Color(0xFFEEE9E0) // --border-subtle

// Semantic state colors — --success/--warning/--danger, same hex in both themes.
val StateConnected = Color(0xFF10B981)
val StateConnecting = Color(0xFFF59E0B)
val StateError = Color(0xFFF43F5E)

// Idle/off tone — --pill-dot-off, used for the disconnected dot.
val StateDisconnected = Color(0xFF5A5654)
