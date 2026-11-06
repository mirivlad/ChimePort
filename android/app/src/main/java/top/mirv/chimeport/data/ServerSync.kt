package top.mirv.chimeport.data

import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import java.io.IOException
import java.util.concurrent.ConcurrentHashMap

/** Shared account lock for UI and background service. REST cursor is the source of truth. */
object ServerSync {
    private val locks = ConcurrentHashMap<String, Mutex>()

    suspend fun refresh(cache: Cache, serverId: String): List<Notice> = withContext(Dispatchers.IO) {
        locks.getOrPut(serverId) { Mutex() }.withLock {
            val server = cache.servers().firstOrNull { it.id == serverId }
                ?: return@withLock emptyList()
            if (server.token.isEmpty()) throw IOException("Нужен повторный вход в " + server.label)
            if (Api.actualInstanceId(server) != server.instanceId) {
                throw IOException("Идентификатор сервера изменился; история не объединена.")
            }
            val known = cache.messages().asSequence()
                .filter { it.serverId == server.id }.map { it.id }.toMutableSet()
            val added = mutableListOf<Notice>()
            val liveChannels = Api.channels(server)
            val liveSlugs = liveChannels.map { it.slug }.toSet()
            val cachedSlugs = cache.messages().asSequence()
                .filter { it.serverId == server.id }.map { it.channel }.toSet()
            val needsReplay = ChannelCachePolicy.needsReplay(cachedSlugs, liveSlugs)
            cache.replaceChannels(server.id, liveChannels)
            // A channel can be renamed/consolidated server-side without changing
            // message IDs. Replay the history so cached old slugs get updated.
            var cursor = if (needsReplay) "" else server.cursor
            var pages = 0
            do {
                val page = Api.messages(server, cursor)
                page.items.forEach { if (known.add(it.id)) added.add(it) }
                // Messages and cursor are committed atomically by Cache.applyPage().
                cache.applyPage(server.id, page)
                if (page.hasMore && page.nextCursor == cursor) {
                    throw IOException("Сервер вернул повторный курсор.")
                }
                cursor = page.nextCursor
                pages++
                if (!page.hasMore) break
            } while (pages < 500)
            if (pages >= 500) throw IOException("История слишком велика для одного прохода.")
            // Only after the complete replay: never discard cached history on errors.
            if (needsReplay) cache.removeMessagesFromDeletedChannels(server.id, liveSlugs)
            cache.markSynced(server.id)
            added
        }
    }
}
