package top.mirv.chimeport

internal fun recommendedDeliveryAction(
    enabled: Boolean, serviceRunning: Boolean, loggedInAccounts: Int
): String = when {
    !enabled -> "Включите фоновую доставку в настройках"
    loggedInAccounts <= 0 -> "Подключите сервер и войдите"
    !serviceRunning -> "Откройте приложение и возобновите подключение"
    else -> "Проверяйте состояние каждого сервера ниже"
}
