package com.projectorremote.player

import android.os.Handler
import android.os.Looper
import android.util.Log
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.WebSocket
import okhttp3.WebSocketListener
import org.json.JSONObject
import java.util.concurrent.TimeUnit

/** The link to the Go server's /player endpoint on this same box. It
 * reconnects with backoff for as long as it's open, and delivers every
 * callback on the main thread. */
class ControlSocket(
    private val url: String,
    private val onOpen: () -> Unit,
    private val onMessage: (JSONObject) -> Unit,
) {
    private val main = Handler(Looper.getMainLooper())
    private val client = OkHttpClient.Builder()
        .pingInterval(15, TimeUnit.SECONDS)
        .build()
    private var ws: WebSocket? = null
    private var attempt = 0
    private var closed = false

    fun connect() {
        if (closed) return
        ws = client.newWebSocket(Request.Builder().url(url).build(), object : WebSocketListener() {
            override fun onOpen(webSocket: WebSocket, response: Response) {
                main.post {
                    attempt = 0
                    onOpen()
                }
            }

            override fun onMessage(webSocket: WebSocket, text: String) {
                val msg = try {
                    JSONObject(text)
                } catch (e: Exception) {
                    Log.w(TAG, "bad message: $text")
                    return
                }
                main.post { onMessage(msg) }
            }

            override fun onClosed(webSocket: WebSocket, code: Int, reason: String) = retry(webSocket)

            override fun onFailure(webSocket: WebSocket, t: Throwable, response: Response?) {
                Log.w(TAG, "socket failed: ${t.message}")
                retry(webSocket)
            }
        })
    }

    private fun retry(from: WebSocket) {
        main.post {
            if (closed || from != ws) return@post
            ws = null
            val delay = minOf(500L shl minOf(attempt, 4), 8000L)
            attempt++
            main.postDelayed({ connect() }, delay)
        }
    }

    fun send(msg: JSONObject) {
        ws?.send(msg.toString())
    }

    fun close() {
        closed = true
        main.removeCallbacksAndMessages(null)
        ws?.close(1000, null)
        client.dispatcher.executorService.shutdown()
    }

    companion object {
        private const val TAG = "ControlSocket"
    }
}
