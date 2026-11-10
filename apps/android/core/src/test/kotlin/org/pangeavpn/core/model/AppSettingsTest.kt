package org.pangeavpn.core.model

import kotlin.test.Test
import kotlin.test.assertEquals
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.encodeToString
import kotlinx.serialization.json.Json

class AppSettingsTest {
    // Matches TunnelBridge's decoder.
    private val json = Json { ignoreUnknownKeys = true }

    @Test
    fun `a blob from before the race setting decodes with racing on`() {
        val settings = json.decodeFromString<AppSettings>(
            """{"preferredTransport":"auto","mtu":1380,"deadDrop":true}""",
        )
        assertTrue(settings.transportRace)
    }

    @Test
    fun `an explicit false survives decode`() {
        val settings = json.decodeFromString<AppSettings>("""{"transportRace":false}""")
        assertFalse(settings.transportRace)
    }

    @Test
    fun `the race setting round trips`() {
        val off = AppSettings(transportRace = false)
        assertEquals(off, json.decodeFromString(json.encodeToString(off)))
        assertTrue(AppSettings().transportRace)
    }

    // Go keeps postQuantum in the same blob; dropping it here reset it on every save.
    @Test
    fun `post-quantum survives a round trip`() {
        val on = json.decodeFromString<AppSettings>("""{"postQuantum":true}""")
        assertTrue(on.postQuantum)
        assertTrue(json.decodeFromString<AppSettings>(json.encodeToString(on)).postQuantum)
        assertFalse(AppSettings().postQuantum)
    }
}
