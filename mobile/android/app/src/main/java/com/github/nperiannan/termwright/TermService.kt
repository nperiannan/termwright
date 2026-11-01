package com.github.nperiannan.termwright

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.graphics.BitmapFactory
import android.os.Build
import android.os.IBinder
import androidx.core.app.NotificationCompat
import androidx.core.content.ContextCompat

/**
 * Keeps the open terminals connected while the app is in the background: Android
 * leaves a process with a foreground service (and its notification) running.
 * It runs while at least one terminal is open; the notification says which, and
 * can disconnect them all.
 */
class TermService : Service() {
    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == DISCONNECT_ALL || Terms.open.isEmpty()) {
            Terms.open.toList().forEach { Terms.close(it, update = false) }
            running = false
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
            return START_NOT_STICKY
        }
        val n = notification(this)
        if (Build.VERSION.SDK_INT >= 34) startForeground(ID, n, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        else startForeground(ID, n)
        running = true
        return START_NOT_STICKY
    }

    override fun onDestroy() {
        running = false
        super.onDestroy()
    }

    companion object {
        private const val ID = 7001
        private const val CHANNEL = "terminals"
        private const val DISCONNECT_ALL = "com.github.nperiannan.termwright.DISCONNECT_ALL"
        private var running = false

        /** After a terminal was opened or closed: start, update or stop the service. */
        fun update(ctx: Context) {
            val app = ctx.applicationContext
            when {
                Terms.open.isEmpty() -> if (running) app.stopService(Intent(app, TermService::class.java))
                // already in the foreground: only the text changes (starting it again from the background is not allowed)
                running -> app.getSystemService(NotificationManager::class.java).notify(ID, notification(app))
                else -> runCatching { ContextCompat.startForegroundService(app, Intent(app, TermService::class.java)) }
            }
        }

        private fun notification(ctx: Context): Notification {
            val nm = ctx.getSystemService(NotificationManager::class.java)
            if (nm.getNotificationChannel(CHANNEL) == null) {
                nm.createNotificationChannel(NotificationChannel(CHANNEL, "Open terminals", NotificationManager.IMPORTANCE_LOW)
                    .apply { description = "Shown while SSH or Telnet sessions are open, so that they stay connected" })
            }
            val list = Terms.open.toList()
            val connected = list.count { it.state != "closed" }
            val title = if (list.size == 1) "1 session open" else "${list.size} sessions open"
            val text = list.joinToString(", ") { it.sess.name + if (it.state == "closed") " (disconnected)" else "" }
            val open = PendingIntent.getActivity(ctx, 0, Intent(ctx, MainActivity::class.java)
                .addFlags(Intent.FLAG_ACTIVITY_SINGLE_TOP), PendingIntent.FLAG_IMMUTABLE)
            val stop = PendingIntent.getService(ctx, 1, Intent(ctx, TermService::class.java).setAction(DISCONNECT_ALL), PendingIntent.FLAG_IMMUTABLE)
            return NotificationCompat.Builder(ctx, CHANNEL)
                .setSmallIcon(R.drawable.ic_stat_tw)
                .setLargeIcon(BitmapFactory.decodeResource(ctx.resources, R.drawable.tw_logo))
                .setContentTitle("Termwright · $title")
                .setContentText(text)
                .setSubText(if (connected < list.size) "$connected connected" else null)
                .setOngoing(true)
                .setOnlyAlertOnce(true)
                .setContentIntent(open)
                .addAction(0, if (list.size == 1) "Disconnect" else "Disconnect all", stop)
                .build()
        }
    }
}
