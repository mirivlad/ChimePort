package top.mirv.chimeport

import org.junit.Assert.assertEquals
import org.junit.Test

class DeliveryDiagnosticsPolicyTest {
    @Test fun clearRecoveryGuidance() {
        assertEquals("Включите фоновую доставку в настройках",
            recommendedDeliveryAction(false, false, 1))
        assertEquals("Подключите сервер и войдите",
            recommendedDeliveryAction(true, true, 0))
        assertEquals("Откройте приложение и возобновите подключение",
            recommendedDeliveryAction(true, false, 2))
        assertEquals("Проверяйте состояние каждого сервера ниже",
            recommendedDeliveryAction(true, true, 1))
    }
}
