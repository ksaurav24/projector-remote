package com.projectorremote.home

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.ComponentName
import android.content.Intent
import android.content.res.ColorStateList
import android.graphics.Bitmap
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.graphics.drawable.StateListDrawable
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.provider.Settings
import android.util.TypedValue
import android.view.Gravity
import android.view.View
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.view.WindowManager
import android.widget.Button
import android.widget.FrameLayout
import android.widget.ImageView
import android.widget.TextView
import android.widget.Toast
import com.google.zxing.BarcodeFormat
import com.google.zxing.EncodeHintType
import com.google.zxing.qrcode.QRCodeWriter
import com.google.zxing.qrcode.decoder.ErrorCorrectionLevel
import java.net.HttpURLConnection
import java.net.Inet4Address
import java.net.NetworkInterface
import java.net.URL
import java.text.SimpleDateFormat
import java.util.Calendar
import java.util.Date
import java.util.Locale
import java.util.concurrent.Executors
import java.util.concurrent.Future

/** Standalone home app; never owns playback or changes the OEM launcher. */
class HomeActivity : Activity() {
    private val main = Handler(Looper.getMainLooper())
    private val worker = Executors.newSingleThreadExecutor()
    private var probe: Future<*>? = null
    private var active = false
    private var generation = 0
    private var shownUrl: String? = null
    private lateinit var stage: FrameLayout
    private lateinit var greeting: TextView
    private lateinit var clock: TextView
    private lateinit var date: TextView
    private lateinit var status: TextView
    private lateinit var address: TextView
    private lateinit var instruction: TextView
    private lateinit var networkHint: TextView
    private lateinit var qr: ImageView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        window.setDecorFitsSystemWindows(false)
        val root = CinemaStage(this)
        stage = root.content
        setContentView(root)
        window.insetsController?.apply {
            hide(WindowInsets.Type.systemBars())
            systemBarsBehavior = WindowInsetsController.BEHAVIOR_SHOW_TRANSIENT_BARS_BY_SWIPE
        }
        place(text(getString(R.string.app_name), 23f, MUTED), 78, 58, 500, 40)
        clock = text("", 38f, IVORY).apply { gravity = Gravity.END }
        place(clock, 900, 52, 300, 52)
        date = text("", 18f, MUTED).apply { gravity = Gravity.END }
        place(date, 850, 108, 350, 30)
        greeting = text("", 78f, IVORY).apply {
            typeface = Typeface.create("serif", Typeface.NORMAL)
            setLineSpacing(0f, 0.96f)
        }
        place(greeting, 74, 206, 730, 200)
        place(text(getString(R.string.welcome_line), 27f, MUTED), 78, 416, 700, 45)
        status = text(getString(R.string.starting), 19f, MUTED).apply {
            accessibilityLiveRegion = View.ACCESSIBILITY_LIVE_REGION_POLITE
        }
        place(status, 78, 482, 720, 35)

        // The white surround is the QR's quiet zone. No decorative marks cross it.
        val panel = View(this).apply { background = shape(0xC919203C.toInt(), 28f) }
        place(panel, 850, 180, 350, 426)
        qr = ImageView(this).apply {
            contentDescription = getString(R.string.qr_description)
            scaleType = ImageView.ScaleType.FIT_CENTER
            background = shape(Color.WHITE, 12f)
            setPadding(8, 8, 8, 8)
            visibility = View.INVISIBLE
        }
        place(qr, 901, 210, 248, 248)
        instruction = text(getString(R.string.scan_instruction), 22f, IVORY).apply {
            gravity = Gravity.CENTER
        }
        place(instruction, 864, 480, 322, 56)
        address = text("", 18f, MUTED).apply { gravity = Gravity.CENTER }
        place(address, 862, 540, 326, 30)
        networkHint = text(getString(R.string.same_wifi), 17f, MUTED).apply {
            gravity = Gravity.CENTER
        }
        place(networkHint, 820, 621, 410, 45)

        val original = button(R.string.original_home) { openOriginalHome() }
        val network = button(R.string.network_settings) { openNetworkSettings() }
        original.id = View.generateViewId()
        network.id = View.generateViewId()
        original.nextFocusRightId = network.id
        original.nextFocusLeftId = network.id
        network.nextFocusLeftId = original.id
        network.nextFocusRightId = original.id
        place(original, 78, 554, 300, 65)
        place(network, 400, 554, 250, 65)
        place(text(getString(R.string.home_hint), 18f, MUTED), 78, 637, 680, 35)
        original.requestFocus()
    }

    override fun onStart() {
        super.onStart()
        active = true
        generation++
        main.post(tick)
    }

    override fun onStop() {
        active = false
        generation++
        main.removeCallbacks(tick)
        probe?.cancel(true)
        super.onStop()
    }

    override fun onDestroy() {
        worker.shutdownNow()
        super.onDestroy()
    }

    private val tick = object : Runnable {
        override fun run() {
            if (!active) return
            updateTime()
            if (probe?.isDone != false) refreshRemote()
            main.postDelayed(this, 5_000)
        }
    }

    private fun updateTime() {
        val now = Date()
        clock.text = android.text.format.DateFormat.getTimeFormat(this).format(now)
        date.text = SimpleDateFormat("EEEE, d MMMM", Locale.getDefault()).format(now)
        greeting.setText(when (Calendar.getInstance().get(Calendar.HOUR_OF_DAY)) {
            in 5..11 -> R.string.morning
            in 12..16 -> R.string.afternoon
            else -> R.string.evening
        })
    }

    private fun refreshRemote() {
        val run = generation
        val previousUrl = shownUrl
        probe = worker.submit {
            val remote = runCatching {
                val interfaces = NetworkInterface.getNetworkInterfaces()?.toList().orEmpty()
                    .filter { it.isUp && !it.isLoopback }
                    .map { it.name to it.inetAddresses.toList().filterIsInstance<Inet4Address>()
                        .mapNotNull { addr -> addr.hostAddress } }
                RemoteAddress.choose(interfaces)
            }.getOrNull()
            // Probe loopback: Android's app UID routing and the phone's LAN path differ.
            val ready = remote != null && runCatching {
                val connection = URL("http://127.0.0.1:8080/healthz").openConnection() as HttpURLConnection
                try {
                    connection.connectTimeout = 1_500
                    connection.readTimeout = 1_500
                    connection.responseCode == 200
                } finally {
                    connection.disconnect()
                }
            }.getOrDefault(false)
            val bitmap = if (remote != null && remote.url != previousUrl) makeQr(remote.url) else null
            main.post {
                if (!active || generation != run) return@post
                if (remote == null) {
                    shownUrl = null
                    qr.visibility = View.INVISIBLE
                    address.text = ""
                    instruction.setText(R.string.waiting_network)
                    networkHint.setText(R.string.waiting_help)
                    status.setText(R.string.waiting_network)
                } else {
                    if (bitmap != null) qr.setImageBitmap(bitmap)
                    shownUrl = remote.url
                    qr.visibility = View.VISIBLE
                    address.text = remote.url.removePrefix("http://")
                    instruction.setText(R.string.scan_instruction)
                    networkHint.setText(if (remote.tailnet) R.string.tailnet_hint else R.string.same_wifi)
                    status.setText(if (ready) R.string.ready else R.string.waiting_server)
                }
            }
        }
    }

    private fun makeQr(url: String): Bitmap {
        val matrix = QRCodeWriter().encode(url, BarcodeFormat.QR_CODE, 464, 464,
            mapOf(EncodeHintType.MARGIN to 4, EncodeHintType.ERROR_CORRECTION to ErrorCorrectionLevel.M))
        val pixels = IntArray(matrix.width * matrix.height) { index ->
            if (matrix[index % matrix.width, index / matrix.width]) Color.BLACK else Color.WHITE
        }
        return Bitmap.createBitmap(pixels, matrix.width, matrix.height, Bitmap.Config.ARGB_8888)
    }

    private fun openOriginalHome() {
        launch(Intent(Intent.ACTION_MAIN).apply {
            component = ComponentName("com.jixin.launcher5", "com.jixin.launcher5.MainActivity")
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_RESET_TASK_IF_NEEDED)
        }, R.string.original_unavailable)
    }

    private fun openNetworkSettings() {
        val wifi = Intent(Settings.ACTION_WIFI_SETTINGS)
        if (!launch(wifi, null)) launch(Intent(Settings.ACTION_SETTINGS), R.string.unavailable)
    }

    private fun launch(intent: Intent, error: Int?): Boolean = try {
        startActivity(intent)
        true
    } catch (_: ActivityNotFoundException) {
        if (error != null) Toast.makeText(this, error, Toast.LENGTH_LONG).show()
        false
    } catch (_: SecurityException) {
        if (error != null) Toast.makeText(this, error, Toast.LENGTH_LONG).show()
        false
    }

    private fun text(value: String, size: Float, color: Int) = TextView(this).apply {
        text = value
        setTextColor(color)
        setTextSize(TypedValue.COMPLEX_UNIT_PX, size * resources.configuration.fontScale.coerceAtMost(1.25f))
        includeFontPadding = false
        gravity = Gravity.CENTER_VERTICAL
    }

    private fun button(label: Int, action: () -> Unit) = Button(this).apply {
        setText(label)
        isAllCaps = false
        setTextSize(TypedValue.COMPLEX_UNIT_PX, 23f)
        typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
        setPadding(16, 0, 16, 0)
        minimumHeight = 0
        minimumWidth = 0
        stateListAnimator = null
        setTextColor(ColorStateList(arrayOf(intArrayOf(android.R.attr.state_focused), intArrayOf(android.R.attr.state_pressed), intArrayOf()), intArrayOf(INK, INK, IVORY)))
        background = StateListDrawable().apply {
            addState(intArrayOf(android.R.attr.state_focused), shape(GOLD, 16f))
            addState(intArrayOf(android.R.attr.state_pressed), shape(GOLD, 16f))
            addState(intArrayOf(), shape(0xFF29314C.toInt(), 16f, 0xFF687087.toInt()))
        }
        isFocusable = true
        setOnClickListener { action() }
    }

    private fun place(view: View, x: Int, y: Int, width: Int, height: Int) {
        stage.addView(view, FrameLayout.LayoutParams(width, height).apply {
            leftMargin = x
            topMargin = y
        })
    }

    private fun shape(color: Int, radius: Float, stroke: Int? = null) = GradientDrawable().apply {
        setColor(color)
        cornerRadius = radius
        if (stroke != null) setStroke(1, stroke)
    }

    companion object {
        private val IVORY = Color.rgb(247, 240, 226)
        private val MUTED = Color.rgb(194, 200, 218)
        private val GOLD = Color.rgb(231, 207, 167)
        private val INK = Color.rgb(23, 28, 56)
    }
}
