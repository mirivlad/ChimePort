package top.mirv.chimeport.data

import org.json.JSONArray
import org.json.JSONObject
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URI
import java.net.URL
import java.nio.charset.StandardCharsets
import java.security.MessageDigest
import java.security.SecureRandom
import java.util.Base64
import java.util.concurrent.ConcurrentHashMap

class ApiError(val code: Int, text: String) : IOException(text)

object Api {
    private val refreshLocks = ConcurrentHashMap<String, Any>()

    /** Rotate at most once per connection, shared by foreground and background sync. */
    fun ensureFresh(cache: Cache, account: ServerAccount): ServerAccount {
        if (account.providerOrigin.isEmpty()) return account
        val lock = refreshLocks.getOrPut(account.id) { Any() }
        synchronized(lock) {
            val current = cache.servers().firstOrNull { it.id == account.id }
                ?: throw IOException("Подключение удалено.")
            if (current.providerOrigin.isEmpty()) return current
            val expiresSoon = current.accessExpiresAt == 0L ||
                System.currentTimeMillis() + 120_000L >= current.accessExpiresAt
            if (!expiresSoon) return current
            if (!current.refreshToken.startsWith("cp_r_")) {
                throw ApiError(401,"Для подключения TMS требуется повторный вход.")
            }
            val retryId = cache.refreshRetryId(current.id)
            val response = call(current.origin, "/api/v1/external/refresh",
                post = JSONObject().put("refresh_token",current.refreshToken)
                    .put("retry_id",retryId))
            val access = response.getString("access_token")
            val refresh = response.getString("refresh_token")
            val ttl = response.optLong("expires_in",0L)
            if (!access.startsWith("cp_e_") || !refresh.startsWith("cp_r_") ||
                ttl !in 1..3600) {
                throw IOException("Сервер вернул некорректное продление авторизации.")
            }
            // Tokens are written in one transaction after both values validated.
            cache.replaceSessionTokens(current.id,access,refresh,System.currentTimeMillis()+ttl*1000L)
            return cache.servers().first { it.id == current.id }
        }
    }
    fun normalizeOrigin(raw: String): String {
        val input = raw.trim().trimEnd('/')
        if (input.isEmpty()) throw IllegalArgumentException("Введите адрес сервера.")
        val withScheme = if ("://" !in input) "https://$input" else input
        val uri = try { URI(withScheme) } catch (_: Exception) {
            throw IllegalArgumentException("Неправильный адрес сервера.")
        }
        if (uri.scheme != "https" || uri.host.isNullOrBlank() || uri.userInfo != null ||
            uri.rawQuery != null || uri.rawFragment != null || (uri.rawPath != "" && uri.rawPath != "/") ||
            uri.port !in -1..65535 || uri.port == 0
        ) throw IllegalArgumentException("Укажите HTTPS-адрес сервера без пути и параметров.")
        return URI("https", null, uri.host, uri.port, null, null, null).toASCIIString().trimEnd('/')
    }

    private fun call(origin: String, path: String, token: String? = null, post: JSONObject? = null, method: String = if (post == null) "GET" else "POST"): JSONObject {
        val url = URL(origin + path)
        val conn = url.openConnection() as HttpURLConnection
        try {
            conn.connectTimeout = 12_000
            conn.readTimeout = 20_000
            conn.instanceFollowRedirects = false
            conn.requestMethod = method
            conn.setRequestProperty("Accept", "application/json")
            if (token != null) conn.setRequestProperty("Authorization", "Bearer $token")
            if (post != null) {
                conn.doOutput = true
                conn.setRequestProperty("Content-Type", "application/json; charset=utf-8")
                conn.outputStream.use { it.write(post.toString().toByteArray(StandardCharsets.UTF_8)) }
            }
            val status = conn.responseCode
            if (status in 300..399) throw ApiError(status, "Сервер перенаправляет запрос. Проверьте его адрес.")
            val response = (if (status in 200..299) conn.inputStream else conn.errorStream)
                ?.bufferedReader(StandardCharsets.UTF_8)?.use { it.readText() } ?: ""
            val objectValue = runCatching { JSONObject(response) }.getOrElse { JSONObject() }
            if (status !in 200..299) {
                val message = objectValue.optJSONObject("error")?.optString("message")
                    ?.takeIf { it.isNotBlank() }
                val detail = when (status) {
                    401 -> "Сессия истекла или неверный пароль. Войдите снова."
                    403 -> "Нет доступа. Проверьте учётную запись."
                    404 -> "Ресурс на сервере не найден."
                    429 -> "Слишком много запросов. Повторите позже."
                    in 500..599 -> "Сервер временно недоступен (HTTP $status)."
                    else -> message ?: "HTTP $status"
                }
                throw ApiError(status, detail)
            }
            return objectValue
        } catch (e: javax.net.ssl.SSLException) {
            throw IOException("Ошибка HTTPS или сертификата сервера. Проверка TLS не отключалась.", e)
        } catch (e: java.net.UnknownHostException) {
            throw IOException("Не удалось найти сервер. Проверьте домен и интернет.", e)
        } catch (e: java.net.SocketTimeoutException) {
            throw IOException("Сервер не ответил вовремя. Проверьте соединение.", e)
        } finally { conn.disconnect() }
    }

    fun probe(origin: String): InstanceInfo {
        val value = call(origin, "/api/v1/instance")
        if (value.optInt("api_version") != 1 || value.optString("instance_id").isBlank()) {
            throw IOException("На адресе нет совместимого сервера ChimePort API v1.")
        }
        return InstanceInfo(value.getString("instance_id"), value.optString("name", "ChimePort"))
    }

    data class TrustedDiscovery(val issuer: String, val delivery: String)

    fun discoverTms(issuer: String): TrustedDiscovery {
        val obj = call(issuer, "/.well-known/chimeport.json")
        if (obj.optString("protocol") != "chimeport-trusted-app-v1" ||
            normalizeOrigin(obj.optString("issuer")) != issuer) {
            throw IOException("На адресе нет совместимой интеграции TMS.")
        }
        return TrustedDiscovery(issuer, normalizeOrigin(obj.getString("chimeport_url")))
    }

    /** Two TMS instances on one ChimePort must not share local-cache identities. */
    fun scopedInstanceId(chimeInstance: String, issuer: String): String {
        val hash = MessageDigest.getInstance("SHA-256")
            .digest(issuer.toByteArray(StandardCharsets.UTF_8))
            .joinToString("") { "%02x".format(it) }.take(24)
        return "$chimeInstance:trusted:$hash"
    }

    fun actualInstanceId(server: ServerAccount): String {
        val actual = probe(server.origin).id
        return if (server.providerOrigin.isNotEmpty()) scopedInstanceId(actual, server.providerOrigin) else actual
    }

    /**
     * Password goes ONLY to the typed, verified TMS origin.
     * TMS issues an app-specific short-lived ticket using the PKCE challenge.
     */
    fun loginViaTms(issuer: String, delivery: String, username: String, password: String): SessionInfo {
        val verifierBytes = ByteArray(32)
        SecureRandom().nextBytes(verifierBytes)
        val verifier = Base64.getUrlEncoder().withoutPadding().encodeToString(verifierBytes)
        val challenge = Base64.getUrlEncoder().withoutPadding().encodeToString(
            MessageDigest.getInstance("SHA-256").digest(verifier.toByteArray(StandardCharsets.US_ASCII))
        )
        val login = call(issuer, "/api/notifications/mobile/login", post = JSONObject()
            .put("username", username)
            .put("password", password)
            .put("code_challenge", challenge)
            .put("device_name", "Android · ChimePort"))
        val returnedOrigin = normalizeOrigin(login.getString("chimeport_url"))
        if (returnedOrigin != delivery) throw IOException("TMS указал другой сервер уведомлений. Проверьте подключение.")
        val recipient = login.getString("recipient").toLongOrNull()
            ?: throw IOException("TMS вернул неверный идентификатор пользователя.")
        if (recipient <= 0) throw IOException("TMS вернул неверный идентификатор пользователя.")
        val ticket = login.getString("ticket")
        if (!ticket.startsWith("cp_k_")) throw IOException("TMS вернул некорректный пропуск.")
        val exchanged = call(delivery, "/api/v1/external/exchange", post = JSONObject()
            .put("ticket", ticket)
            .put("code_verifier", verifier))
        val token = exchanged.getString("access_token")
        if (!token.startsWith("cp_e_")) throw IOException("Сервер уведомлений вернул некорректную сессию.")
        val refresh = exchanged.getString("refresh_token")
        val ttl = exchanged.optLong("expires_in",0)
        if (!refresh.startsWith("cp_r_") || ttl !in 1..3600) {
            throw IOException("Сервер уведомлений не поддерживает автоматическое продление.")
        }
        return SessionInfo(token, login.getString("username"), recipient,refresh,
            System.currentTimeMillis()+ttl*1000L)
    }

    fun login(origin: String, username: String, password: String): SessionInfo {
        val value = call(origin, "/api/v1/auth/login", post = JSONObject()
            .put("username", username.trim())
            .put("password", password)
            .put("mode", "api"))
        val user = value.getJSONObject("user")
        return SessionInfo(value.getString("access_token"), user.getString("username"), user.getLong("id"))
    }

    fun channels(server: ServerAccount): List<ChannelInfo> {
        if (server.providerOrigin.isNotEmpty()) {
            val info = call(server.origin, "/api/v1/external/info", server.token)
            if (info.optString("origin") != server.providerOrigin) {
                throw IOException("Несовпадение привязки TMS к сервису уведомлений.")
            }
            val slug = info.getString("application")
            return listOf(ChannelInfo(server.id, server.label, 0, slug, "TMS", 0))
        }
        val data = call(server.origin, "/api/v1/channels", server.token).optJSONArray("items") ?: JSONArray()
        return (0 until data.length()).map { i ->
            val v = data.getJSONObject(i)
            ChannelInfo(server.id, server.label, v.getLong("id"), v.getString("slug"), v.getString("name"), 0)
        }
    }

    fun messages(server: ServerAccount, cursor: String): Page {
        val path = (if (server.providerOrigin.isNotEmpty()) "/api/v1/external/messages?limit=200"
                    else "/api/v1/messages?limit=200") +
            if (cursor.isNotBlank()) "&after=" + java.net.URLEncoder.encode(cursor, "UTF-8") else ""
        val data = call(server.origin, path, server.token)
        val items = data.optJSONArray("items") ?: JSONArray()
        val result = (0 until items.length()).map { i ->
            val v = items.getJSONObject(i)
            Notice(
                id = v.getString("id"), serverId = server.id, serverLabel = server.label,
                channel = v.getString("channel"), channelName = v.getString("channel"),
                source = v.optString("source"), title = v.getString("title"),
                body = v.optString("body"), priority = v.optString("priority", "normal"),
                url = v.optString("url"), createdAt = v.getString("created_at"),
                readAt = if (v.isNull("read_at")) null else v.optString("read_at")
            )
        }
        return Page(result, data.optString("next_cursor"), data.optBoolean("has_more"))
    }

    fun read(server: ServerAccount, id: String) {
        require(id.all { it.isDigit() }) { "Неверный идентификатор сообщения" }
        call(server.origin, if (server.providerOrigin.isNotEmpty()) "/api/v1/external/messages/$id/read"
             else "/api/v1/messages/$id/read", server.token, JSONObject())
    }

    fun unread(server: ServerAccount, id: String) {
        require(id.isNotEmpty() && id.all { it.isDigit() }) { "Неверный идентификатор сообщения" }
        call(server.origin, if (server.providerOrigin.isNotEmpty()) "/api/v1/external/messages/$id/read"
             else "/api/v1/messages/$id/read", server.token, method = "DELETE")
    }

    fun logout(server: ServerAccount) {
        call(server.origin, if (server.providerOrigin.isNotEmpty()) "/api/v1/external/logout"
             else "/api/v1/auth/logout", server.token, JSONObject())
    }
}
