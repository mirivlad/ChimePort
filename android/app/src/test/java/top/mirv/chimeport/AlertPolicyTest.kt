package top.mirv.chimeport

import org.junit.Assert.*
import org.junit.Test

class AlertPolicyTest {
    @Test fun priorityRoutesToAndroidLane() {
        assertEquals(AlertLane.HIGH, alertLane("urgent", AlertLevel.AUTO))
        assertEquals(AlertLane.HIGH, alertLane("high", AlertLevel.AUTO))
        assertEquals(AlertLane.NORMAL, alertLane("normal", AlertLevel.AUTO))
        assertEquals(AlertLane.QUIET, alertLane("low", AlertLevel.AUTO))
        assertEquals(AlertLane.QUIET, alertLane("urgent", AlertLevel.QUIET))
        assertEquals(AlertLane.HIGH, alertLane("low", AlertLevel.IMPORTANT))
    }

    @Test fun deviceToneRotatesChannelOnlyWhenUriChanges() {
        val picked = AlertProfile(
            sound = AlertSound.DEVICE,
            ringtoneUri = "content://media/internal/audio/media/17",
            ringtoneTitle = "Первый звук"
        )
        val first = nextAlertProfile(AlertProfile(), picked)
        assertEquals(1, first.revision)
        val sameSoundRenamed = nextAlertProfile(
            first, first.copy(ringtoneTitle = "Новое название")
        )
        assertEquals(1, sameSoundRenamed.revision)
        val second = nextAlertProfile(
            sameSoundRenamed,
            sameSoundRenamed.copy(ringtoneUri = "content://media/internal/audio/media/18")
        )
        assertEquals(2, second.revision)
        assertEquals(
            3, nextAlertProfile(second, second.copy(sound = AlertSound.SYSTEM)).revision
        )
    }

    @Test fun onlyActualSettingsChangesRotateChannelId() {
        val original = AlertProfile()
        val same = nextAlertProfile(original, original)
        assertEquals(0, same.revision)
        val different = nextAlertProfile(same, AlertProfile(sound = AlertSound.CHIME))
        assertEquals(1, different.revision)
        val unchanged = nextAlertProfile(different, different.copy(revision = 234))
        assertEquals(1, unchanged.revision)
        assertEquals(2, nextAlertProfile(different, different.copy(vibrate = true)).revision)
    }
}
