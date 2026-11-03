package top.mirv.chimeport

import android.content.Context
import android.content.SharedPreferences
import java.security.MessageDigest

/** Shared readable preferences for Activity and background notification delivery. */
object QuietSchedules {
    private const val FILE_NAME = "chimeport_quiet_hours"
    private fun prefs(context: Context): SharedPreferences =
        context.getSharedPreferences(FILE_NAME, Context.MODE_PRIVATE)

    private fun hash(text: String): String =
        MessageDigest.getInstance("SHA-256")
            .digest(text.toByteArray(Charsets.UTF_8))
            .take(12)
            .joinToString("") { "%02x".format(it.toInt() and 255) }

    private fun globalPrefix() = "global."
    private fun serverPrefix(serverId: String) = "server." + hash(serverId) + "."
    private fun channelPrefix(serverId: String, channel: String) =
        "channel." + hash(serverId + "::" + channel) + "."

    private fun read(shared: SharedPreferences, prefix: String, defaultMode: QuietMode): QuietRule {
        val mode = runCatching {
            QuietMode.valueOf(shared.getString(prefix + "mode", defaultMode.name)!!)
        }.getOrDefault(defaultMode)
        return QuietRule(
            mode = mode,
            startMinutes = shared.getInt(prefix + "start", 22 * 60).coerceIn(0, 1439),
            endMinutes = shared.getInt(prefix + "end", 7 * 60).coerceIn(0, 1439),
            allowUrgent = shared.getBoolean(prefix + "urgent", false)
        )
    }

    fun global(context: Context): QuietRule =
        read(prefs(context), globalPrefix(), QuietMode.OFF)

    fun server(context: Context, serverId: String): QuietRule =
        read(prefs(context), serverPrefix(serverId), QuietMode.INHERIT)

    fun channel(context: Context, serverId: String, slug: String): QuietRule =
        read(prefs(context), channelPrefix(serverId, slug), QuietMode.INHERIT)

    fun effective(context: Context, serverId: String, slug: String): EffectiveQuietRule =
        resolveQuietRule(global(context), server(context, serverId), channel(context, serverId, slug))

    private fun write(context: Context, prefix: String, rule: QuietRule) {
        check(prefs(context).edit()
            .putString(prefix + "mode", rule.mode.name)
            .putInt(prefix + "start", rule.startMinutes)
            .putInt(prefix + "end", rule.endMinutes)
            .putBoolean(prefix + "urgent", rule.allowUrgent)
            .commit()) { "Could not save quiet hours" }
    }

    @Synchronized fun setGlobal(context: Context, rule: QuietRule) =
        write(context, globalPrefix(), rule.copy(
            mode = if (rule.mode == QuietMode.INHERIT) QuietMode.OFF else rule.mode
        ))

    @Synchronized fun setServer(context: Context, serverId: String, rule: QuietRule) =
        write(context, serverPrefix(serverId), rule)

    @Synchronized fun setChannel(context: Context, serverId: String, slug: String, rule: QuietRule) =
        write(context, channelPrefix(serverId, slug), rule)

    fun forgetServer(context: Context, serverId: String, slugs: Collection<String>) {
        val edit = prefs(context).edit()
        fun clear(prefix: String) {
            for (suffix in listOf("mode", "start", "end", "urgent")) {
                edit.remove(prefix + suffix)
            }
        }
        clear(serverPrefix(serverId))
        slugs.forEach { clear(channelPrefix(serverId, it)) }
        edit.apply()
    }
}
