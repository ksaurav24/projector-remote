package com.projectorremote.player

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class TrackLogicTest {
    @Test
    fun trackIdRoundTrips() {
        assertEquals("2:1", trackId(2, 1))
        assertEquals(2 to 1, parseTrackId("2:1"))
        assertNull(parseTrackId("off"))
        assertNull(parseTrackId("2:x"))
    }

    @Test
    fun labelDropsNetMirrorNumbering() {
        assertEquals("Hindi", trackLabel("1. Hindi", "hi"))
        assertEquals("Hindi", trackLabel("[2] Hindi", "hi"))
        assertEquals("English CC", trackLabel("English CC", "en"))
    }

    @Test
    fun labelFallsBackToLanguageName() {
        assertEquals("Tamil", trackLabel(null, "ta"))
        assertEquals("Tamil", trackLabel("  ", "ta"))
        assertEquals("Unknown", trackLabel(null, null))
        assertEquals("Unknown", trackLabel(null, "und"))
    }

    @Test
    fun prefsParse() {
        val p = Prefs(audio = "hi", text = "off", quality = "720")
        assertEquals("hi", p.preferredAudio)
        assertTrue(p.subtitlesOff)
        assertNull(p.preferredText)
        assertEquals(720, p.fixedHeight)

        val q = Prefs(audio = "", text = "en", quality = "auto")
        assertNull(q.preferredAudio)
        assertFalse(q.subtitlesOff)
        assertEquals("en", q.preferredText)
        assertNull(q.fixedHeight)
    }
}
