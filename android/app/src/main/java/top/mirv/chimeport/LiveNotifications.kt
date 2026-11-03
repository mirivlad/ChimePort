package top.mirv.chimeport

import android.Manifest
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import top.mirv.chimeport.data.Notice

/**
 * Best-effort notifications for NEW live SSE events while the application is open.
 * Never notify for historical catch-up or the initial sync; background delivery is a later stage.
 */
object LiveNotifications {
    private const val CHANNEL_ID = "chimeport-foreground-events"

    fun ensureChannel(context: Context) {
        val manager = context.getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel(CHANNEL_ID, "Новые события ChimePort", NotificationManager.IMPORTANCE_DEFAULT).apply {
                description = "События, полученные во время открытого приложения"
            }
        )
    }

    fun show(context: Context, notice: Notice) {
        if (Build.VERSION.SDK_INT >= 33 &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED
        ) return
        ensureChannel(context)
        val launch = Intent(context, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
        }
        val pending = PendingIntent.getActivity(
            context, 0, launch, PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val manager = context.getSystemService(NotificationManager::class.java)
        val body = notice.body.ifBlank { notice.channelName }
        val notification = Notification.Builder(context, CHANNEL_ID)
            .setSmallIcon(R.drawable.chimeport_notification)
            .setContentTitle(notice.title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setSubText(notice.serverLabel + " · " + notice.channelName)
            .setContentIntent(pending)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_STATUS)
            .build()
        manager.notify(notice.key.hashCode(), notification)
    }
}
