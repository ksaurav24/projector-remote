package com.projectorremote.home

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.LinearGradient
import android.graphics.Paint
import android.graphics.Path
import android.graphics.RadialGradient
import android.graphics.Shader
import android.view.View
import android.widget.FrameLayout
import kotlin.math.min

/** One landscape composition scaled to the actual output, independent of OEM density overrides. */
internal class CinemaStage(context: Context) : FrameLayout(context) {
    val content = FrameLayout(context)

    init {
        setBackgroundColor(Color.rgb(23, 28, 56))
        content.addView(Landscape(context), LayoutParams(1280, 720))
        addView(content)
    }

    override fun onMeasure(widthMeasureSpec: Int, heightMeasureSpec: Int) {
        setMeasuredDimension(MeasureSpec.getSize(widthMeasureSpec), MeasureSpec.getSize(heightMeasureSpec))
        content.measure(MeasureSpec.makeMeasureSpec(1280, MeasureSpec.EXACTLY), MeasureSpec.makeMeasureSpec(720, MeasureSpec.EXACTLY))
    }

    override fun onLayout(changed: Boolean, left: Int, top: Int, right: Int, bottom: Int) {
        val scale = min(width / 1280f, height / 720f)
        content.layout(0, 0, 1280, 720)
        content.pivotX = 0f
        content.pivotY = 0f
        content.scaleX = scale
        content.scaleY = scale
        content.translationX = (width - 1280 * scale) / 2f
        content.translationY = (height - 720 * scale) / 2f
    }
}

/** Offline artwork: a still moon above layered Himalayan silhouettes. No network assets or animation. */
private class Landscape(context: Context) : View(context) {
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val path = Path()
    private val sky = LinearGradient(0f, 0f, 1280f, 720f,
        intArrayOf(0xFF171C38.toInt(), 0xFF343454.toInt(), 0xFF69627A.toInt()), null, Shader.TileMode.CLAMP)
    private val glow = RadialGradient(741f, 206f, 220f,
        intArrayOf(0x306E799E, Color.TRANSPARENT), null, Shader.TileMode.CLAMP)
    private val scrim = LinearGradient(0f, 0f, 820f, 0f,
        intArrayOf(0xAA171C38.toInt(), Color.TRANSPARENT), null, Shader.TileMode.CLAMP)
    private val stars = listOf(540f to 85f, 660f to 130f, 803f to 78f, 456f to 156f, 1170f to 154f)
    private val farRidge = floatArrayOf(0f,540f, 210f,460f, 345f,508f, 530f,404f, 640f,445f, 777f,330f, 940f,452f, 1088f,396f, 1280f,480f)
    private val middleRidge = floatArrayOf(0f,601f, 180f,537f, 330f,573f, 520f,500f, 650f,555f, 840f,440f, 1000f,553f, 1150f,493f, 1280f,537f)
    private val nearRidge = floatArrayOf(0f,665f, 220f,614f, 390f,649f, 655f,580f, 800f,620f, 1040f,564f, 1280f,633f)

    override fun onDraw(canvas: Canvas) {
        paint.shader = sky
        canvas.drawRect(0f, 0f, 1280f, 720f, paint)
        paint.shader = glow
        canvas.drawCircle(741f, 206f, 220f, paint)
        paint.shader = null
        paint.color = 0xFFE7DECA.toInt()
        canvas.drawCircle(741f, 206f, 37f, paint)
        // Few stars, intentionally away from the greeting and QR code.
        paint.color = 0x88E7DECA.toInt()
        for ((x, y) in stars) {
            canvas.drawCircle(x, y, 1.5f, paint)
        }
        mountain(canvas, 0xFF4D4C6B.toInt(), farRidge)
        mountain(canvas, 0xFF303B58.toInt(), middleRidge)
        mountain(canvas, 0xFF202C46.toInt(), nearRidge)
        // Darken the reading area without putting the greeting inside a card.
        paint.shader = scrim
        canvas.drawRect(0f, 0f, 820f, 720f, paint)
        paint.shader = null
    }

    private fun mountain(canvas: Canvas, color: Int, points: FloatArray) {
        path.reset()
        path.moveTo(points[0], points[1])
        for (i in 2 until points.size step 2) path.lineTo(points[i], points[i + 1])
        path.lineTo(1280f, 720f)
        path.lineTo(0f, 720f)
        path.close()
        paint.color = color
        canvas.drawPath(path, paint)
    }
}
