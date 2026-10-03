package com.projectorremote.player

import java.util.Locale

// Pure helpers kept apart from the Android/Media3 code so they can be
// unit-tested on the JVM.

/** Track ids sent to the server: "<group index>:<track index>" within
 * the player's current Tracks. Valid only until the next load. */
fun trackId(group: Int, track: Int): String = "$group:$track"

fun parseTrackId(id: String): Pair<Int, Int>? {
    val parts = id.split(":")
    if (parts.size != 2) return null
    val g = parts[0].toIntOrNull() ?: return null
    val t = parts[1].toIntOrNull() ?: return null
    return g to t
}

private val numberingRe = Regex("""^\s*(\d+\.|\[\d+])\s*""")

/** A human label for a track. NetMirror numbers its track names
 * ("1. Hindi", "[2] Hindi"); the number means nothing to a viewer. */
fun trackLabel(label: String?, lang: String?): String {
    val cleaned = label?.replace(numberingRe, "")?.trim()
    if (!cleaned.isNullOrEmpty()) return cleaned
    if (lang.isNullOrEmpty() || lang == "und") return "Unknown"
    val name = Locale.forLanguageTag(lang).getDisplayLanguage(Locale.ENGLISH)
    return name.ifEmpty { lang }
}

/** Track preferences from the server, applied on every load so choices
 * carry across episodes. Matched by language or height, never by id. */
data class Prefs(val audio: String, val text: String, val quality: String) {
    val preferredAudio: String? get() = audio.ifEmpty { null }
    val subtitlesOff: Boolean get() = text == "off"
    val preferredText: String? get() = text.takeIf { it.isNotEmpty() && it != "off" }
    val fixedHeight: Int? get() = quality.toIntOrNull()
}
