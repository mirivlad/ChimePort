package top.mirv.chimeport.data

/** A removed channel may leave stale channel labels in the offline message cache. */
object ChannelCachePolicy {
    fun needsReplay(cachedSlugs: Set<String>, liveSlugs: Set<String>): Boolean =
        liveSlugs.isNotEmpty() && cachedSlugs.any { it !in liveSlugs }
}
