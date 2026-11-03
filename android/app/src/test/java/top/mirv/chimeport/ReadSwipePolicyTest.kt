package top.mirv.chimeport

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class ReadSwipePolicyTest {
    @Test fun repeatRoundTripsOnSameNotification() {
        var read = false
        repeat(4) {
            assertNull(readStateAfterSwipe(read, ReadSwipeDirection.RIGHT))
            read = readStateAfterSwipe(read, ReadSwipeDirection.LEFT)!!
            assertEquals(true, read)
            assertNull(readStateAfterSwipe(read, ReadSwipeDirection.LEFT))
            read = readStateAfterSwipe(read, ReadSwipeDirection.RIGHT)!!
            assertEquals(false, read)
        }
        assertEquals(true, readStateAfterSwipe(read, ReadSwipeDirection.LEFT))
    }

    @Test fun wrongDirectionDoesNotMutateStatus() {
        assertNull(readStateAfterSwipe(false, ReadSwipeDirection.RIGHT))
        assertNull(readStateAfterSwipe(true, ReadSwipeDirection.LEFT))
        assertEquals(false, readStateAfterSwipe(true, ReadSwipeDirection.RIGHT))
        assertEquals(true, readStateAfterSwipe(false, ReadSwipeDirection.LEFT))
    }
}
