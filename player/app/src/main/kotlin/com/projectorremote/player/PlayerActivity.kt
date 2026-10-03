package com.projectorremote.player

import android.app.Activity
import android.graphics.Color
import android.os.Build
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.view.KeyEvent
import android.view.View
import android.view.WindowInsets
import android.widget.TextView
import androidx.annotation.OptIn
import androidx.media3.common.C
import androidx.media3.common.MediaItem
import androidx.media3.common.PlaybackException
import androidx.media3.common.Player
import androidx.media3.common.TrackSelectionOverride
import androidx.media3.common.TrackSelectionParameters
import androidx.media3.common.util.UnstableApi
import androidx.media3.datasource.DefaultHttpDataSource
import androidx.media3.datasource.HttpDataSource
import androidx.media3.exoplayer.ExoPlayer
import androidx.media3.exoplayer.hls.HlsMediaSource
import androidx.media3.ui.CaptionStyleCompat
import androidx.media3.ui.PlayerView
import androidx.media3.ui.SubtitleView
import org.json.JSONArray
import org.json.JSONObject

/**
 * Plays whatever the Go server on this box tells it to. All session
 * logic (what to play, resume points, next episode, preferences) lives
 * in the server; this activity only loads streams, reports what the
 * player is doing, and applies commands. See internal/playback.
 */
@OptIn(UnstableApi::class)
class PlayerActivity : Activity() {
    private lateinit var player: ExoPlayer
    private lateinit var playerView: PlayerView
    private lateinit var socket: ControlSocket
    private lateinit var titleCard: View
    private lateinit var countdownCard: View
    private lateinit var countdownRing: CountdownRing
    private lateinit var errorCard: View

    private val main = Handler(Looper.getMainLooper())
    private val http = DefaultHttpDataSource.Factory()
        .setUserAgent(USER_AGENT)
        .setAllowCrossProtocolRedirects(true)

    /** What's loaded, echoed in every report so the server can drop
     * stale ones after it switches to a new stream. */
    private var contentId = ""
    private var failed = false
    /** Set when the server ended the session, so closing doesn't report
     * back that the viewer closed it. */
    private var stoppedByServer = false
    private var countdownTotal = 0
    private var pausedByStop = false
    /** The current stream has been reported as ended. */
    private var ended = false

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_player)
        hideSystemBars()

        playerView = findViewById(R.id.player_view)
        titleCard = findViewById(R.id.title_card)
        countdownCard = findViewById(R.id.countdown_card)
        countdownRing = findViewById(R.id.countdown_ring)
        errorCard = findViewById(R.id.error_card)

        player = ExoPlayer.Builder(this).build()
        player.addListener(listener)
        playerView.player = player
        playerView.subtitleView?.apply {
            // Larger than the phone-sized default: read from across a room.
            setFractionalTextSize(SubtitleView.DEFAULT_TEXT_SIZE_FRACTION * 1.2f)
            setStyle(
                CaptionStyleCompat(
                    Color.WHITE, 0x99000000.toInt(), Color.TRANSPARENT,
                    CaptionStyleCompat.EDGE_TYPE_NONE, Color.BLACK, null,
                ),
            )
        }

        socket = ControlSocket(CONTROL_URL, onOpen = ::sayHello, onMessage = ::onCommand)
        socket.connect()
        main.post(ticker)
    }

    override fun onStart() {
        super.onStart()
        // Back in front after something covered us (the screensaver, a
        // system dialog): carry on if that's what paused playback.
        if (pausedByStop) player.playWhenReady = true
        pausedByStop = false
    }

    override fun onStop() {
        super.onStop()
        // Home pressed mid-play: don't keep playing behind the launcher.
        if (!isFinishing && player.playWhenReady) {
            player.playWhenReady = false
            pausedByStop = true
        }
    }

    override fun onDestroy() {
        main.removeCallbacksAndMessages(null)
        if (!stoppedByServer) socket.send(JSONObject().put("type", "closed"))
        socket.close()
        player.release()
        super.onDestroy()
    }

    private fun hideSystemBars() {
        if (Build.VERSION.SDK_INT >= 30) {
            window.insetsController?.hide(WindowInsets.Type.systemBars())
        } else {
            @Suppress("DEPRECATION")
            window.decorView.systemUiVisibility = View.SYSTEM_UI_FLAG_FULLSCREEN or
                View.SYSTEM_UI_FLAG_HIDE_NAVIGATION or View.SYSTEM_UI_FLAG_IMMERSIVE_STICKY
        }
    }

    // --- Reporting to the server ---

    private fun sayHello() {
        socket.send(JSONObject().put("type", "hello").put("version", 1).put("contentId", contentId))
        sendState()
    }

    private val ticker = object : Runnable {
        override fun run() {
            if (player.isPlaying) {
                // NetMirror playlists can understate the media's length;
                // left alone, playback runs past the stated end and Media3
                // raises a stuck-player timeout a minute later. The stated
                // end is the end.
                val dur = player.duration
                if (dur != C.TIME_UNSET && dur > 0 && player.currentPosition >= dur) {
                    player.playWhenReady = false
                    reportEnded()
                }
                sendState()
            }
            main.postDelayed(this, 1000)
        }
    }

    private fun reportEnded() {
        if (ended) return
        ended = true
        socket.send(JSONObject().put("type", "ended").put("contentId", contentId))
    }

    private val listener = object : Player.Listener {
        override fun onPlaybackStateChanged(state: Int) {
            if (state == Player.STATE_ENDED) reportEnded()
        }

        override fun onEvents(p: Player, events: Player.Events) {
            if (events.containsAny(
                    Player.EVENT_PLAYBACK_STATE_CHANGED,
                    Player.EVENT_PLAY_WHEN_READY_CHANGED,
                    Player.EVENT_TRACKS_CHANGED,
                    Player.EVENT_TRACK_SELECTION_PARAMETERS_CHANGED,
                    Player.EVENT_POSITION_DISCONTINUITY,
                )
            ) sendState()
        }

        override fun onPlayerError(error: PlaybackException) {
            var http = 0
            var cause: Throwable? = error
            while (cause != null) {
                if (cause is HttpDataSource.InvalidResponseCodeException) {
                    http = cause.responseCode
                    break
                }
                cause = cause.cause
            }
            socket.send(
                JSONObject().put("type", "error").put("contentId", contentId)
                    .put("http", http).put("message", "${error.errorCodeName}: ${error.message}")
                    .put("pos", player.currentPosition),
            )
        }
    }

    private fun status(): String = if (ended) "ended" else when (player.playbackState) {
        Player.STATE_BUFFERING -> "buffering"
        Player.STATE_ENDED -> "ended"
        Player.STATE_READY -> if (player.playWhenReady) "playing" else "paused"
        else -> "loading"
    }

    private fun sendState() {
        if (contentId.isEmpty() || failed || player.playbackState == Player.STATE_IDLE) return
        val msg = JSONObject().put("type", "state").put("contentId", contentId)
            .put("status", status())
            .put("pos", player.currentPosition)
            .put("dur", player.duration.takeIf { it != C.TIME_UNSET } ?: 0)
        describeTracks(msg)
        socket.send(msg)
    }

    /** Adds "tracks" and "selected" to msg from the player's current
     * tracks, which come from the HLS manifest. */
    private fun describeTracks(msg: JSONObject) {
        val audio = JSONArray()
        val text = JSONArray()
        val video = JSONArray()
        var selAudio = ""
        var selText = "off"
        var selVideo = "auto"
        val params = player.trackSelectionParameters
        val pinnedHeight = params.maxVideoHeight.takeIf { it != Int.MAX_VALUE && it == params.minVideoHeight }
        val videoOverride = params.overrides.values.firstOrNull { it.type == C.TRACK_TYPE_VIDEO }

        player.currentTracks.groups.forEachIndexed { gi, g ->
            for (ti in 0 until g.length) {
                if (!g.isTrackSupported(ti)) continue
                val f = g.getTrackFormat(ti)
                val id = trackId(gi, ti)
                when (g.type) {
                    C.TRACK_TYPE_AUDIO -> {
                        audio.put(JSONObject().put("id", id).put("lang", f.language ?: "").put("label", trackLabel(f.label, f.language)))
                        if (g.isTrackSelected(ti)) selAudio = id
                    }
                    C.TRACK_TYPE_TEXT -> {
                        text.put(JSONObject().put("id", id).put("lang", f.language ?: "").put("label", trackLabel(f.label, f.language)))
                        if (g.isTrackSelected(ti)) selText = id
                    }
                    C.TRACK_TYPE_VIDEO -> {
                        video.put(JSONObject().put("id", id).put("height", f.height).put("bitrate", f.bitrate))
                        val overridden = videoOverride?.mediaTrackGroup == g.mediaTrackGroup && ti in videoOverride.trackIndices
                        if (overridden || (videoOverride == null && pinnedHeight != null && f.height == pinnedHeight)) selVideo = id
                    }
                }
            }
        }
        msg.put("tracks", JSONObject().put("audio", audio).put("text", text).put("video", video))
        msg.put("selected", JSONObject().put("audio", selAudio).put("text", selText).put("video", selVideo))
    }

    // --- Commands from the server ---

    private fun onCommand(msg: JSONObject) {
        when (msg.optString("type")) {
            "load" -> load(msg)
            "set_paused" -> {
                player.playWhenReady = !msg.optBoolean("paused")
                playerView.showController()
            }
            "seek" -> {
                player.seekTo(msg.optLong("to"))
                playerView.showController()
            }
            "skip" -> skip(msg.optLong("by"))
            "select_track" -> selectTrack(msg.optString("kind"), msg.optString("id"))
            "countdown" -> showCountdown(msg.optInt("seconds"), msg.optString("label"))
            "countdown_cancel" -> hideCountdown()
            "fail" -> fail(msg.optString("message"))
            "stop" -> {
                stoppedByServer = true
                finish()
            }
        }
    }

    private fun load(msg: JSONObject) {
        failed = false
        ended = false
        errorCard.visibility = View.GONE
        hideCountdown()
        contentId = msg.optString("contentId")

        val p = msg.optJSONObject("prefs")
        applyPrefs(Prefs(p?.optString("audio") ?: "", p?.optString("text") ?: "", p?.optString("quality") ?: ""))

        // Every playlist and segment request must carry the Referer.
        http.setDefaultRequestProperties(mapOf("Referer" to msg.optString("referer")))
        val source = HlsMediaSource.Factory(http).createMediaSource(MediaItem.fromUri(msg.optString("url")))
        player.setMediaSource(source, msg.optLong("startPos"))
        player.prepare()
        player.playWhenReady = true
        showTitle(msg.optString("title"), msg.optString("subtitle"))
    }

    private fun applyPrefs(prefs: Prefs) {
        val b = player.trackSelectionParameters.buildUpon()
            .clearOverrides()
            .setTrackTypeDisabled(C.TRACK_TYPE_TEXT, prefs.subtitlesOff)
            .setPreferredAudioLanguage(prefs.preferredAudio)
            .setPreferredTextLanguage(prefs.preferredText)
        val h = prefs.fixedHeight
        if (h != null) b.setMinVideoSize(0, h).setMaxVideoSize(Int.MAX_VALUE, h) else b.clearVideoSizeConstraints()
        player.trackSelectionParameters = b.build()
    }

    private fun selectTrack(kind: String, id: String) {
        val b = player.trackSelectionParameters.buildUpon()
        when (kind) {
            "audio" -> override(b, id)
            "text" -> if (id == "off") {
                b.clearOverridesOfType(C.TRACK_TYPE_TEXT).setTrackTypeDisabled(C.TRACK_TYPE_TEXT, true)
            } else {
                b.setTrackTypeDisabled(C.TRACK_TYPE_TEXT, false)
                override(b, id)
            }
            "video" -> {
                b.clearVideoSizeConstraints()
                if (id == "auto") b.clearOverridesOfType(C.TRACK_TYPE_VIDEO) else override(b, id)
            }
        }
        player.trackSelectionParameters = b.build()
    }

    private fun override(b: TrackSelectionParameters.Builder, id: String) {
        val (gi, ti) = parseTrackId(id) ?: return
        val group = player.currentTracks.groups.getOrNull(gi) ?: return
        b.setOverrideForType(TrackSelectionOverride(group.mediaTrackGroup, ti))
    }

    private fun skip(byMs: Long) {
        val dur = player.duration.takeIf { it != C.TIME_UNSET } ?: Long.MAX_VALUE
        player.seekTo((player.currentPosition + byMs).coerceIn(0, dur))
        playerView.showController()
    }

    private fun fail(message: String) {
        failed = true
        player.stop()
        hideCountdown()
        findViewById<TextView>(R.id.error_text).text = message
        errorCard.visibility = View.VISIBLE
    }

    // --- Overlays ---

    private val hideTitle = Runnable { titleCard.animate().alpha(0f).setDuration(600).start() }

    private fun showTitle(title: String, subtitle: String) {
        findViewById<TextView>(R.id.title_text).text = title
        findViewById<TextView>(R.id.subtitle_text).apply {
            text = subtitle
            visibility = if (subtitle.isEmpty()) View.GONE else View.VISIBLE
        }
        main.removeCallbacks(hideTitle)
        titleCard.animate().alpha(1f).setDuration(300).start()
        main.postDelayed(hideTitle, 4000)
    }

    private fun showCountdown(seconds: Int, label: String) {
        if (countdownCard.visibility != View.VISIBLE) {
            countdownTotal = seconds
            countdownCard.alpha = 0f
            countdownCard.visibility = View.VISIBLE
            countdownCard.animate().alpha(1f).setDuration(250).start()
        }
        findViewById<TextView>(R.id.countdown_label).text = label
        countdownRing.set(seconds, countdownTotal.coerceAtLeast(seconds))
    }

    private fun hideCountdown() {
        countdownCard.visibility = View.GONE
    }

    // --- Remote keys (the projector's IR remote, or the phone's d-pad) ---

    override fun dispatchKeyEvent(event: KeyEvent): Boolean {
        if (event.action != KeyEvent.ACTION_DOWN) return super.dispatchKeyEvent(event)
        when (event.keyCode) {
            KeyEvent.KEYCODE_BACK -> {
                if (countdownCard.visibility == View.VISIBLE) {
                    hideCountdown()
                    socket.send(JSONObject().put("type", "cancel_autoplay"))
                } else {
                    finish()
                }
            }
            KeyEvent.KEYCODE_DPAD_CENTER, KeyEvent.KEYCODE_ENTER,
            KeyEvent.KEYCODE_MEDIA_PLAY_PAUSE, KeyEvent.KEYCODE_SPACE -> {
                player.playWhenReady = !player.playWhenReady
                playerView.showController()
            }
            KeyEvent.KEYCODE_MEDIA_PLAY -> player.playWhenReady = true
            KeyEvent.KEYCODE_MEDIA_PAUSE -> player.playWhenReady = false
            KeyEvent.KEYCODE_DPAD_LEFT, KeyEvent.KEYCODE_MEDIA_REWIND -> skip(-SKIP_MS)
            KeyEvent.KEYCODE_DPAD_RIGHT, KeyEvent.KEYCODE_MEDIA_FAST_FORWARD -> skip(SKIP_MS)
            KeyEvent.KEYCODE_DPAD_UP, KeyEvent.KEYCODE_DPAD_DOWN -> playerView.showController()
            else -> return super.dispatchKeyEvent(event)
        }
        return true
    }

    companion object {
        private const val CONTROL_URL = "ws://127.0.0.1:8080/player"
        private const val SKIP_MS = 10_000L
        private const val USER_AGENT =
            "Mozilla/5.0 (X11; Linux x86_64; rv:130.0) Gecko/20100101 Firefox/130.0"
    }
}
