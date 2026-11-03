package top.mirv.chimeport

import org.junit.Assert.*
import org.junit.Test

class QuietPolicyTest {
    private val overnight = QuietRule(
        mode = QuietMode.SCHEDULE,
        startMinutes = 22 * 60,
        endMinutes = 7 * 60
    )
    private val off = QuietRule(mode = QuietMode.OFF)
    private val inherit = QuietRule(mode = QuietMode.INHERIT)

    @Test fun crossingMidnightIsInclusiveAtStartExclusiveAtEnd() {
        assertFalse(isWithinQuietHours(overnight, 21 * 60 + 59))
        assertTrue(isWithinQuietHours(overnight, 22 * 60))
        assertTrue(isWithinQuietHours(overnight, 23 * 60 + 59))
        assertTrue(isWithinQuietHours(overnight, 0))
        assertTrue(isWithinQuietHours(overnight, 6 * 60 + 59))
        assertFalse(isWithinQuietHours(overnight, 7 * 60))
        assertFalse(isWithinQuietHours(overnight, 12 * 60))
    }

    @Test fun sameTimeMeansAllDayAndSameDayRangeWorks() {
        val allDay = overnight.copy(startMinutes = 9 * 60, endMinutes = 9 * 60)
        assertTrue(isWithinQuietHours(allDay, 0))
        assertTrue(isWithinQuietHours(allDay, 9 * 60))
        assertTrue(isWithinQuietHours(allDay, 23 * 60 + 59))
        val daytime = overnight.copy(startMinutes = 9 * 60, endMinutes = 17 * 60)
        assertFalse(isWithinQuietHours(daytime, 8 * 60 + 59))
        assertTrue(isWithinQuietHours(daytime, 9 * 60))
        assertFalse(isWithinQuietHours(daytime, 17 * 60))
    }

    @Test fun explicitServerOffDisablesGlobalRuleButChannelCanOverride() {
        val local = overnight.copy(startMinutes = 12 * 60, endMinutes = 13 * 60)
        assertEquals(QuietSource.SERVER, resolveQuietRule(overnight, off, inherit).source)
        assertFalse(shouldQuietNotification("normal", resolveQuietRule(overnight, off, inherit), 23 * 60))
        val choice = resolveQuietRule(overnight, off, local)
        assertEquals(QuietSource.CHANNEL, choice.source)
        assertTrue(shouldQuietNotification("normal", choice, 12 * 60 + 30))
        assertFalse(shouldQuietNotification("normal", choice, 23 * 60))
        assertEquals(QuietSource.GLOBAL, resolveQuietRule(overnight, inherit, inherit).source)
    }

    @Test fun urgentExceptionIsOnlyForUrgentAndNeverChangesReadStatus() {
        val active = EffectiveQuietRule(overnight.copy(allowUrgent = true), QuietSource.GLOBAL)
        assertTrue(shouldQuietNotification("normal", active, 2 * 60))
        assertTrue(shouldQuietNotification("high", active, 2 * 60))
        assertFalse(shouldQuietNotification("urgent", active, 2 * 60))
        assertFalse(shouldQuietNotification("URGENT", active, 2 * 60))
        assertFalse(shouldQuietNotification("urgent", active, 14 * 60))
        val strict = EffectiveQuietRule(overnight, QuietSource.CHANNEL)
        assertTrue(shouldQuietNotification("urgent", strict, 2 * 60))
    }

    @Test fun defaultIsNotQuietAndBadTimesAreRejected() {
        assertFalse(isWithinQuietHours(off, 23 * 60))
        assertFalse(isWithinQuietHours(inherit, 23 * 60))
        assertThrows(IllegalArgumentException::class.java) {
            QuietRule(startMinutes = 1440)
        }
        assertEquals("00:03", formatQuietMinute(3))
        assertEquals("23:59", formatQuietMinute(1439))
    }
}
