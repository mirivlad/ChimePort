package top.mirv.chimeport

/** Inheritance is available only for server and channel overrides. */
enum class QuietMode(val label: String) {
    INHERIT("Наследовать"),
    OFF("Не использовать тихие часы"),
    SCHEDULE("Своё расписание")
}

data class QuietRule(
    val mode: QuietMode = QuietMode.INHERIT,
    val startMinutes: Int = 22 * 60,
    val endMinutes: Int = 7 * 60,
    val allowUrgent: Boolean = false
) {
    init {
        require(startMinutes in 0..1439 && endMinutes in 0..1439) {
            "Time must be a local minute between 00:00 and 23:59"
        }
    }
}

data class EffectiveQuietRule(val rule: QuietRule, val source: QuietSource)
enum class QuietSource { GLOBAL, SERVER, CHANNEL }

/**
 * Resolve channel -> server -> global independently for each account.
 * "OFF" is an explicit override; it is NOT the same as INHERIT.
 */
fun resolveQuietRule(
    global: QuietRule, server: QuietRule, channel: QuietRule
): EffectiveQuietRule = when {
    channel.mode != QuietMode.INHERIT -> EffectiveQuietRule(channel, QuietSource.CHANNEL)
    server.mode != QuietMode.INHERIT -> EffectiveQuietRule(server, QuietSource.SERVER)
    else -> EffectiveQuietRule(
        if (global.mode == QuietMode.INHERIT) global.copy(mode = QuietMode.OFF) else global,
        QuietSource.GLOBAL
    )
}

/**
 * Local wall clock, start inclusive / end exclusive.
 * Identical times mean 24 hours; a crossing-midnight range works without a timer.
 */
fun isWithinQuietHours(rule: QuietRule, minuteOfDay: Int): Boolean {
    require(minuteOfDay in 0..1439)
    if (rule.mode != QuietMode.SCHEDULE) return false
    val start = rule.startMinutes
    val end = rule.endMinutes
    return when {
        start == end -> true
        start < end -> minuteOfDay >= start && minuteOfDay < end
        else -> minuteOfDay >= start || minuteOfDay < end
    }
}

/** "urgent" is the only critical priority; Android system DND remains authoritative. */
fun shouldQuietNotification(
    priority: String, effective: EffectiveQuietRule, minuteOfDay: Int
): Boolean = isWithinQuietHours(effective.rule, minuteOfDay) &&
    !(effective.rule.allowUrgent && priority.equals("urgent", ignoreCase = true))

fun formatQuietMinute(value: Int): String =
    (value / 60).toString().padStart(2, '0') + ":" +
    (value % 60).toString().padStart(2, '0')
