package top.mirv.chimeport

import android.content.Context
import android.content.SharedPreferences
import java.security.MessageDigest

/**
 * App-local profiles are scoped by local connection ID and channel slug;
 * they contain no credentials and are shared by Activity and delivery service.
 */
object AlertProfiles {
    private const val PREFS = "chimeport_alert_profiles"

    private fun store(context: Context): SharedPreferences =
        context.getSharedPreferences(PREFS, Context.MODE_PRIVATE)

    internal fun channelIdentity(serverId: String, slug: String): String =
        serverId + "::" + slug

    internal fun digest(value: String): String =
        MessageDigest.getInstance("SHA-256").digest(value.toByteArray(Charsets.UTF_8))
            .take(12).joinToString("") { "%02x".format(it.toInt() and 0xff) }

    private fun prefix(serverId: String, slug: String): String =
        "profile." + digest(channelIdentity(serverId, slug)) + "."

    fun get(context: Context, serverId: String, slug: String): AlertProfile {
        val shared = store(context)
        val prefix = prefix(serverId, slug)
        val sound = runCatching {
            AlertSound.valueOf(shared.getString(prefix + "sound", null) ?: AlertSound.SYSTEM.name)
        }.getOrDefault(AlertSound.SYSTEM)
        val level = runCatching {
            AlertLevel.valueOf(shared.getString(prefix + "level", null) ?: AlertLevel.AUTO.name)
        }.getOrDefault(AlertLevel.AUTO)
        return AlertProfile(
            sound = sound, level = level,
            vibrate = shared.getBoolean(prefix + "vibrate", false),
            ringtoneUri = shared.getString(prefix + "ringtoneUri", null) ?: "",
            ringtoneTitle = shared.getString(prefix + "ringtoneTitle", null) ?: "",
            revision = shared.getInt(prefix + "revision", 0)
        )
    }

    @Synchronized
    fun save(context: Context, serverId: String, slug: String, proposed: AlertProfile): AlertProfile {
        val previous = get(context, serverId, slug)
        val updated = nextAlertProfile(previous, proposed)
        if (previous == updated) return updated
        val prefix = prefix(serverId, slug)
        check(store(context).edit()
            .putString(prefix + "sound", updated.sound.name)
            .putString(prefix + "level", updated.level.name)
            .putBoolean(prefix + "vibrate", updated.vibrate)
            .putString(prefix + "ringtoneUri", updated.ringtoneUri)
            .putString(prefix + "ringtoneTitle", updated.ringtoneTitle)
            .putInt(prefix + "revision", updated.revision)
            .commit()) { "Не удалось сохранить настройки уведомлений" }
        return updated
    }

    fun forgetServer(context: Context, serverId: String, slugs: Collection<String>) {
        val editor = store(context).edit()
        for (slug in slugs) {
            val prefix = prefix(serverId, slug)
            for (suffix in listOf("sound", "level", "vibrate", "ringtoneUri", "ringtoneTitle", "revision")) {
                editor.remove(prefix + suffix)
            }
        }
        editor.apply()
    }

    fun channelId(serverId: String, slug: String, revision: Int, lane: AlertLane): String =
        "cp.alert." + digest(channelIdentity(serverId, slug)) + ".v" + revision +
            "." + lane.name.lowercase()

    fun groupId(serverId: String): String = "cp.group." + digest(serverId)
    fun notificationTag(serverId: String, id: String): String = "cp.msg." + serverId + "." + id
    fun summaryTag(serverId: String): String = "cp.summary." + serverId
}
