package com.github.nperiannan.termwright

import android.Manifest
import android.app.AlarmManager
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.BitmapFactory
import android.net.Uri
import android.os.Build
import androidx.core.app.NotificationCompat
import androidx.core.app.NotificationManagerCompat
import androidx.core.content.ContextCompat
import androidx.glance.appwidget.updateAll
import androidx.work.CoroutineWorker
import androidx.work.ExistingPeriodicWorkPolicy
import androidx.work.ExistingWorkPolicy
import androidx.work.OneTimeWorkRequestBuilder
import androidx.work.PeriodicWorkRequestBuilder
import androidx.work.WorkManager
import androidx.work.WorkerParameters
import androidx.work.workDataOf
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch
import kotlinx.coroutines.withTimeoutOrNull
import java.time.DayOfWeek
import java.time.ZonedDateTime
import java.util.concurrent.TimeUnit

/** Wakes without the app open: from the widget and on a schedule. No one is there to answer a login or a key question. */
object Wakes {
    /** How long a woken device has to answer before the widget and the notification say it did not. */
    const val WAIT_MS = 180_000L

    /** Why a wake did not go out, in a few words. */
    fun why(r: WakeOutcome): String = when (r) {
        is WakeOutcome.Failed -> r.text
        is WakeOutcome.NeedLogin -> "the jump host ${r.jump.name} needs a login - open Termwright"
        is WakeOutcome.NewHostKey, is WakeOutcome.ChangedHostKey -> "the jump host's key needs your OK - open Termwright"
        is WakeOutcome.Done -> ""
    }

    suspend fun refreshWidget(ctx: Context) = try {
        TwWidget().updateAll(ctx)
    } catch (_: Exception) {
    }
}

/** Sends the magic packet (unless it was sent already) and waits for the device to answer; tells the widget, and after a scheduled wake the user. */
class WakeWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val ctx = applicationContext
        val path = inputData.getString("path") ?: return Result.success()
        val scheduled = inputData.getBoolean("scheduled", false)
        val store = Store(ctx)
        val all = store.sessions()
        val d = all.firstOrNull { it.path == path } ?: return Result.success()
        var sentAt = inputData.getLong("sentAt", 0)
        var how = inputData.getString("how") ?: ""
        if (sentAt == 0L) {
            val r = Core.wake(ctx, store, d, all)
            if (r !is WakeOutcome.Done) {
                store.setStatus(path, Status("down", Wakes.why(r)))
                Wakes.refreshWidget(ctx)
                if (scheduled) Notify.wake(ctx, d, "Not sent: ${Wakes.why(r)}")
                return Result.success()
            }
            sentAt = System.currentTimeMillis()
            how = r.text
        }
        store.setStatus(path, Status("waking"))
        Wakes.refreshWidget(ctx)
        while (System.currentTimeMillis() - sentAt < Wakes.WAIT_MS) {
            if (Core.isUp(d)) {
                store.setStatus(path, Status("up"))
                Wakes.refreshWidget(ctx)
                if (scheduled) Notify.wake(ctx, d, "$how. It answered after ${(System.currentTimeMillis() - sentAt) / 1000} s.")
                return Result.success()
            }
            delay(5_000)
        }
        store.setStatus(path, Status("down", "no answer yet"))
        Wakes.refreshWidget(ctx)
        if (scheduled) Notify.wake(ctx, d, "$how, but it did not answer within ${Wakes.WAIT_MS / 60_000} minutes.")
        return Result.success()
    }

    companion object {
        fun start(ctx: Context, path: String, scheduled: Boolean, sentAt: Long = 0, how: String = "") {
            val req = OneTimeWorkRequestBuilder<WakeWorker>()
                .setInputData(workDataOf("path" to path, "scheduled" to scheduled, "sentAt" to sentAt, "how" to how)).build()
            WorkManager.getInstance(ctx).enqueueUniqueWork("wake:$path", ExistingWorkPolicy.REPLACE, req)
        }
    }
}

/** Checks which of the widget's devices answer: every 30 minutes, and when asked. */
class StatusWorker(ctx: Context, params: WorkerParameters) : CoroutineWorker(ctx, params) {
    override suspend fun doWork(): Result {
        val ctx = applicationContext
        val store = Store(ctx)
        val all = store.sessions()
        store.widget.mapNotNull { p -> all.firstOrNull { it.path == p } }.forEach { d ->
            if (store.status(d.path)?.state == "waking") return@forEach // its WakeWorker reports
            store.setStatus(d.path, Status(if (Core.isUp(d)) "up" else "down"))
        }
        store.checkedAt = System.currentTimeMillis()
        Wakes.refreshWidget(ctx)
        return Result.success()
    }

    companion object {
        fun now(ctx: Context) = WorkManager.getInstance(ctx)
            .enqueueUniqueWork("status-now", ExistingWorkPolicy.KEEP, OneTimeWorkRequestBuilder<StatusWorker>().build())

        fun every30(ctx: Context) = WorkManager.getInstance(ctx).enqueueUniquePeriodicWork(
            "status", ExistingPeriodicWorkPolicy.KEEP, PeriodicWorkRequestBuilder<StatusWorker>(30, TimeUnit.MINUTES).build(),
        )

        fun stop(ctx: Context) = WorkManager.getInstance(ctx).cancelUniqueWork("status")
    }
}

/** Alarms for the phone's wake schedules. */
object Scheduler {
    /** The next time a schedule calls for a wake, after now. */
    fun next(sc: Sched, now: ZonedDateTime): ZonedDateTime {
        val hm = sc.at.split(":").map { it.trim().toIntOrNull() ?: 0 }
        var t = now.withHour(hm.getOrElse(0) { 7 }.coerceIn(0, 23)).withMinute(hm.getOrElse(1) { 0 }.coerceIn(0, 59)).withSecond(0).withNano(0)
        if (!t.isAfter(now)) t = t.plusDays(1)
        if (sc.mode == "weekdays") while (t.dayOfWeek == DayOfWeek.SATURDAY || t.dayOfWeek == DayOfWeek.SUNDAY) t = t.plusDays(1)
        return t
    }

    /** Whether Android lets the app wake at the exact minute (Alarms & reminders, Android 12 and later). */
    fun exact(ctx: Context): Boolean =
        Build.VERSION.SDK_INT < 31 || ctx.getSystemService(AlarmManager::class.java).canScheduleExactAlarms()

    fun armAll(ctx: Context) = Store(ctx).sessions().forEach { arm(ctx, it) }

    fun arm(ctx: Context, s: Sess) {
        val am = ctx.getSystemService(AlarmManager::class.java) ?: return
        val pi = pending(ctx, s.path)
        am.cancel(pi)
        val sc = Store(ctx).schedule(s)
        if (!sc.on || !s.canWake) return
        val at = next(sc, ZonedDateTime.now()).toInstant().toEpochMilli()
        if (Build.VERSION.SDK_INT < 31 || am.canScheduleExactAlarms()) am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pi)
        else am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, at, pi)
    }

    private fun pending(ctx: Context, path: String): PendingIntent = PendingIntent.getBroadcast(
        ctx, path.hashCode(),
        Intent(ctx, ScheduleReceiver::class.java).setAction(ScheduleReceiver.WAKE).setData(Uri.fromParts("tw", path, null)).putExtra("path", path),
        PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE,
    )
}

/** A scheduled wake is due (the packet goes out at once, while Android keeps the phone awake for the alarm); and after a restart or a clock change the alarms are set again. */
class ScheduleReceiver : BroadcastReceiver() {
    override fun onReceive(ctx: Context, intent: Intent) {
        if (intent.action != WAKE) {
            Scheduler.armAll(ctx)
            return
        }
        val path = intent.getStringExtra("path") ?: return
        val done = goAsync()
        CoroutineScope(SupervisorJob() + Dispatchers.IO).launch {
            try {
                val store = Store(ctx)
                val all = store.sessions()
                val d = all.firstOrNull { it.path == path }
                if (d != null && store.schedule(d).on) {
                    val r = withTimeoutOrNull(25_000) { Core.wake(ctx, store, d, all) }
                    when {
                        r == null -> Notify.wake(ctx, d, "Not sent: the network did not answer in time.")
                        r is WakeOutcome.Done -> WakeWorker.start(ctx, path, scheduled = true, sentAt = System.currentTimeMillis(), how = r.text)
                        else -> Notify.wake(ctx, d, "Not sent: ${Wakes.why(r)}")
                    }
                    Scheduler.arm(ctx, d) // the next one
                }
            } finally {
                done.finish()
            }
        }
    }

    companion object {
        const val WAKE = "com.github.nperiannan.termwright.SCHEDULED_WAKE"
    }
}

/** Notifications after scheduled wakes. */
object Notify {
    private const val CHANNEL = "wakes"

    fun wake(ctx: Context, d: Sess, text: String) {
        if (Build.VERSION.SDK_INT >= 33 &&
            ContextCompat.checkSelfPermission(ctx, Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        val nm = ctx.getSystemService(NotificationManager::class.java)
        if (nm.getNotificationChannel(CHANNEL) == null) {
            nm.createNotificationChannel(NotificationChannel(CHANNEL, "Scheduled wakes", NotificationManager.IMPORTANCE_DEFAULT)
                .apply { description = "Whether a scheduled wake went out and the device answered" })
        }
        val open = PendingIntent.getActivity(ctx, 0, Intent(ctx, MainActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val n = NotificationCompat.Builder(ctx, CHANNEL)
            .setSmallIcon(R.drawable.ic_stat_tw)
            .setLargeIcon(BitmapFactory.decodeResource(ctx.resources, R.drawable.tw_logo))
            .setContentTitle("Scheduled wake: ${d.name}")
            .setContentText(text)
            .setStyle(NotificationCompat.BigTextStyle().bigText(text))
            .setContentIntent(open)
            .setAutoCancel(true)
            .build()
        try {
            NotificationManagerCompat.from(ctx).notify(d.path.hashCode(), n)
        } catch (_: SecurityException) {
        }
    }
}
