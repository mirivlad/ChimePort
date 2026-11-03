package top.mirv.chimeport

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationChannelGroup
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.media.AudioAttributes
import android.media.RingtoneManager
import android.net.Uri
import android.os.Build
import java.time.LocalTime
import top.mirv.chimeport.data.Notice

/**
 * New *live* events are grouped by account and routed to per-channel Android
 * NotificationChannels. Sound/importance/vibration changes create a new revision:
 * Android deliberately does not allow mutation of an existing channel's behavior.
 */
object LiveNotifications {
    private const val MESSAGE_ID = 4001
    private const val SUMMARY_ID = 4002
    private const val SUMMARY_CHANNEL = "cp.summary.silent.v1"

    fun ensureChannel(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(
                SUMMARY_CHANNEL, "Группы уведомлений ChimePort", NotificationManager.IMPORTANCE_MIN
            ).apply {
                description = "Беззвучная группировка уведомлений по серверам"
                setSound(null, null)
                enableVibration(false)
                setShowBadge(false)
            }
        )
    }

    private fun eventChannel(context: Context, notice: Notice): String {
        val manager = context.getSystemService(NotificationManager::class.java)
        val profile = AlertProfiles.get(context, notice.serverId, notice.channel)
        // Evaluate wall-clock time on every event, including after time zone changes.
        // The dedicated quiet lane has neither sound nor vibration.
        val localTime = LocalTime.now()
        val minuteOfDay = localTime.hour * 60 + localTime.minute
        val quiet = shouldQuietNotification(
            notice.priority,
            QuietSchedules.effective(context, notice.serverId, notice.channel),
            minuteOfDay
        )
        val lane = if (quiet) AlertLane.QUIET else alertLane(notice.priority, profile.level)
        val osImportance = when (lane) {
            AlertLane.HIGH -> NotificationManager.IMPORTANCE_HIGH
            AlertLane.NORMAL -> NotificationManager.IMPORTANCE_DEFAULT
            AlertLane.QUIET -> NotificationManager.IMPORTANCE_LOW
        }
        val serverGroupId = AlertProfiles.groupId(notice.serverId)
        manager.createNotificationChannelGroup(NotificationChannelGroup(serverGroupId, notice.serverLabel))
        val channelId = AlertProfiles.channelId(
            notice.serverId, notice.channel, profile.revision, lane
        )
        // Existing OS channels, including user changes in Android Settings, remain untouched.
        if (manager.getNotificationChannel(channelId) == null) {
            val laneLabel = when (lane) {
                AlertLane.HIGH -> "важные"
                AlertLane.NORMAL -> "обычные"
                AlertLane.QUIET -> "тихие"
            }
            val channel = NotificationChannel(
                channelId, notice.channelName + " · " + laneLabel, osImportance
            ).apply {
                description = "Сервер " + notice.serverLabel + " · " + notice.channelName
                this.group = serverGroupId
                enableVibration(profile.vibrate && lane != AlertLane.QUIET)
                val sound: Uri? = if (lane == AlertLane.QUIET) null else when (profile.sound) {
                    AlertSound.SYSTEM -> RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
                    AlertSound.DEVICE -> profile.ringtoneUri.takeIf { it.isNotBlank() }
                        ?.let(Uri::parse)
                        ?: RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
                    AlertSound.CHIME -> Uri.parse(
                        "android.resource://" + context.packageName + "/raw/chime_soft"
                    )
                    AlertSound.SIGNAL -> Uri.parse(
                        "android.resource://" + context.packageName + "/raw/signal_pulse"
                    )
                    AlertSound.SILENT -> null
                }
                if (sound == null) {
                    setSound(null, null)
                } else {
                    val attributes = AudioAttributes.Builder()
                        .setUsage(AudioAttributes.USAGE_NOTIFICATION)
                        .setContentType(AudioAttributes.CONTENT_TYPE_SONIFICATION)
                        .build()
                    setSound(sound, attributes)
                }
            }
            manager.createNotificationChannel(channel)
        }
        return channelId
    }

    private fun deepLink(context: Context, notice: Notice): PendingIntent {
        val intent = Intent(context, MainActivity::class.java).apply {
            action = Intent.ACTION_VIEW
            data = Uri.Builder().scheme("chimeport").authority("notice")
                .appendPath(notice.serverId).appendPath(notice.id).build()
            flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
        }
        // The data URI differentiates each message even when requestCode is shared.
        return PendingIntent.getActivity(
            context, 0, intent, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
    }

    fun show(context: Context, notice: Notice) {
        if (notice.readAt != null || (Build.VERSION.SDK_INT >= 33 &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED)
        ) return
        ensureChannel(context)
        val id = eventChannel(context, notice)
        val manager = context.getSystemService(NotificationManager::class.java)
        val body = notice.body.ifBlank { notice.channelName }
        val notification = Notification.Builder(context, id)
            .setSmallIcon(R.drawable.chimeport_notification)
            .setContentTitle(notice.title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setSubText(notice.serverLabel + " · " + notice.channelName)
            .setContentIntent(deepLink(context, notice))
            .setGroup(AlertProfiles.groupId(notice.serverId))
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_EVENT)
            .setShowWhen(true)
            .setWhen(runCatching {
                java.time.Instant.parse(notice.createdAt).toEpochMilli()
            }.getOrDefault(System.currentTimeMillis()))
            .build()
        manager.notify(AlertProfiles.notificationTag(notice.serverId, notice.id), MESSAGE_ID, notification)
        updateSummary(context, notice.serverId, notice.serverLabel)
    }

    private fun updateSummary(context: Context, serverId: String, serverLabel: String) {
        val manager = context.getSystemService(NotificationManager::class.java)
        val count = manager.activeNotifications.count {
            it.tag?.startsWith("cp.msg." + serverId + ".") == true
        }
        val tag = AlertProfiles.summaryTag(serverId)
        if (count < 2) {
            manager.cancel(tag, SUMMARY_ID)
            return
        }
        val open = PendingIntent.getActivity(
            context, serverId.hashCode(),
            Intent(context, MainActivity::class.java).apply {
                flags = Intent.FLAG_ACTIVITY_CLEAR_TOP or Intent.FLAG_ACTIVITY_SINGLE_TOP
            },
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val summary = Notification.Builder(context, SUMMARY_CHANNEL)
            .setSmallIcon(R.drawable.chimeport_notification)
            .setContentTitle(serverLabel)
            .setContentText(count.toString() + " уведомлений ChimePort")
            .setContentIntent(open)
            .setGroup(AlertProfiles.groupId(serverId))
            .setGroupSummary(true)
            .setGroupAlertBehavior(Notification.GROUP_ALERT_CHILDREN)
            .setOnlyAlertOnce(true)
            .build()
        manager.notify(tag, SUMMARY_ID, summary)
    }

    fun dismiss(context: Context, notice: Notice) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.cancel(AlertProfiles.notificationTag(notice.serverId, notice.id), MESSAGE_ID)
        updateSummary(context, notice.serverId, notice.serverLabel)
    }

    fun forgetServer(context: Context, serverId: String) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.activeNotifications.filter {
            it.tag?.startsWith("cp.msg." + serverId + ".") == true
        }.forEach { manager.cancel(it.tag, it.id) }
        manager.cancel(AlertProfiles.summaryTag(serverId), SUMMARY_ID)
        manager.deleteNotificationChannelGroup(AlertProfiles.groupId(serverId))
    }
}
