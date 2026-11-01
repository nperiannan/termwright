package com.github.nperiannan.termwright

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import org.json.JSONArray
import org.json.JSONObject
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/** A Termwright session as the app keeps it (the fields of mobile/core appSession). */
data class Sess(
    val path: String, val name: String, val folder: String, val protocol: String,
    val host: String, val port: Int, val user: String, val mac: String,
    val wolNetwork: String, val wolPort: Int, val broadcast: String, val knock: String,
    val jump: String, val deviceType: String, val environment: String,
    val wakeSchedule: String = "", val wakeAt: String = "",
) {
    val canWake get() = mac.isNotBlank()
    /** SSH and Telnet sessions open a terminal on the phone. */
    val canTerminal get() = host.isNotBlank() && (protocol == "SSH2" || protocol == "Telnet")
    val isWol get() = protocol == "WOL"

    fun toJson(): JSONObject = JSONObject()
        .put("path", path).put("name", name).put("folder", folder).put("protocol", protocol)
        .put("host", host).put("port", port).put("user", user).put("mac", mac)
        .put("wolNetwork", wolNetwork).put("wolPort", wolPort).put("broadcast", broadcast).put("knock", knock)
        .put("jump", jump).put("deviceType", deviceType).put("environment", environment)
        .put("wakeSchedule", wakeSchedule).put("wakeAt", wakeAt)

    companion object {
        fun from(o: JSONObject) = Sess(
            o.optString("path"), o.optString("name"), o.optString("folder"), o.optString("protocol"),
            o.optString("host"), o.optInt("port"), o.optString("user"), o.optString("mac"),
            o.optString("wolNetwork"), o.optInt("wolPort"), o.optString("broadcast"), o.optString("knock"),
            o.optString("jump"), o.optString("deviceType"), o.optString("environment"),
            o.optString("wakeSchedule"), o.optString("wakeAt"),
        )
    }
}

/** A wake on a schedule from the phone: "daily" or "weekdays" at "HH:MM" (local time). */
data class Sched(val mode: String = "daily", val at: String = "07:00", val on: Boolean = false)

/** What the widget shows for a device: "up", "down", "waking", or a short reason it could not be woken. */
data class Status(val state: String, val note: String = "")

/** The login to a jump host or a device, kept encrypted with a key in the Android Keystore (user: "" = the session's). */
data class Login(val password: String = "", val key: String = "", val passphrase: String = "", val hostKey: String = "", val user: String = "") {
    val usable get() = password.isNotEmpty() || key.isNotBlank()
}

/** Sessions (sessions.json in the app's files) and logins (encrypted in preferences). */
class Store(private val ctx: Context) {
    private val file = File(ctx.filesDir, "sessions.json")
    private val prefs = ctx.getSharedPreferences("termwright", Context.MODE_PRIVATE)

    fun sessions(): List<Sess> = try {
        val a = JSONArray(file.readText())
        (0 until a.length()).map { Sess.from(a.getJSONObject(it)) }
    } catch (_: Exception) {
        emptyList()
    }

    fun saveSessions(list: List<Sess>) {
        val a = JSONArray()
        list.forEach { a.put(it.toJson()) }
        file.writeText(a.toString())
    }

    var favourite: String
        get() = prefs.getString("favourite", "") ?: ""
        set(v) = prefs.edit().putString("favourite", v).apply()

    /** The terminal's text size. */
    var fontSize: Int
        get() = prefs.getInt("fontSize", 13)
        set(v) = prefs.edit().putInt("fontSize", v.coerceIn(8, 24)).apply()

    // ---- home screen widget: the devices on it, and what was last seen of them ----
    var widget: List<String>
        get() = strList(prefs.getString("widget", null))
        set(v) = prefs.edit().putString("widget", JSONArray(v).toString()).apply()

    fun status(path: String): Status? = obj("status").optJSONObject(path)?.let { Status(it.optString("state"), it.optString("note")) }

    fun setStatus(path: String, st: Status) = synchronized(LOCK) {
        val all = obj("status")
        all.put(path, JSONObject().put("state", st.state).put("note", st.note))
        prefs.edit().putString("status", all.toString()).commit()
    }

    /** When the widget's devices were last checked (milliseconds), 0 if never. */
    var checkedAt: Long
        get() = prefs.getLong("checkedAt", 0)
        set(v) = prefs.edit().putLong("checkedAt", v).apply()

    // ---- wakes on a schedule ----
    /** The phone's schedule for a device; off, with the desktop's schedule (from the import) as the starting values, if none was set. */
    fun schedule(s: Sess): Sched = obj("schedules").optJSONObject(s.path)?.let { Sched(it.optString("mode", "daily"), it.optString("at", "07:00"), it.optBoolean("on")) }
        ?: Sched(s.wakeSchedule.ifEmpty { "daily" }, s.wakeAt.ifEmpty { "07:00" }, false)

    fun setSchedule(path: String, sc: Sched) = synchronized(LOCK) {
        val all = obj("schedules")
        all.put(path, JSONObject().put("mode", sc.mode).put("at", sc.at).put("on", sc.on))
        prefs.edit().putString("schedules", all.toString()).commit()
    }

    private fun obj(key: String): JSONObject = try {
        JSONObject(prefs.getString(key, null) ?: "{}")
    } catch (_: Exception) {
        JSONObject()
    }

    private fun strList(json: String?): List<String> = try {
        val a = JSONArray(json ?: "[]")
        (0 until a.length()).map { a.getString(it) }
    } catch (_: Exception) {
        emptyList()
    }

    fun login(path: String): Login {
        val all = logins()
        val o = all.optJSONObject(path) ?: return Login()
        return Login(o.optString("password"), o.optString("key"), o.optString("passphrase"), o.optString("hostKey"), o.optString("user"))
    }

    fun saveLogin(path: String, l: Login) {
        val all = logins()
        all.put(path, JSONObject().put("password", l.password).put("key", l.key).put("passphrase", l.passphrase).put("hostKey", l.hostKey).put("user", l.user))
        prefs.edit().putString("logins", encrypt(all.toString())).apply()
    }

    private fun logins(): JSONObject {
        val enc = prefs.getString("logins", null) ?: return JSONObject()
        return try {
            JSONObject(decrypt(enc))
        } catch (_: Exception) {
            JSONObject()
        }
    }

    // ---- encryption: AES-GCM with a key that never leaves the Android Keystore ----
    private fun key(): SecretKey {
        val ks = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (ks.getEntry(ALIAS, null) as? KeyStore.SecretKeyEntry)?.let { return it.secretKey }
        val gen = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
        gen.init(
            KeyGenParameterSpec.Builder(ALIAS, KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .build()
        )
        return gen.generateKey()
    }

    private fun encrypt(text: String): String {
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.ENCRYPT_MODE, key())
        val out = c.iv + c.doFinal(text.toByteArray())
        return Base64.encodeToString(out, Base64.NO_WRAP)
    }

    private fun decrypt(b64: String): String {
        val all = Base64.decode(b64, Base64.NO_WRAP)
        val c = Cipher.getInstance("AES/GCM/NoPadding")
        c.init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, all, 0, 12))
        return String(c.doFinal(all, 12, all.size - 12))
    }

    companion object {
        private const val ALIAS = "termwright-logins"
        /** Statuses are written from the widget's workers and the alarms at the same time. */
        private val LOCK = Any()
    }
}
