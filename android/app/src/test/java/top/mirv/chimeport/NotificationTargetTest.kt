package top.mirv.chimeport

import org.junit.Assert.*
import org.junit.Test

class NotificationTargetTest {
    @Test fun twoServersCanHaveTheSameMessageId() {
        val a = parseNotificationTarget("chimeport", "notice", listOf("server-a", "42"))
        val b = parseNotificationTarget("chimeport", "notice", listOf("server-b", "42"))
        assertEquals("server-a" to "42", a)
        assertEquals("server-b" to "42", b)
        assertNotEquals(a, b)
    }

    @Test fun rejectsMalformedOrUnrelatedUris() {
        assertNull(parseNotificationTarget("https", "notice", listOf("a", "1")))
        assertNull(parseNotificationTarget("chimeport", "other", listOf("a", "1")))
        assertNull(parseNotificationTarget("chimeport", "notice", listOf("a")))
        assertNull(parseNotificationTarget("chimeport", "notice", listOf("a", "1", "2")))
        assertNull(parseNotificationTarget("chimeport", "notice", listOf("a", "../secret")))
        assertNull(parseNotificationTarget("chimeport", "notice", listOf("", "1")))
    }
}
