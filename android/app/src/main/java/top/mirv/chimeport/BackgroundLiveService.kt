package top.mirv.chimeport

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.os.IBinder
import android.os.Build
import android.content.pm.ServiceInfo
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.SupervisorJob
import kotlinx.coroutines.cancel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import java.security.MessageDigest
import java.util.concurrent.ConcurrentHashMap
import top.mirv.chimeport.data.ApiError
import top.mirv.chimeport.data.Cache
import top.mirv.chimeport.data.EventStream
import top.mirv.chimeport.data.ServerSync

/**
 * User-enabled persistent SSE listener. An ongoing notification is mandatory.
 * This cannot override Android force-stop, battery restrictions or network outages.
 */
class BackgroundLiveService : Service() {
    companion object {
        private const val CHANNEL_ID = "chimeport-background-status"
        private const val NOTIFICATION_ID = 401
        @Volatile var running: Boolean = false
            private set

        fun enable(context: Context) {
            val prefs = context.getSharedPreferences("chimeport_preferences", Context.MODE_PRIVATE)
            prefs.edit().putBoolean("background_live", true).apply()
            if (!running) {
                context.startForegroundService(Intent(context, BackgroundLiveService::class.java))
            }
        }

        private const val ACTION_RECONNECT = "top.mirv.chimeport.action.RECONNECT"

        fun reconnect(context: Context) {
            check(enabled(context)) { "Фоновая доставка выключена" }
            context.startForegroundService(Intent(context, BackgroundLiveService::class.java).apply {
                action = ACTION_RECONNECT
            })
        }

        fun disable(context: Context) {
            context.getSharedPreferences("chimeport_preferences", Context.MODE_PRIVATE)
                .edit().putBoolean("background_live", false).apply()
            context.stopService(Intent(context, BackgroundLiveService::class.java))
        }

        fun enabled(context: Context): Boolean =
            context.getSharedPreferences("chimeport_preferences", Context.MODE_PRIVATE)
                .getBoolean("background_live", false)
    }

    private val scope = CoroutineScope(SupervisorJob() + Dispatchers.IO)
    private val cache by lazy { Cache(this) }
    private val streams = EventStream()
    private val jobs = mutableMapOf<String, Job>()
    private val rejectedSessions = ConcurrentHashMap<String, String>()
    private var manager: Job? = null

    private fun sessionFingerprint(token: String): String =
        MessageDigest.getInstance("SHA-256")
            .digest(token.toByteArray(Charsets.UTF_8))
            .take(12).joinToString("") { "%02x".format(it.toInt() and 255) }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (!enabled(this)) {
            stopSelf()
            return START_NOT_STICKY
        }
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(NotificationChannel(
            CHANNEL_ID, "Фоновое соединение ChimePort", NotificationManager.IMPORTANCE_LOW
        ).apply { description = "Постоянное соединение с собственными серверами" })
        val open = PendingIntent.getActivity(
            this, 0,
            Intent(this, MainActivity::class.java).apply {
                this.flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            },
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val notification = Notification.Builder(this, CHANNEL_ID)
            .setSmallIcon(R.drawable.chimeport_notification)
            .setContentTitle("ChimePort · фоновая доставка")
            .setContentText("Ожидание событий с подключённых серверов")
            .setContentIntent(open)
            .setOngoing(true)
            .setCategory(Notification.CATEGORY_SERVICE)
            .build()
        if (Build.VERSION.SDK_INT >= 34) {
            startForeground(NOTIFICATION_ID, notification, ServiceInfo.FOREGROUND_SERVICE_TYPE_SPECIAL_USE)
        } else {
            startForeground(NOTIFICATION_ID, notification)
        }
        running = true
        ConnectionDiagnostics.serviceStarted(this)
        if (intent?.action == ACTION_RECONNECT) {
            // User explicitly requested reconnection; closing the socket wakes listeners.
            rejectedSessions.clear()
            streams.disconnectAll()
        }
        if (manager?.isActive != true) {
            manager = scope.launch {
                while (isActive && enabled(this@BackgroundLiveService)) {
                    val accounts = cache.servers()
                    val ids = accounts.map { it.id }.toSet()
                    jobs.keys.toList().filter { it !in ids }.forEach { id ->
                        jobs.remove(id)?.cancel()
                        rejectedSessions.remove(id)
                        streams.disconnect(id)
                    }
                    for (account in accounts) {
                        val fingerprint = sessionFingerprint(account.token)
                        if (rejectedSessions[account.id] == fingerprint) continue
                        rejectedSessions.remove(account.id)
                        if (jobs[account.id]?.isActive != true) {
                            jobs[account.id] = launch { observe(account.id) }
                        }
                    }
                    delay(5000L)
                }
            }
        }
        return START_STICKY
    }

    private suspend fun observe(id: String) {
        var failures = 0
        while (currentCoroutineContext().isActive && enabled(this)) {
            val server = cache.servers().firstOrNull { it.id == id } ?: break
            if (server.token.isBlank()) {
                ConnectionDiagnostics.loginRequired(this, id)
                break
            }
            ConnectionDiagnostics.connecting(this, id)
            try {
                val ready = withContext(Dispatchers.IO) { Api.ensureFresh(cache,server) }
                streams.listen(ready,
                    onConnected = {
                        failures = 0
                        ConnectionDiagnostics.connected(this@BackgroundLiveService, id)
                        // Silent catch-up after a restart or lost connection.
                        ServerSync.refresh(cache, id)
                        ConnectionDiagnostics.synced(this@BackgroundLiveService, id)
                        ChangeBus.emit()
                    },
                    onMessage = { lastId ->
                        ConnectionDiagnostics.event(this@BackgroundLiveService, id)
                        val fresh = ServerSync.refresh(cache, id)
                        ConnectionDiagnostics.synced(this@BackgroundLiveService, id)
                        ChangeBus.emit()
                        // Suppress historical messages and avoid notifying twice for the same ID.
                        fresh.filter { it.readAt == null }.take(12).forEach { notice ->
                            LiveNotifications.show(this@BackgroundLiveService, notice)
                        }
                    }
                )
            } catch (stop: CancellationException) {
                throw stop
            } catch (unauthorized: ApiError) {
                if (unauthorized.code == 401 || unauthorized.code == 403) {
                    rejectedSessions[id] = sessionFingerprint(server.token)
                    ConnectionDiagnostics.loginRequired(this, id)
                    break
                }
                val backoff = (1000L shl (failures + 1).coerceAtMost(5))
                    .coerceAtMost(30000L)
                ConnectionDiagnostics.retrying(this, id, backoff, deliveryErrorLabel(unauthorized))
            } catch (error: Exception) {
                val backoff = (1000L shl (failures + 1).coerceAtMost(5))
                    .coerceAtMost(30000L)
                ConnectionDiagnostics.retrying(this, id, backoff, deliveryErrorLabel(error))
            }
            failures++
            delay((1000L shl failures.coerceAtMost(5)).coerceAtMost(30000L))
        }
    }

    override fun onDestroy() {
        running = false
        streams.disconnectAll()
        scope.cancel()
        jobs.clear()
        cache.servers().forEach { ConnectionDiagnostics.stopped(this, it.id) }
        ChangeBus.emit()
        super.onDestroy()
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        // Leave service running when the task is removed. OS may still terminate it.
        super.onTaskRemoved(rootIntent)
    }
}
