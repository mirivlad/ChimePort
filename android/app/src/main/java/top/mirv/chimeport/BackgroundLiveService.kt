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
    private var manager: Job? = null

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
        if (manager?.isActive != true) {
            manager = scope.launch {
                while (isActive && enabled(this@BackgroundLiveService)) {
                    val accounts = cache.servers().map { it.id }
                    val ids = accounts.toSet()
                    jobs.keys.toList().filter { it !in ids }.forEach { id ->
                        jobs.remove(id)?.cancel()
                        streams.disconnect(id)
                    }
                    for (id in accounts) {
                        if (jobs[id]?.isActive != true) {
                            jobs[id] = launch { observe(id) }
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
            if (server.token.isBlank()) break
            try {
                streams.listen(server,
                    onConnected = {
                        failures = 0
                        // Silent catch-up after a restart or lost connection.
                        ServerSync.refresh(cache, id)
                        ChangeBus.emit()
                    },
                    onMessage = { lastId ->
                        val fresh = ServerSync.refresh(cache, id)
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
                if (unauthorized.code == 401 || unauthorized.code == 403) break
            } catch (_: Exception) {
                // Keep attempting connections while the user-enabled service is alive.
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
        ChangeBus.emit()
        super.onDestroy()
    }

    override fun onTaskRemoved(rootIntent: Intent?) {
        // Leave service running when the task is removed. OS may still terminate it.
        super.onTaskRemoved(rootIntent)
    }
}
