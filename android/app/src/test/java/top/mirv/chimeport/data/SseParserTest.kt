package top.mirv.chimeport.data

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class SseParserTest {
    @Test fun parsesCompleteEventAndIgnoresHeartbeat() {
        val parser = SseParser()
        assertNull(parser.accept(": connected"))
        assertNull(parser.accept(""))
        assertNull(parser.accept("event: messages_available"))
        assertNull(parser.accept("data: {\"last_id\":\"42\"}"))
        assertEquals("{\"last_id\":\"42\"}", parser.accept(""))
        assertNull(parser.accept(""))
        assertNull(parser.accept("event: unrelated"))
        assertNull(parser.accept("data: value"))
        assertNull(parser.accept(""))
    }

    @Test fun waitsForBlankLine() {
        val parser = SseParser()
        parser.accept("event: messages_available")
        parser.accept("data: {\"last_id\":\"1\"}")
        assertNull(parser.accept(": keepalive"))
        assertEquals("{\"last_id\":\"1\"}", parser.accept(""))
    }
}
