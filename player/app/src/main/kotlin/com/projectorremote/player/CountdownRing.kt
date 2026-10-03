package com.projectorremote.player

import android.animation.ValueAnimator
import android.content.Context
import android.graphics.Canvas
import android.graphics.Paint
import android.graphics.RectF
import android.util.AttributeSet
import android.view.View
import android.view.animation.LinearInterpolator

/** An orange ring that drains as the next-episode countdown runs, with
 * the seconds left in the middle. Animates smoothly between the
 * server's once-a-second ticks. */
class CountdownRing @JvmOverloads constructor(
    context: Context,
    attrs: AttributeSet? = null,
) : View(context, attrs) {
    private val density = resources.displayMetrics.density
    private val track = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 4 * density
        color = 0x33FFFFFF
    }
    private val arc = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        style = Paint.Style.STROKE
        strokeWidth = 4 * density
        strokeCap = Paint.Cap.ROUND
        color = 0xFFF97316.toInt()
    }
    private val text = Paint(Paint.ANTI_ALIAS_FLAG).apply {
        color = 0xFFF5EFE8.toInt()
        textAlign = Paint.Align.CENTER
        textSize = 20 * density
        isFakeBoldText = true
    }
    private val bounds = RectF()
    private var fraction = 1f
    private var seconds = 0
    private var animator: ValueAnimator? = null

    /** Show [left] of [total] seconds, draining toward the next second. */
    fun set(left: Int, total: Int) {
        seconds = left
        animator?.cancel()
        val from = left.toFloat() / total
        val to = (left - 1).coerceAtLeast(0).toFloat() / total
        animator = ValueAnimator.ofFloat(from, to).apply {
            duration = 1000
            interpolator = LinearInterpolator()
            addUpdateListener {
                fraction = it.animatedValue as Float
                invalidate()
            }
            start()
        }
    }

    override fun onDetachedFromWindow() {
        animator?.cancel()
        super.onDetachedFromWindow()
    }

    override fun onDraw(canvas: Canvas) {
        val inset = arc.strokeWidth
        bounds.set(inset, inset, width - inset, height - inset)
        canvas.drawOval(bounds, track)
        canvas.drawArc(bounds, -90f, 360f * fraction, false, arc)
        val y = height / 2f - (text.descent() + text.ascent()) / 2
        canvas.drawText(seconds.toString(), width / 2f, y, text)
    }
}
