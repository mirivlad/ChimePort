package top.mirv.chimeport.data

import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class ChannelCachePolicyTest {
    @Test fun resyncsWhenDeletedChannelStillHasCachedMessages() {
        assertTrue(ChannelCachePolicy.needsReplay(
            setOf("home", "home/door", "servers"), setOf("home", "servers")
        ))
    }

    @Test fun keepsCursorWhenOnlyValidChannelsExist() {
        assertFalse(ChannelCachePolicy.needsReplay(
            setOf("home", "servers"), setOf("home", "servers")
        ))
    }

    @Test fun doesNotReconcileOnEmptyChannelResponse() {
        assertFalse(ChannelCachePolicy.needsReplay(setOf("home/door"), emptySet()))
    }
}
