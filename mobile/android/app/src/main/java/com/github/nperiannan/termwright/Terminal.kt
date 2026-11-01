package com.github.nperiannan.termwright

import android.annotation.SuppressLint
import android.content.ClipData
import android.content.ClipboardManager
import android.content.Context
import android.os.Handler
import android.os.Looper
import android.util.Base64
import android.view.ViewGroup
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.activity.compose.BackHandler
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.horizontalScroll
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.MoreVert
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.platform.LocalView
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.compose.ui.viewinterop.AndroidView
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import twcore.Screen
import twcore.Twcore
import java.io.ByteArrayOutputStream
import java.util.concurrent.Executors

/** An open terminal: the connection (Go) and its screen (xterm.js in a WebView, kept while the session is open). */
class TermSession(val sess: Sess, ctx: Context) {
    var term: twcore.Terminal? = null
    var state by mutableStateOf("connecting") // connecting, connected, closed
    var reason by mutableStateOf("")
    var since by mutableLongStateOf(System.currentTimeMillis())
    var ctrl by mutableStateOf(false)
    var alt by mutableStateOf(false)
    private var cols = 80
    private var rows = 24
    private val main = Handler(Looper.getMainLooper())
    private val writer = Executors.newSingleThreadExecutor() // what is typed goes out in order, off the main thread
    private val pending = ByteArrayOutputStream()
    private var ready = false
    private var scheduled = false
    private val fontSize = Store(ctx).fontSize

    @SuppressLint("SetJavaScriptEnabled")
    val web: WebView = WebView(ctx.applicationContext).apply {
        settings.javaScriptEnabled = true
        setBackgroundColor(0xFF0C0D10.toInt())
        addJavascriptInterface(Bridge(), "tw")
        loadUrl("file:///android_asset/term/term.html")
    }

    /** What the page calls ("tw" in term.html); on a WebView thread. */
    inner class Bridge {
        @JavascriptInterface fun input(d: String) {
            main.post { typed(d) }
        }

        @JavascriptInterface fun key(k: String) {
            main.post { send(k) }
        }
        @JavascriptInterface fun fontSize(): Int = this@TermSession.fontSize
        @JavascriptInterface fun ready() {
            synchronized(pending) { ready = true }
            main.post { flush() }
        }
        @JavascriptInterface fun resize(c: Int, r: Int) {
            cols = c
            rows = r
            writer.execute { term?.resize(c.toLong(), r.toLong()) }
        }
    }

    /** The Go side, for the connection (a Go thread). */
    val screen = object : Screen {
        override fun output(data: ByteArray?) {
            data ?: return
            synchronized(pending) {
                pending.write(data)
                if (!scheduled) {
                    scheduled = true
                    main.postDelayed({ flush() }, 12) // gather what arrives in one frame
                }
            }
        }

        override fun closed(reason: String?) {
            main.post {
                state = "closed"
                this@TermSession.reason = reason ?: ""
                term = null
                print("\r\n\u001b[2m[Disconnected${if (reason.isNullOrBlank()) "" else ": $reason"}]\u001b[0m\r\n")
            }
        }
    }

    private fun flush() {
        val b: ByteArray
        synchronized(pending) {
            scheduled = false
            if (!ready || pending.size() == 0) return
            b = pending.toByteArray()
            pending.reset()
        }
        web.evaluateJavascript("twWrite('${Base64.encodeToString(b, Base64.NO_WRAP)}')", null)
    }

    /** Shows a line from the app itself (not from the device). */
    fun print(text: String) = screen.output(text.toByteArray())

    /** From the phone keyboard: Ctrl and Alt from the bar apply to the next key. */
    private fun typed(d: String) {
        var s = d
        if (ctrl && s.length == 1) s = ctrlOf(s[0])
        if (alt) s = "\u001b" + s
        ctrl = false
        alt = false
        send(s)
    }

    fun send(s: String) {
        val t = term ?: return
        writer.execute { runCatching { t.write(s.toByteArray()) } }
    }

    fun keyFromBar(name: String) = web.evaluateJavascript("twKey('$name')", null)

    fun opened(t: twcore.Terminal) {
        term = t
        state = "connected"
        reason = ""
        since = System.currentTimeMillis()
        writer.execute { t.resize(cols.toLong(), rows.toLong()) }
    }

    fun disconnect() {
        val t = term ?: return
        writer.execute { t.close() }
    }

    fun destroy() {
        disconnect()
        writer.shutdown()
        (web.parent as? ViewGroup)?.removeView(web)
        web.destroy()
    }

    fun setFont(n: Int) = web.evaluateJavascript("twFont($n)", null)

    companion object {
        /** Ctrl + a key: Ctrl+C is 3, Ctrl+[ is Esc… */
        fun ctrlOf(c: Char): String = when (c) {
            in 'a'..'z' -> (c - 'a' + 1).toChar().toString()
            in 'A'..'Z' -> (c - 'A' + 1).toChar().toString()
            '@', ' ', '2' -> "\u0000"
            '[', '3' -> "\u001b"
            '\\', '4' -> "\u001c"
            ']', '5' -> "\u001d"
            '^', '6' -> "\u001e"
            '_', '-', '7' -> "\u001f"
            '?', '8' -> "\u007f"
            else -> c.toString()
        }
    }
}

/** The result of connecting, or what the user has to answer first. */
sealed class ConnectOutcome {
    data class Opened(val ts: TermSession) : ConnectOutcome()
    data class Failed(val text: String) : ConnectOutcome()
    data class NeedLogin(val error: String) : ConnectOutcome()
    data class NeedJumpLogin(val jump: Sess, val error: String) : ConnectOutcome()
    /** A host key to accept: the device's own or (jump = true) the jump host's; changed = it differs from the one accepted before. */
    data class HostKey(val of: Sess, val key: String, val jump: Boolean, val changed: Boolean) : ConnectOutcome()
}

/** The open terminals (while the app runs), and connecting. */
object Terms {
    val open = mutableStateListOf<TermSession>()

    /** Logins typed without "Remember": only until the app is closed. */
    val temp = mutableMapOf<String, Login>()

    fun login(store: Store, path: String): Login {
        val saved = store.login(path)
        return temp[path]?.copy(hostKey = saved.hostKey) ?: saved
    }

    suspend fun connect(ctx: Context, store: Store, s: Sess, all: List<Sess>, existing: TermSession? = null): ConnectOutcome {
        val login = login(store, s.path)
        if (s.protocol == "SSH2" && !login.usable) return ConnectOutcome.NeedLogin("")
        val nets = Core.localNets(ctx)
        val jump = all.firstOrNull { it.path == s.jump }?.takeIf { !Core.onDeviceNetwork(s, nets) }
        val req = JSONObject().put("protocol", s.protocol).put("host", s.host).put("port", s.port)
            .put("user", login.user.ifEmpty { s.user }).put("password", login.password).put("key", login.key)
            .put("passphrase", login.passphrase).put("hostKey", login.hostKey)
        if (jump != null) {
            val jl = store.login(jump.path)
            if (!jl.usable) return ConnectOutcome.NeedJumpLogin(jump, "")
            req.put("jump", JSONObject().put("host", jump.host).put("port", jump.port).put("user", jl.user.ifEmpty { jump.user })
                .put("password", jl.password).put("key", jl.key).put("passphrase", jl.passphrase)
                .put("hostKey", jl.hostKey.ifEmpty { "SHA256:-" })) // the first time the key comes back to be accepted
        }
        val ts = existing ?: TermSession(s, ctx)
        ts.state = "connecting"
        if (existing != null) ts.print("\r\n\u001b[2m[Connecting to ${s.host}…]\u001b[0m\r\n")
        return try {
            val t = withContext(Dispatchers.IO) { Twcore.open(req.toString(), ts.screen) }
            ts.opened(t)
            if (ts !in open) open.add(ts)
            ConnectOutcome.Opened(ts)
        } catch (e: Exception) {
            if (existing == null) ts.destroy() else ts.state = "closed"
            val msg = e.message ?: e.toString()
            when {
                msg.startsWith(Twcore.JumpHostKeyChanged) && jump != null -> {
                    val saved = store.login(jump.path).hostKey
                    ConnectOutcome.HostKey(jump, msg.removePrefix(Twcore.JumpHostKeyChanged).trim(), true, saved.isNotEmpty() && saved != "SHA256:-")
                }
                msg.startsWith(Twcore.HostKeyChanged) ->
                    ConnectOutcome.HostKey(s, msg.removePrefix(Twcore.HostKeyChanged).trim(), false, login.hostKey.isNotEmpty())
                jump != null && msg.startsWith("jump host") && (msg.contains("unable to authenticate") || msg.contains("private key")) ->
                    ConnectOutcome.NeedJumpLogin(jump, msg)
                msg.contains("unable to authenticate") || msg.contains("private key") -> ConnectOutcome.NeedLogin(msg)
                else -> ConnectOutcome.Failed(msg)
            }
        }
    }

    fun close(ts: TermSession) {
        open.remove(ts)
        ts.destroy()
    }
}

/** The terminal screen: the device's output, the keys a phone keyboard lacks, and the open sessions. */
@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun TerminalScreen(ts: TermSession, onBack: () -> Unit, onShow: (TermSession) -> Unit, onReconnect: (TermSession) -> Unit) {
    val ctx = LocalContext.current
    val store = remember { Store(ctx) }
    var menu by remember { mutableStateOf(false) }
    var switcher by remember { mutableStateOf(false) }
    BackHandler(onBack = onBack)
    val view = LocalView.current
    DisposableEffect(Unit) { // a terminal stays on while it is shown
        view.keepScreenOn = true
        onDispose { view.keepScreenOn = false }
    }
    Column(Modifier.fillMaxSize().background(Color(0xFF0C0D10)).systemBarsPadding().imePadding()) {
        Row(Modifier.fillMaxWidth().background(Color(0xFF17181C)).padding(horizontal = 4.dp, vertical = 2.dp), verticalAlignment = Alignment.CenterVertically) {
            IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Sessions") }
            Column(Modifier.weight(1f).clickable { switcher = true }) {
                Text(ts.sess.name, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
                val proto = if (ts.sess.protocol == "SSH2") "SSH" else ts.sess.protocol
                val st = when (ts.state) { "connecting" -> "connecting…"; "closed" -> "disconnected"; else -> "" }
                Text(listOf(proto, st, "${Terms.open.size} open ⌄").filter { it.isNotEmpty() }.joinToString(" · "),
                    fontSize = 11.sp, fontFamily = FontFamily.Monospace, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            Box {
                IconButton(onClick = { menu = true }) { Icon(Icons.Default.MoreVert, "Menu") }
                DropdownMenu(expanded = menu, onDismissRequest = { menu = false }) {
                    DropdownMenuItem(text = { Text("Copy") }, onClick = {
                        menu = false
                        ts.web.evaluateJavascript("twSelection()") { r ->
                            val text = runCatching { JSONArray("[$r]").getString(0) }.getOrDefault("")
                            if (text.isNotEmpty()) ctx.getSystemService(ClipboardManager::class.java)
                                .setPrimaryClip(ClipData.newPlainText("Termwright", text))
                        }
                    })
                    DropdownMenuItem(text = { Text("Paste") }, onClick = {
                        menu = false
                        val text = ctx.getSystemService(ClipboardManager::class.java).primaryClip
                            ?.takeIf { it.itemCount > 0 }?.getItemAt(0)?.coerceToText(ctx)?.toString() ?: ""
                        if (text.isNotEmpty()) ts.web.evaluateJavascript("twPaste(${JSONObject.quote(text)})", null)
                    })
                    DropdownMenuItem(text = { Text("Select all text") }, onClick = { menu = false; ts.web.evaluateJavascript("twSelectAll()", null) })
                    HorizontalDivider()
                    DropdownMenuItem(text = { Text("Text size  A−") }, onClick = {
                        store.fontSize -= 1
                        Terms.open.forEach { it.setFont(store.fontSize) }
                    })
                    DropdownMenuItem(text = { Text("Text size  A+") }, onClick = {
                        store.fontSize += 1
                        Terms.open.forEach { it.setFont(store.fontSize) }
                    })
                    DropdownMenuItem(text = { Text("Send Break") }, enabled = ts.state == "connected", onClick = {
                        menu = false
                        ts.term?.let { t -> Thread { runCatching { t.sendBreak() } }.start() }
                    })
                    HorizontalDivider()
                    DropdownMenuItem(text = { Text("Reconnect") }, enabled = ts.state == "closed", onClick = { menu = false; onReconnect(ts) })
                    DropdownMenuItem(text = { Text("Disconnect", color = MaterialTheme.colorScheme.error) }, onClick = {
                        menu = false
                        Terms.close(ts)
                        onBack()
                    })
                }
            }
        }
        AndroidView(
            factory = { ts.web.also { (it.parent as? ViewGroup)?.removeView(it) } },
            modifier = Modifier.weight(1f).fillMaxWidth(),
        )
        if (ts.state == "closed") {
            Row(Modifier.fillMaxWidth().background(Color(0xFF2A1F12)).padding(horizontal = 12.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
                Text("Disconnected" + if (ts.reason.isNotBlank()) ": ${ts.reason}" else "", Modifier.weight(1f), fontSize = 12.sp,
                    maxLines = 2, overflow = TextOverflow.Ellipsis, color = Color(0xFFFCD9A8))
                TextButton(onClick = { onReconnect(ts) }) { Text("Reconnect") }
            }
        }
        KeyBar(ts)
    }
    if (switcher) {
        ModalBottomSheet(onDismissRequest = { switcher = false }) {
            Column(Modifier.padding(horizontal = 20.dp).padding(bottom = 28.dp)) {
                Text("Open sessions", fontSize = 18.sp, fontWeight = FontWeight.SemiBold)
                Spacer(Modifier.height(8.dp))
                Terms.open.toList().forEach { o ->
                    Row(Modifier.fillMaxWidth().clickable {
                        switcher = false
                        if (o.state == "closed") onReconnect(o)
                        onShow(o)
                    }.padding(vertical = 8.dp), verticalAlignment = Alignment.CenterVertically) {
                        Box(Modifier.size(8.dp).clip(CircleShape).background(if (o.state == "connected") Color(0xFF22C55E) else Color(0xFFF59E0B)))
                        Spacer(Modifier.width(12.dp))
                        Column(Modifier.weight(1f)) {
                            Text(o.sess.name, fontWeight = if (o === ts) FontWeight.SemiBold else FontWeight.Normal)
                            val mins = (System.currentTimeMillis() - o.since) / 60_000
                            Text((if (o.sess.protocol == "SSH2") "SSH" else o.sess.protocol) + " · " +
                                (if (o.state == "closed") "disconnected - tap to reconnect" else "connected $mins min"),
                                fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        TextButton(onClick = {
                            Terms.close(o)
                            if (o === ts) { switcher = false; Terms.open.firstOrNull()?.let(onShow) ?: onBack() }
                        }) { Text("✕") }
                    }
                }
            }
        }
    }
}

/** Esc, Tab, Ctrl, Alt, the cursor keys and characters that are hard to reach on a phone keyboard. */
@Composable
private fun KeyBar(ts: TermSession) {
    Row(Modifier.fillMaxWidth().background(Color(0xFF17181C)).horizontalScroll(rememberScrollState()).padding(4.dp)) {
        KeyCap("Esc") { ts.keyFromBar("esc") }
        KeyCap("Tab") { ts.keyFromBar("tab") }
        KeyCap("Ctrl", ts.ctrl) { ts.ctrl = !ts.ctrl }
        KeyCap("Alt", ts.alt) { ts.alt = !ts.alt }
        KeyCap("↑") { ts.keyFromBar("up") }
        KeyCap("↓") { ts.keyFromBar("down") }
        KeyCap("←") { ts.keyFromBar("left") }
        KeyCap("→") { ts.keyFromBar("right") }
        for (c in listOf("|", "?", "/", "-", "~")) KeyCap(c) { ts.send(c) }
        KeyCap("Home") { ts.keyFromBar("home") }
        KeyCap("End") { ts.keyFromBar("end") }
        KeyCap("PgUp") { ts.keyFromBar("pgup") }
        KeyCap("PgDn") { ts.keyFromBar("pgdn") }
    }
}

@Composable
private fun KeyCap(label: String, on: Boolean = false, act: () -> Unit) {
    Box(
        Modifier.padding(horizontal = 2.dp).defaultMinSize(minWidth = 40.dp).clip(RoundedCornerShape(6.dp))
            .background(if (on) Color(0xFF5AA9FF) else Color(0xFF24262B)).clickable(onClick = act).padding(horizontal = 8.dp, vertical = 9.dp),
        contentAlignment = Alignment.Center,
    ) { Text(label, fontSize = 13.sp, color = if (on) Color(0xFF04213F) else Color(0xFFDFE2E7), fontWeight = if (on) FontWeight.SemiBold else FontWeight.Normal) }
}
