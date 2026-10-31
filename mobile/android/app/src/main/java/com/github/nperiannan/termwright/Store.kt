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
) {
    val canWake get() = mac.isNotBlank()
    val isWol get() = protocol == "WOL"

    fun toJson(): JSONObject = JSONObject()
        .put("path", path).put("name", name).put("folder", folder).put("protocol", protocol)
        .put("host", host).put("port", port).put("user", user).put("mac", mac)
        .put("wolNetwork", wolNetwork).put("wolPort", wolPort).put("broadcast", broadcast).put("knock", knock)
        .put("jump", jump).put("deviceType", deviceType).put("environment", environment)

    companion object {
        fun from(o: JSONObject) = Sess(
            o.optString("path"), o.optString("name"), o.optString("folder"), o.optString("protocol"),
            o.optString("host"), o.optInt("port"), o.optString("user"), o.optString("mac"),
            o.optString("wolNetwork"), o.optInt("wolPort"), o.optString("broadcast"), o.optString("knock"),
            o.optString("jump"), o.optString("deviceType"), o.optString("environment"),
        )
    }
}

/** The login to a jump host, kept encrypted with a key in the Android Keystore. */
data class Login(val password: String = "", val key: String = "", val passphrase: String = "", val hostKey: String = "") {
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

    fun login(path: String): Login {
        val all = logins()
        val o = all.optJSONObject(path) ?: return Login()
        return Login(o.optString("password"), o.optString("key"), o.optString("passphrase"), o.optString("hostKey"))
    }

    fun saveLogin(path: String, l: Login) {
        val all = logins()
        all.put(path, JSONObject().put("password", l.password).put("key", l.key).put("passphrase", l.passphrase).put("hostKey", l.hostKey))
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
    }
}
