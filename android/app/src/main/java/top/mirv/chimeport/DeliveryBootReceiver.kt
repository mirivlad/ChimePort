package top.mirv.chimeport

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * BOOT_COMPLETED is delivered after user unlock; credentials live in standard
 * credential-protected app storage and must NOT be opened at LOCKED_BOOT_COMPLETED.
 * Android can restrict background starts or OEM battery settings may block delivery.
 */
class DeliveryBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val action = intent.action
        if (action != Intent.ACTION_BOOT_COMPLETED &&
            action != Intent.ACTION_MY_PACKAGE_REPLACED) return
        if (!BackgroundLiveService.enabled(context)) {
            ConnectionDiagnostics.bootResult(context, "Фоновая доставка выключена")
            return
        }
        try {
            context.startForegroundService(
                Intent(context, BackgroundLiveService::class.java)
            )
            ConnectionDiagnostics.bootResult(
                context, if (action == Intent.ACTION_BOOT_COMPLETED)
                    "Запуск после перезагрузки запрошен"
                else "Перезапуск после обновления запрошен"
            )
        } catch (_: SecurityException) {
            ConnectionDiagnostics.bootResult(context, "Нет разрешения на фоновый сервис")
        } catch (_: IllegalStateException) {
            // Includes ForegroundServiceStartNotAllowedException (Android 12+).
            ConnectionDiagnostics.bootResult(context, "Android ограничил запуск из фона")
        }
    }
}
