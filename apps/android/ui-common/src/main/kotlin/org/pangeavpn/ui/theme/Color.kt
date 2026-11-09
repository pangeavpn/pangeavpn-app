package org.pangeavpn.ui.theme

import androidx.compose.ui.graphics.Color
import org.pangeavpn.core.Accent

// The desktop stylesheet's custom properties; iOS Palette.swift carries the same values.

val Accent.color: Color get() = Color(argb)
val Accent.hover: Color get() = Color(hoverArgb)

/** Text and glyphs on an accent or error fill, the same in both themes. */
val OnAccent = Color(0xFFFBF9F2)

val DarkBackground = Color(0xFF141312)
val DarkSurface = Color(0xFF1E1C1C)
val DarkSurfaceElevated = Color(0xFF272424)
val DarkOnSurface = Color(0xFFFBF9F2)
val DarkOnSurfaceSecondary = Color(0xFFAEA299)
val DarkOnSurfaceMuted = Color(0xFF84847C)
val DarkOutline = Color(0xFF332F2F)
val DarkOutlineSubtle = Color(0xFF272424)

val LightBackground = Color(0xFFFBF9F2)
val LightSurface = Color(0xFFFFFFFF)
val LightSurfaceElevated = Color(0xFFFFFFFF)
val LightOnSurface = Color(0xFF1E1C1C)
val LightOnSurfaceSecondary = Color(0xFF5F5B53)
val LightOnSurfaceMuted = Color(0xFF6D685F)
val LightOutline = Color(0xFFDDD8CF)
val LightOutlineSubtle = Color(0xFFEEE9E0)

val StateConnected = Color(0xFF10B981)
val StateConnecting = Color(0xFFF59E0B)
val StateError = Color(0xFFF43F5E)

/** The off tone of an idle status dot. */
val StateDisconnected = Color(0xFF5A5654)
