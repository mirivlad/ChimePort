package top.mirv.chimeport

enum class AlertSound(val label: String) {
    SYSTEM("Системный"), DEVICE("Из звуков телефона"), CHIME("Мягкий звон"), SIGNAL("Тройной сигнал"), SILENT("Без звука")
}

enum class AlertLevel(val label: String) {
    AUTO("По важности сообщения"), IMPORTANT("Всегда важные"), QUIET("Тихие")
}

data class AlertProfile(
    val sound: AlertSound = AlertSound.SYSTEM,
    val vibrate: Boolean = false,
    val level: AlertLevel = AlertLevel.AUTO,
    val ringtoneUri: String = "",
    val ringtoneTitle: String = "",
    val revision: Int = 0
)

enum class AlertLane { HIGH, NORMAL, QUIET }

fun alertLane(priority: String, level: AlertLevel): AlertLane = when (level) {
    AlertLevel.IMPORTANT -> AlertLane.HIGH
    AlertLevel.QUIET -> AlertLane.QUIET
    AlertLevel.AUTO -> when (priority.lowercase()) {
        "urgent", "high" -> AlertLane.HIGH
        "low" -> AlertLane.QUIET
        else -> AlertLane.NORMAL
    }
}

fun nextAlertProfile(previous: AlertProfile, proposed: AlertProfile): AlertProfile {
    val changed = previous.sound != proposed.sound ||
        previous.vibrate != proposed.vibrate || previous.level != proposed.level ||
        (previous.sound == AlertSound.DEVICE && proposed.sound == AlertSound.DEVICE &&
            previous.ringtoneUri != proposed.ringtoneUri)
    return proposed.copy(revision = if (changed) previous.revision + 1 else previous.revision)
}
