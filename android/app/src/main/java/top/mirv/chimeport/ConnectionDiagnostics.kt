package top.mirv.chimeport

import android.content.Context
import android.content.SharedPreferences
import java.security.MessageDigest

/** No credentials, token values, HTTP bodies, or raw URLs are persisted here. */
enum class DeliveryStatus {
    CONNECTING, CONNECTED, RETRYING, LOGIN_REQUIRED, STOPPED
}

data class DeliverySnapshot(
    val status: DeliveryStatus = DeliveryStatus.STOPPED,
    val lastConnectedAt: Long = 0L,
    val lastSyncAt: Long = 0L,
    val lastEventAt: Long = 0L,
    val lastErrorAt: Long = 0L,
    val lastError: String = "",
    val retryAt: Long = 0L
)

object ConnectionDiagnostics {
    private const val FILE = "chimeport_delivery_diagnostics"

    private fun prefs(context: Context): SharedPreferences =
        context.getSharedPreferences(FILE, Context.MODE_PRIVATE)

    private fun accountPrefix(serverId: String): String {
        val hash = MessageDigest.getInstance("SHA-256")
            .digest(serverId.toByteArray(Charsets.UTF_8))
            .take(12).joinToString("") { "%02x".format(it.toInt() and 255) }
        return "account.$hash."
    }

    fun read(context: Context, serverId: String): DeliverySnapshot {
        val p = prefs(context)
        val key = accountPrefix(serverId)
        return DeliverySnapshot(
            status = runCatching {
                DeliveryStatus.valueOf(p.getString(key + "status", "STOPPED")!!)
            }.getOrDefault(DeliveryStatus.STOPPED),
            lastConnectedAt = p.getLong(key + "connected", 0),
            lastSyncAt = p.getLong(key + "sync", 0),
            lastEventAt = p.getLong(key + "event", 0),
            lastErrorAt = p.getLong(key + "errorTime", 0),
            lastError = p.getString(key + "error", "") ?: "",
            retryAt = p.getLong(key + "retry", 0)
        )
    }

    fun connecting(context: Context, id: String) {
        prefs(context).edit().putString(accountPrefix(id) + "status", "CONNECTING").apply()
    }

    fun connected(context: Context, id: String) {
        val key = accountPrefix(id)
        prefs(context).edit()
            .putString(key + "status", "CONNECTED")
            .putLong(key + "connected", System.currentTimeMillis())
            .putLong(key + "retry", 0L)
            .putString(key + "error", "")
            .apply()
    }

    fun synced(context: Context, id: String) {
        prefs(context).edit().putLong(
            accountPrefix(id) + "sync", System.currentTimeMillis()
        ).apply()
    }

    fun event(context: Context, id: String) {
        prefs(context).edit().putLong(
            accountPrefix(id) + "event", System.currentTimeMillis()
        ).apply()
    }

    fun retrying(context: Context, id: String, delayMs: Long, reason: String) {
        val key = accountPrefix(id)
        prefs(context).edit()
            .putString(key + "status", "RETRYING")
            .putLong(key + "errorTime", System.currentTimeMillis())
            .putString(key + "error", reason.take(150))
            .putLong(key + "retry", System.currentTimeMillis() + delayMs)
            .apply()
    }

    fun loginRequired(context: Context, id: String) {
        val key = accountPrefix(id)
        prefs(context).edit()
            .putString(key + "status", "LOGIN_REQUIRED")
            .putString(key + "error", "Сессия недействительна")
            .putLong(key + "errorTime", System.currentTimeMillis())
            .putLong(key + "retry", 0L)
            .apply()
    }

    fun stopped(context: Context, id: String) {
        prefs(context).edit()
            .putString(accountPrefix(id) + "status", "STOPPED")
            .putLong(accountPrefix(id) + "retry", 0L)
            .apply()
    }

    fun bootResult(context: Context, result: String) {
        prefs(context).edit()
            .putLong("boot.time", System.currentTimeMillis())
            .putString("boot.result", result.take(160))
            .apply()
    }

    fun bootStatus(context: Context): Pair<Long, String> {
        val pref = prefs(context)
        return pref.getLong("boot.time", 0L) to (pref.getString("boot.result", "") ?: "")
    }

    fun serviceStarted(context: Context) {
        prefs(context).edit().putLong("service.started", System.currentTimeMillis()).apply()
    }
    fun serviceStartTime(context: Context): Long = prefs(context).getLong("service.started", 0L)

    fun forget(context: Context, id: String) {
        val key = accountPrefix(id)
        val edit = prefs(context).edit()
        listOf("status", "connected", "sync", "event", "errorTime", "error", "retry")
            .forEach { edit.remove(key + it) }
        edit.apply()
    }
}

/** Abstract error category for diagnostics, without HTTP response bodies or credentials. */
internal fun deliveryErrorLabel(error: Exception): String = when (error) {
    is top.mirv.chimeport.data.ApiError -> "HTTP " + error.code
    is java.net.UnknownHostException -> "DNS: сервер не найден"
    is java.net.SocketTimeoutException -> "Истекло время ожидания"
    is java.net.ConnectException -> "Нет соединения с сервером"
    is java.io.EOFException -> "Поток событий прерван"
    else -> error.javaClass.simpleName.take(64).ifBlank { "Ошибка соединения" }
}
