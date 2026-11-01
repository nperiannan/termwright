package com.github.nperiannan.termwright

import android.content.Context
import android.net.ConnectivityManager
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.async
import kotlinx.coroutines.awaitAll
import kotlinx.coroutines.coroutineScope
import kotlinx.coroutines.withContext
import org.json.JSONArray
import org.json.JSONObject
import java.net.Inet4Address
import twcore.Twcore

/** The result of a wake, or why the user has to act first. */
sealed class WakeOutcome {
    data class Done(val text: String) : WakeOutcome()
    data class Failed(val text: String) : WakeOutcome()
    /** The jump host needs a login (none saved yet, or the saved one was refused). */
    data class NeedLogin(val jump: Sess, val error: String) : WakeOutcome()
    /** First connection to the jump host: the user accepts its key. */
    data class NewHostKey(val jump: Sess, val key: String) : WakeOutcome()
    /** The jump host shows another key than the one accepted before. */
    data class ChangedHostKey(val jump: Sess, val key: String) : WakeOutcome()
}

/** Calls into the Go part (twcore.aar, from mobile/core). */
object Core {
    fun import(ctx: Context, name: String, data: ByteArray): List<Sess> {
        val a = JSONArray(Twcore.importSessions(ctx.cacheDir.absolutePath, name, data))
        return (0 until a.length()).map { Sess.from(a.getJSONObject(it)) }
    }

    /** The phone's IPv4 networks, e.g. "192.168.1.23/24" (Go cannot list them on Android 11+). */
    fun localNets(ctx: Context): List<String> {
        val cm = ctx.getSystemService(ConnectivityManager::class.java) ?: return emptyList()
        val lp = cm.getLinkProperties(cm.activeNetwork) ?: return emptyList()
        return lp.linkAddresses.filter { it.address is Inet4Address }.map { "${it.address.hostAddress}/${it.prefixLength}" }
    }

    suspend fun wake(ctx: Context, store: Store, d: Sess, sessions: List<Sess>, acceptKey: String = ""): WakeOutcome =
        withContext(Dispatchers.IO) {
            val jump = sessions.firstOrNull { it.path == d.jump }
            val nets = localNets(ctx)
            val req = JSONObject().put("device", d.toJson()).put("localNets", JSONArray(nets))
            if (jump != null) {
                var login = store.login(jump.path)
                if (acceptKey.isNotEmpty()) {
                    login = login.copy(hostKey = acceptKey)
                    store.saveLogin(jump.path, login)
                }
                if (login.usable) {
                    req.put("jump", JSONObject().put("host", jump.host).put("port", jump.port).put("user", jump.user)
                        .put("password", login.password).put("key", login.key).put("passphrase", login.passphrase)
                        // the first time no key is expected; Go returns it and the user accepts it
                        .put("hostKey", login.hostKey.ifEmpty { "SHA256:-" }))
                } else if (!onDeviceNetwork(d, nets)) {
                    return@withContext WakeOutcome.NeedLogin(jump, "")
                }
            }
            try {
                val res = JSONObject(Twcore.wake(req.toString()))
                val via = res.optString("via")
                val sent = res.optJSONArray("sent")?.let { a -> (0 until a.length()).joinToString(", ") { a.getString(it) } } ?: ""
                val knock = res.optString("knocked")
                WakeOutcome.Done(buildString {
                    append("Magic packet sent to ${d.name}")
                    append(if (via.isEmpty()) " from this phone" else " from ${jump?.name ?: via}")
                    if (sent.isNotEmpty()) append(" ($sent)")
                    if (knock.isNotEmpty()) append(", then connected to $knock")
                })
            } catch (e: Exception) {
                val msg = e.message ?: e.toString()
                when {
                    jump != null && msg.contains("host key changed: ") -> {
                        val key = msg.substringAfter("host key changed: ").trim()
                        val saved = store.login(jump.path).hostKey
                        if (saved.isEmpty()) WakeOutcome.NewHostKey(jump, key) else WakeOutcome.ChangedHostKey(jump, key)
                    }
                    jump != null && (msg.contains("unable to authenticate") || msg.contains("private key")) ->
                        WakeOutcome.NeedLogin(jump, msg)
                    else -> WakeOutcome.Failed(msg)
                }
            }
        }

    fun onDeviceNetwork(d: Sess, nets: List<String>): Boolean = nets.any { n ->
        val ip = n.substringBefore("/")
        val bits = n.substringAfter("/").toIntOrNull() ?: 24
        listOf(d.host, d.wolNetwork.substringBefore("/")).any { it.isNotBlank() && samePrefix(ip, it, bits) }
    }

    private fun samePrefix(a: String, b: String, bits: Int): Boolean {
        fun v(s: String) = s.split(".").mapNotNull { it.toIntOrNull() }.takeIf { it.size == 4 }?.fold(0L) { acc, x -> (acc shl 8) or x.toLong() }
        val x = v(a) ?: return false
        val y = v(b) ?: return false
        val mask = if (bits == 0) 0L else (0xFFFFFFFFL shl (32 - bits)) and 0xFFFFFFFFL
        return (x and mask) == (y and mask)
    }

    /** Whether the device answers: a TCP port (apps cannot ping), several tried at once. */
    suspend fun isUp(d: Sess): Boolean = withContext(Dispatchers.IO) {
        if (d.host.isBlank()) return@withContext false
        val ports = (d.knock.split(',', ' ', ';').mapNotNull { it.trim().toIntOrNull() } +
            listOf(if (d.port > 0) d.port else 0, 22, 445, 3389, 5900, 80)).filter { it > 0 }.distinct().take(6)
        coroutineScope {
            ports.map { p -> async { Twcore.reachable(d.host, p.toLong(), 1200) } }.awaitAll().any { it }
        }
    }
}
