package top.mirv.chimeport

/**
 * Notifications contain no credentials. Resolve their connection ID and message ID
 * locally and reject malformed or unrelated links.
 */
internal fun parseNotificationTarget(
    scheme: String?, authority: String?, segments: List<String>
): Pair<String, String>? {
    if (scheme != "chimeport" || authority != "notice" || segments.size != 2) return null
    val serverId = segments[0]
    val messageId = segments[1]
    if (serverId.isBlank() || messageId.isEmpty() ||
        !messageId.all { it.isDigit() }) return null
    return serverId to messageId
}
