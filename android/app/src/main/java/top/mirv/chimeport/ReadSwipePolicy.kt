package top.mirv.chimeport

internal enum class ReadSwipeDirection { LEFT, RIGHT }

/** The action must match the notification's CURRENT read state. */
internal fun readStateAfterSwipe(isRead: Boolean, direction: ReadSwipeDirection): Boolean? =
    when {
        !isRead && direction == ReadSwipeDirection.LEFT -> true
        isRead && direction == ReadSwipeDirection.RIGHT -> false
        else -> null
    }
