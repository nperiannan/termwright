package com.github.nperiannan.termwright

import android.content.Context
import android.text.format.DateUtils
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.glance.GlanceId
import androidx.glance.GlanceModifier
import androidx.glance.Image
import androidx.glance.ImageProvider
import androidx.glance.action.ActionParameters
import androidx.glance.action.actionParametersOf
import androidx.glance.action.actionStartActivity
import androidx.glance.action.clickable
import androidx.glance.appwidget.CircularProgressIndicator
import androidx.glance.appwidget.GlanceAppWidget
import androidx.glance.appwidget.GlanceAppWidgetReceiver
import androidx.glance.appwidget.action.ActionCallback
import androidx.glance.appwidget.action.actionRunCallback
import androidx.glance.appwidget.cornerRadius
import androidx.glance.appwidget.lazy.LazyColumn
import androidx.glance.appwidget.lazy.items
import androidx.glance.appwidget.provideContent
import androidx.glance.appwidget.updateAll
import androidx.glance.background
import androidx.glance.layout.Alignment
import androidx.glance.layout.Box
import androidx.glance.layout.Column
import androidx.glance.layout.Row
import androidx.glance.layout.Spacer
import androidx.glance.layout.fillMaxSize
import androidx.glance.layout.fillMaxWidth
import androidx.glance.layout.height
import androidx.glance.layout.padding
import androidx.glance.layout.size
import androidx.glance.layout.width
import androidx.glance.text.FontWeight
import androidx.glance.text.Text
import androidx.glance.text.TextStyle
import androidx.glance.unit.ColorProvider

private val BG = ColorProvider(Color(0xEB1B1C20))
private val FG = ColorProvider(Color(0xFFE4E5E9))
private val MUTED = ColorProvider(Color(0xFF9A9EA7))
private val TONAL = ColorProvider(Color(0xFF2B3A52))
private val ON_TONAL = ColorProvider(Color(0xFFCFE3FF))
private val LINE = ColorProvider(Color(0xFF2A2C31))

val PathKey = ActionParameters.Key<String>("path")

/** Home screen widget: the devices picked in the app, whether they answer, and a Wake button. */
class TwWidget : GlanceAppWidget() {
    override suspend fun provideGlance(context: Context, id: GlanceId) {
        provideContent {
            // read on every update (Wakes.refreshWidget), so the widget shows what the workers last saw
            val store = Store(context)
            val all = store.sessions()
            val rows = store.widget.mapNotNull { p -> all.firstOrNull { it.path == p } }.map { it to store.status(it.path) }
            Content(rows, store.checkedAt)
        }
    }
}

@Composable
private fun Content(rows: List<Pair<Sess, Status?>>, checkedAt: Long) {
    Column(GlanceModifier.fillMaxSize().background(BG).cornerRadius(24.dp).padding(horizontal = 12.dp, vertical = 10.dp)) {
        Row(GlanceModifier.fillMaxWidth().padding(bottom = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            Image(ImageProvider(R.drawable.tw_logo), "Termwright", GlanceModifier.size(22.dp).clickable(actionStartActivity<MainActivity>()))
            Spacer(GlanceModifier.width(8.dp))
            Text("Termwright", GlanceModifier.defaultWeight().clickable(actionStartActivity<MainActivity>()),
                style = TextStyle(color = FG, fontSize = 14.sp, fontWeight = FontWeight.Medium))
            val ago = if (checkedAt == 0L) "check" else "checked " + DateUtils.getRelativeTimeSpanString(checkedAt, System.currentTimeMillis(), DateUtils.MINUTE_IN_MILLIS)
            Text("$ago  ⟳", GlanceModifier.padding(4.dp).clickable(actionRunCallback<RefreshAction>()), style = TextStyle(color = MUTED, fontSize = 11.sp))
        }
        if (rows.isEmpty()) {
            Text("Open Termwright, tap a device and turn on Show on the home screen widget.",
                GlanceModifier.padding(top = 8.dp).clickable(actionStartActivity<MainActivity>()), style = TextStyle(color = MUTED, fontSize = 12.sp))
            return@Column
        }
        LazyColumn {
            items(rows, itemId = { it.first.path.hashCode().toLong() }) { (s, st) -> DeviceRow(s, st) }
        }
    }
}

@Composable
private fun DeviceRow(s: Sess, st: Status?) {
    Column {
        Box(GlanceModifier.fillMaxWidth().height(1.dp).background(LINE)) {}
        Row(GlanceModifier.fillMaxWidth().padding(vertical = 6.dp), verticalAlignment = Alignment.CenterVertically) {
            val state = st?.state ?: ""
            val dot = when (state) {
                "up" -> Color(0xFF22C55E)
                "waking" -> Color(0xFFF59E0B)
                else -> Color(0xFF555860)
            }
            Box(GlanceModifier.size(8.dp).cornerRadius(4.dp).background(dot)) {}
            Spacer(GlanceModifier.width(10.dp))
            Column(GlanceModifier.defaultWeight().clickable(actionStartActivity<MainActivity>())) {
                Text(s.name, style = TextStyle(color = FG, fontSize = 13.sp, fontWeight = FontWeight.Medium), maxLines = 1)
                val note = when (state) {
                    "up" -> "up"
                    "waking" -> "waking…"
                    "down" -> st?.note?.ifEmpty { null } ?: if (s.canWake) "sleeping" else "not answering"
                    else -> "not checked yet"
                }
                val via = if (s.jump.isNotBlank() && state != "up") " · via " + s.jump.substringAfterLast('/') else ""
                Text(note + via, style = TextStyle(color = MUTED, fontSize = 11.sp), maxLines = 1)
            }
            when {
                state == "waking" -> CircularProgressIndicator(GlanceModifier.size(20.dp))
                state == "up" -> Text("Up", GlanceModifier.padding(horizontal = 12.dp, vertical = 6.dp), style = TextStyle(color = MUTED, fontSize = 12.sp))
                s.canWake -> Box(
                    GlanceModifier.background(TONAL).cornerRadius(16.dp).padding(horizontal = 14.dp, vertical = 6.dp)
                        .clickable(actionRunCallback<WakeAction>(actionParametersOf(PathKey to s.path))),
                ) { Text("Wake", style = TextStyle(color = ON_TONAL, fontSize = 12.sp, fontWeight = FontWeight.Medium)) }
            }
        }
    }
}

class WakeAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        val path = parameters[PathKey] ?: return
        Store(context).setStatus(path, Status("waking"))
        TwWidget().updateAll(context)
        WakeWorker.start(context, path, scheduled = false)
    }
}

class RefreshAction : ActionCallback {
    override suspend fun onAction(context: Context, glanceId: GlanceId, parameters: ActionParameters) {
        StatusWorker.now(context)
    }
}

class TwWidgetReceiver : GlanceAppWidgetReceiver() {
    override val glanceAppWidget: GlanceAppWidget = TwWidget()

    override fun onEnabled(context: Context) {
        super.onEnabled(context)
        StatusWorker.every30(context)
        StatusWorker.now(context)
    }

    override fun onDisabled(context: Context) {
        super.onDisabled(context)
        StatusWorker.stop(context)
    }
}
