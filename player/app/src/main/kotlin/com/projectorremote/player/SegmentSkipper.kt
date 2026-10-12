package com.projectorremote.player

import org.json.JSONObject

/** Small local position check. No network work or per-tick JSON parsing. */
class SegmentSkipper {
    data class Range(val start: Long, val end: Long)
    private var episode = ""
    private val handled = mutableSetOf<String>()
    private var intro: Range? = null
    private var outro: Range? = null
    private var introMode = "manual"
    private var outroMode = "manual"

    fun load(episodeKey: String, ranges: JSONObject?, settings: JSONObject?) {
        if (episode != episodeKey) {
            handled.clear()
            episode = episodeKey
        }
        update(ranges, settings)
    }

    fun update(ranges: JSONObject?, settings: JSONObject?) {
        intro = parse(ranges?.optJSONObject("intro"))
        outro = parse(ranges?.optJSONObject("outro"))
        introMode = settings?.optString("intro", "manual") ?: "manual"
        outroMode = settings?.optString("outro", "manual") ?: "manual"
    }

    private fun parse(value: JSONObject?): Range? {
        if (value == null) return null
        val start = value.optLong("start", -1)
        val end = value.optLong("end", -1)
        return if (start >= 0 && end > start) Range(start, end) else null
    }

    fun active(position: Long, duration: Long): String? {
        if (duration <= 0) return null
        if (introMode != "off" && contains(intro, position, duration)) return "intro"
        if (outroMode != "off" && contains(outro, position, duration)) return "outro"
        return null
    }

    private fun contains(range: Range?, position: Long, duration: Long): Boolean =
        range != null && range.end <= duration && position >= range.start && position < range.end

    fun automatic(kind: String): Boolean =
        kind !in handled && (if (kind == "intro") introMode else outroMode) == "auto"

    fun skip(kind: String, position: Long, duration: Long): Long? {
        if (active(position, duration) != kind) return null
        val range = if (kind == "intro") intro else outro
        handled.add(kind)
        return range?.end
    }

    fun manualSeek(position: Long, duration: Long) {
        // Seeking into a song is an intentional choice to watch it.
        active(position, duration)?.let { handled.add(it) }
    }

    fun hasRanges(): Boolean = (intro != null && introMode != "off") || (outro != null && outroMode != "off")
}
