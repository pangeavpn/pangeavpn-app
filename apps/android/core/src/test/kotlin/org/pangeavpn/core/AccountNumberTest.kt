package org.pangeavpn.core

import kotlin.test.Test
import kotlin.test.assertEquals
import org.pangeavpn.core.util.formatAccountNumberInput
import org.pangeavpn.core.util.normalizeAccountNumber

class AccountNumberTest {

    @Test
    fun `legacy digit-only tokens pass through untouched`() {
        assertEquals("0123456789012345", normalizeAccountNumber("0123456789012345"))
        assertEquals("0123456789012345", formatAccountNumberInput("0123456789012345"))
        assertEquals("0123", formatAccountNumberInput("0123"))
    }

    @Test
    fun `normalize uppercases, strips separators and folds O I L`() {
        assertEquals(
            "ABCDEFGHJKMNPQRSTVWXYZ01",
            normalizeAccountNumber("abcd-efgh jkmn-pqrs-tvwx-yz01"),
        )
        assertEquals("011", normalizeAccountNumber("oil"))
        assertEquals("ABCDEFGH", normalizeAccountNumber("  ABCD-EFGH  "))
    }

    @Test
    fun `normalize is idempotent`() {
        val once = normalizeAccountNumber("abcd-efgh-jkmn-pqrs-tvwx-yz01")
        assertEquals(once, normalizeAccountNumber(once))
    }

    @Test
    fun `format groups in blocks of four with no trailing dash`() {
        assertEquals("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01", formatAccountNumberInput("abcdefghjkmnpqrstvwxyz01"))
        assertEquals("ABCD", formatAccountNumberInput("abcd"))
        assertEquals("ABCD-E", formatAccountNumberInput("abcde"))
    }

    @Test
    fun `format is stable when re-applied to its own output`() {
        val once = formatAccountNumberInput("abcdefghjkmnpqrstvwxyz01")
        assertEquals(once, formatAccountNumberInput(once))
    }

    @Test
    fun `nothing is rejected for length or charset`() {
        assertEquals("ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6", formatAccountNumberInput("abcdefghjkmnpqrstvwxyz0123456"))
        assertEquals("AB!CD", normalizeAccountNumber("ab!cd"))
    }

    @Test
    fun `only ASCII digits count as a legacy token`() {
        // isDigit() would treat these as a legacy token and pass them through,
        // diverging from desktop's /^\d*$/ for the app's ar and fa locales.
        assertEquals("١٢٣٤", formatAccountNumberInput("١٢٣٤"))
        assertEquals("١٢٣٤-٥٦٧٨", formatAccountNumberInput("١٢٣٤٥٦٧٨"))
    }

    @Test
    fun `empty input stays empty`() {
        assertEquals("", normalizeAccountNumber(""))
        assertEquals("", formatAccountNumberInput(""))
    }
}
