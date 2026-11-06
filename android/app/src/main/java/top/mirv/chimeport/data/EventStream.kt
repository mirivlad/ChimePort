package top.mirv.chimeport.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext
import org.json.JSONObject
import java.io.EOFException
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.ConcurrentHashMap

/**
 * One authenticated SSE connection per account, kept only while the UI is in the foreground.
 * The server streams a change hint; REST remains the source of truth for history and read state.
 */
class EventStream {
    private val connections = ConcurrentHashMap<String, HttpURLConnection>()

    fun disconnect(id: String) {
        connections.remove(id)?.disconnect()
    }

    fun disconnectAll() {
        connections.keys.toList().forEach(::disconnect)
    }

    suspend fun listen(
        account: ServerAccount,
        onConnected: suspend () -> Unit,
        onMessage: suspend (String) -> Unit
    ) = withContext(Dispatchers.IO) {
        val connection = (URL(account.origin + if (account.providerOrigin.isNotEmpty()) "/api/v1/external/events" else "/api/v1/events").openConnection() as HttpURLConnection).apply {
            requestMethod = "GET"
            connectTimeout = 12_000
            readTimeout = 75_000 // Server sends a keepalive comment every 25 seconds.
            instanceFollowRedirects = false
            setRequestProperty("Authorization", "Bearer " + account.token)
            setRequestProperty("Accept", "text/event-stream")
            setRequestProperty("Cache-Control", "no-cache")
        }
        connections.put(account.id, connection)?.disconnect()
        try {
            val status = connection.responseCode
            if (status == 401 || status == 403) {
                throw ApiError(status, "Сессия недействительна. Войдите снова.")
            }
            if (status != 200) {
                throw IOException("Поток уведомлений: HTTP $status")
            }
            if (connection.contentType?.substringBefore(';')?.trim() != "text/event-stream") {
                throw IOException("Сервер не вернул поток событий.")
            }
            onConnected()
            val parser = SseParser()
            connection.inputStream.bufferedReader(Charsets.UTF_8).use { reader ->
                while (true) {
                    val line = reader.readLine() ?: throw EOFException("Соединение с сервером прервано.")
                    val data = parser.accept(line)
                    if (data != null) {
                        val lastId = runCatching {
                            JSONObject(data).optString("last_id")
                        }.getOrDefault("")
                        if (lastId.isNotBlank()) onMessage(lastId)
                    }
                }
            }
        } finally {
            connections.remove(account.id, connection)
            connection.disconnect()
        }
    }
}

/** Parses SSE lines; dispatch only a fully terminated messages_available event. */
class SseParser {
    private var event = ""
    private val data = StringBuilder()

    fun accept(line: String): String? {
        if (line.isEmpty()) {
            val result = if (event == "messages_available" && data.isNotEmpty()) {
                data.toString().trimEnd('\n')
            } else null
            event = ""
            data.setLength(0)
            return result
        }
        if (line.startsWith(":")) return null
        when {
            line.startsWith("event:") -> event = line.substringAfter(':').trimStart()
            line.startsWith("data:") -> {
                if (data.length + line.length > 8192) {
                    event = ""
                    data.setLength(0)
                } else {
                    data.append(line.substringAfter(':').trimStart()).append('\n')
                }
            }
        }
        return null
    }
}
