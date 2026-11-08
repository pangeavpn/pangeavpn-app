package org.pangeavpn.core.viewmodel

import kotlin.test.Test
import kotlin.test.assertFalse
import kotlin.test.assertTrue
import kotlinx.serialization.decodeFromString
import kotlinx.serialization.json.Json
import org.pangeavpn.core.model.Server
import org.pangeavpn.core.model.Session

private val node = Server(id = "eu-1", name = "Node", region = "eu", country = "GB", load = 10)

class SignedInLoadTest {

    @Test
    fun `a list the session just fetched is shown without asking the hub again`() {
        assertTrue(usesFetchedList(Session(servers = listOf(node))))
    }

    @Test
    fun `an empty or stored list is fetched again`() {
        assertFalse(usesFetchedList(Session()))
        assertFalse(usesFetchedList(Session(servers = listOf(node), cached = true)))
    }

    @Test
    fun `the session decodes the cached flag from the core`() {
        val json = Json { ignoreUnknownKeys = true }
        assertTrue(json.decodeFromString<Session>("""{"email":"","name":"","servers":[],"cached":true}""").cached)
        assertFalse(json.decodeFromString<Session>("""{"email":"","name":"","servers":[]}""").cached)
    }
}
