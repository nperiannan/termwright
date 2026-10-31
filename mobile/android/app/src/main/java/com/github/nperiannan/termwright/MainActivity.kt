package com.github.nperiannan.termwright

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.OpenableColumns
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.background
import androidx.compose.foundation.clickable
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.CircleShape
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import kotlinx.coroutines.delay
import kotlinx.coroutines.launch

class MainActivity : ComponentActivity() {
    private var shared by mutableStateOf<Uri?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        shared = sharedUri(intent)
        setContent {
            val dark = isSystemInDarkTheme()
            MaterialTheme(colorScheme = if (dark) darkColorScheme(primary = Color(0xFF5AA9FF)) else lightColorScheme(primary = Color(0xFF1F7AE0))) {
                App(shared) { shared = null }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        shared = sharedUri(intent)
    }

    private fun sharedUri(i: Intent?): Uri? = when (i?.action) {
        Intent.ACTION_VIEW -> i.data
        Intent.ACTION_SEND -> if (android.os.Build.VERSION.SDK_INT >= 33) i.getParcelableExtra(Intent.EXTRA_STREAM, Uri::class.java)
            else @Suppress("DEPRECATION") i.getParcelableExtra<Uri>(Intent.EXTRA_STREAM)
        else -> null
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun App(shared: Uri?, sharedDone: () -> Unit) {
    val ctx = LocalContext.current
    val store = remember { Store(ctx) }
    val scope = rememberCoroutineScope()
    val snack = remember { SnackbarHostState() }
    var sessions by remember { mutableStateOf(store.sessions()) }
    var favourite by remember { mutableStateOf(store.favourite) }
    var up by remember { mutableStateOf(mapOf<String, Boolean>()) }
    var busy by remember { mutableStateOf<String?>(null) }
    var open by remember { mutableStateOf<Sess?>(null) }
    var loginFor by remember { mutableStateOf<Pair<Sess, String>?>(null) } // jump host, error
    var hostKey by remember { mutableStateOf<Triple<Sess, String, Boolean>?>(null) } // jump, key, changed
    var pending by remember { mutableStateOf<Sess?>(null) } // the device to wake once the user has answered
    var query by remember { mutableStateOf("") }

    fun importFrom(uri: Uri) = scope.launch {
        try {
            val name = ctx.contentResolver.query(uri, arrayOf(OpenableColumns.DISPLAY_NAME), null, null, null)?.use {
                if (it.moveToFirst()) it.getString(0) else null
            } ?: "sessions.zip"
            val data = ctx.contentResolver.openInputStream(uri)?.use { it.readBytes() } ?: return@launch
            val list = kotlinx.coroutines.withContext(kotlinx.coroutines.Dispatchers.IO) { Core.import(ctx, name, data) }
            store.saveSessions(list)
            sessions = list
            snack.showSnackbar("Imported ${list.size} sessions from $name")
        } catch (e: Exception) {
            snack.showSnackbar("Import failed: ${e.message}")
        }
    }

    val picker = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri -> uri?.let { importFrom(it) } }
    LaunchedEffect(shared) { shared?.let { importFrom(it); sharedDone() } }

    fun wake(d: Sess, acceptKey: String = "") = scope.launch {
        busy = d.path
        when (val r = Core.wake(ctx, store, d, sessions, acceptKey)) {
            is WakeOutcome.Done -> snack.showSnackbar(r.text)
            is WakeOutcome.Failed -> snack.showSnackbar("Wake failed: ${r.text}")
            is WakeOutcome.NeedLogin -> { pending = d; loginFor = r.jump to r.error }
            is WakeOutcome.NewHostKey -> { pending = d; hostKey = Triple(r.jump, r.key, false) }
            is WakeOutcome.ChangedHostKey -> { pending = d; hostKey = Triple(r.jump, r.key, true) }
        }
        busy = null
    }

    // the dots: which devices answer, every minute while the app is open
    LaunchedEffect(sessions) {
        while (true) {
            val m = mutableMapOf<String, Boolean>()
            sessions.filter { it.host.isNotBlank() }.forEach { m[it.path] = Core.isUp(it) }
            up = m
            delay(60_000)
        }
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        TwLogo(26.dp)
                        Spacer(Modifier.width(10.dp))
                        Text("Termwright", fontWeight = FontWeight.SemiBold)
                    }
                },
                actions = { TextButton(onClick = { picker.launch(arrayOf("*/*")) }) { Text("Import") } },
            )
        },
        snackbarHost = { SnackbarHost(snack) },
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize()) {
            if (sessions.isEmpty()) {
                Empty { picker.launch(arrayOf("*/*")) }
                return@Column
            }
            OutlinedTextField(query, { query = it }, Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 8.dp),
                placeholder = { Text("Filter sessions") }, singleLine = true, shape = RoundedCornerShape(12.dp))
            val shown = sessions.filter { query.isBlank() || "${it.path} ${it.host}".contains(query, true) }
                .sortedWith(compareByDescending<Sess> { it.canWake }.thenBy { it.folder.lowercase() }.thenBy { it.name.lowercase() })
            LazyColumn(Modifier.fillMaxSize(), contentPadding = PaddingValues(bottom = 24.dp)) {
                var lastFolder: String? = null
                shown.forEach { s ->
                    if (s.folder != lastFolder) {
                        lastFolder = s.folder
                        item(key = "f:" + s.folder + s.path) {
                            Text(s.folder.ifEmpty { "Top level" }.uppercase(), Modifier.padding(start = 20.dp, top = 14.dp, bottom = 4.dp),
                                fontSize = 11.sp, letterSpacing = 1.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                    item(key = s.path) {
                        SessionRow(s, up[s.path], busy == s.path, s.path == favourite, onOpen = { open = s }, onWake = { wake(s) })
                    }
                }
            }
        }
    }

    open?.let { s ->
        Details(s, sessions, s.path == favourite, onDismiss = { open = null }, onWake = { open = null; wake(s) },
            onFavourite = { store.favourite = if (s.path == favourite) "" else s.path; favourite = store.favourite },
            onLogin = { j -> open = null; loginFor = j to "" })
    }
    loginFor?.let { (jump, err) ->
        LoginDialog(jump, store.login(jump.path), err, onDismiss = { loginFor = null; pending = null }) { l ->
            store.saveLogin(jump.path, l.copy(hostKey = store.login(jump.path).hostKey))
            loginFor = null
            pending?.let { wake(it) }
            pending = null
        }
    }
    hostKey?.let { (jump, key, changed) ->
        AlertDialog(
            onDismissRequest = { hostKey = null; pending = null },
            title = { Text(if (changed) "${jump.name}: the key has changed" else "Trust ${jump.name}?") },
            text = {
                Text(
                    (if (changed) "The server shows another key than the one you accepted before. If it was not reinstalled, someone may be in between.\n\n"
                    else "First connection to ${jump.host}. Check that this is its key:\n\n") + key,
                    fontFamily = FontFamily.Monospace, fontSize = 13.sp,
                )
            },
            confirmButton = { TextButton(onClick = { hostKey = null; pending?.let { wake(it, key) }; pending = null }) { Text(if (changed) "Accept the new key" else "Trust and wake") } },
            dismissButton = { TextButton(onClick = { hostKey = null; pending = null }) { Text("Cancel") } },
        )
    }
}

/** The Termwright logo: the four tiles of the app icon (res/drawable-nodpi/tw_logo.png). */
@Composable
fun TwLogo(size: androidx.compose.ui.unit.Dp) {
    androidx.compose.foundation.Image(
        painter = androidx.compose.ui.res.painterResource(R.drawable.tw_logo),
        contentDescription = "Termwright",
        modifier = Modifier.size(size),
    )
}

@Composable
private fun Empty(onImport: () -> Unit) {
    Column(Modifier.fillMaxSize().padding(32.dp), verticalArrangement = Arrangement.Center, horizontalAlignment = Alignment.CenterHorizontally) {
        TwLogo(64.dp)
        Spacer(Modifier.height(18.dp))
        Text("No sessions yet", fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.height(10.dp))
        Text(
            "In Termwright on your computer: File → Export Sessions → Zip archive. Send the zip to this phone and open it here.",
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )
        Spacer(Modifier.height(20.dp))
        Button(onClick = onImport) { Text("Import a Termwright export") }
    }
}

@Composable
private fun SessionRow(s: Sess, up: Boolean?, busy: Boolean, fav: Boolean, onOpen: () -> Unit, onWake: () -> Unit) {
    Row(
        Modifier.fillMaxWidth().clickable(onClick = onOpen).padding(horizontal = 16.dp, vertical = 10.dp),
        verticalAlignment = Alignment.CenterVertically,
    ) {
        ProtoBadge(s)
        Spacer(Modifier.width(12.dp))
        Column(Modifier.weight(1f)) {
            Text((if (fav) "★ " else "") + s.name, fontWeight = FontWeight.Medium, maxLines = 1, overflow = TextOverflow.Ellipsis)
            Text(listOf(if (s.user.isNotBlank() && !s.isWol) "${s.user}@${s.host}" else s.host, s.protocol.replace("WOL", "Wake-on-LAN"))
                .filter { it.isNotBlank() }.joinToString(" · "), fontSize = 12.sp, fontFamily = FontFamily.Monospace,
                color = MaterialTheme.colorScheme.onSurfaceVariant, maxLines = 1, overflow = TextOverflow.Ellipsis)
        }
        if (up != null) {
            Box(Modifier.padding(horizontal = 10.dp).size(9.dp).clip(CircleShape).background(if (up) Color(0xFF22C55E) else Color(0xFFF59E0B)))
        }
        if (s.canWake) {
            if (busy) CircularProgressIndicator(Modifier.size(22.dp), strokeWidth = 2.dp)
            else FilledTonalButton(onClick = onWake, contentPadding = PaddingValues(horizontal = 14.dp)) { Text("Wake") }
        }
    }
}

@Composable
private fun ProtoBadge(s: Sess) {
    val (txt, col) = when (s.protocol) {
        "WOL" -> "⏻" to Color(0xFFF59E0B)
        "SSH2" -> ">_" to Color(0xFF10B981)
        "Telnet" -> "T" to Color(0xFFF97316)
        "Serial" -> "S" to Color(0xFF8B5CF6)
        "RDP" -> "R" to Color(0xFF3B82F6)
        "VNC" -> "V" to Color(0xFF06B6D4)
        else -> s.protocol.take(1) to Color(0xFF64748B)
    }
    Box(Modifier.size(38.dp).clip(RoundedCornerShape(10.dp)).background(col), contentAlignment = Alignment.Center) {
        Text(txt, color = Color.White, fontWeight = FontWeight.Bold, fontSize = 15.sp)
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun Details(s: Sess, all: List<Sess>, fav: Boolean, onDismiss: () -> Unit, onWake: () -> Unit, onFavourite: () -> Unit, onLogin: (Sess) -> Unit) {
    val jump = all.firstOrNull { it.path == s.jump }
    ModalBottomSheet(onDismissRequest = onDismiss) {
        Column(Modifier.padding(horizontal = 24.dp).padding(bottom = 32.dp)) {
            Text(s.name, fontSize = 20.sp, fontWeight = FontWeight.SemiBold)
            Text(s.folder.ifEmpty { "Top level" }, color = MaterialTheme.colorScheme.onSurfaceVariant)
            Spacer(Modifier.height(16.dp))
            Field("Address", s.host.ifEmpty { "-" })
            if (s.mac.isNotBlank()) Field("MAC address", s.mac)
            if (s.wolNetwork.isNotBlank()) Field("Network", s.wolNetwork)
            if (s.knock.isNotBlank()) Field("Sleep proxy ports", s.knock)
            Field("Jump host", jump?.let { "${it.name} (${it.user}@${it.host})" } ?: "none - only from the device's network")
            Spacer(Modifier.height(16.dp))
            if (s.canWake) Button(onClick = onWake, Modifier.fillMaxWidth()) { Text("Wake ${s.name}") }
            if (jump != null) OutlinedButton(onClick = { onLogin(jump) }, Modifier.fillMaxWidth()) { Text("Login for ${jump.name}…") }
            if (s.canWake) OutlinedButton(onClick = onFavourite, Modifier.fillMaxWidth()) { Text(if (fav) "Remove from the Wake tile" else "Use for the Wake tile") }
            if (s.canWake) Text(
                "The Wake tile is in the quick settings (swipe down twice, edit, drag Wake in).",
                fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant, modifier = Modifier.padding(top = 6.dp),
            )
        }
    }
}

@Composable
private fun Field(label: String, value: String) {
    Column(Modifier.padding(vertical = 4.dp)) {
        Text(label, fontSize = 12.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
        Text(value, fontFamily = FontFamily.Monospace)
    }
}

@Composable
private fun LoginDialog(jump: Sess, current: Login, error: String, onDismiss: () -> Unit, onSave: (Login) -> Unit) {
    var password by remember { mutableStateOf(current.password) }
    var key by remember { mutableStateOf(current.key) }
    var passphrase by remember { mutableStateOf(current.passphrase) }
    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text("Login for ${jump.name}") },
        text = {
            Column {
                Text("${jump.user}@${jump.host} - the packet is sent from there when this phone is not on the device's network. Kept encrypted on this phone.",
                    fontSize = 13.sp, color = MaterialTheme.colorScheme.onSurfaceVariant)
                if (error.isNotBlank()) Text(error, color = MaterialTheme.colorScheme.error, fontSize = 13.sp, modifier = Modifier.padding(top = 6.dp))
                Spacer(Modifier.height(10.dp))
                OutlinedTextField(password, { password = it }, label = { Text("Password") }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation(), keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Password))
                Spacer(Modifier.height(6.dp))
                OutlinedTextField(key, { key = it }, label = { Text("or private key (paste)") }, minLines = 2, maxLines = 4,
                    textStyle = LocalTextStyle.current.copy(fontFamily = FontFamily.Monospace, fontSize = 11.sp))
                if (key.isNotBlank()) OutlinedTextField(passphrase, { passphrase = it }, label = { Text("Key passphrase") }, singleLine = true,
                    visualTransformation = PasswordVisualTransformation())
            }
        },
        confirmButton = { TextButton(onClick = { onSave(Login(password, key.trim(), passphrase)) }) { Text("Save") } },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Cancel") } },
    )
}
