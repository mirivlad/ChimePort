package top.mirv.chimeport.data

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import java.util.UUID

/**
 * App-private offline cache. The primary key is local server-connection ID + message ID:
 * equal message IDs on different ChimePort instances cannot collide.
 */
class Cache(context: Context) : SQLiteOpenHelper(context, "chimeport.sqlite", null, 2) {
    private val settings = context.getSharedPreferences("chimeport_preferences", Context.MODE_PRIVATE)

    override fun onConfigure(db: SQLiteDatabase) {
        db.setForeignKeyConstraintsEnabled(true)
    }

    override fun onCreate(db: SQLiteDatabase) {
        db.execSQL("""
            CREATE TABLE accounts (
              id TEXT PRIMARY KEY, origin TEXT NOT NULL, instance_id TEXT NOT NULL,
              label TEXT NOT NULL, username TEXT NOT NULL, user_id INTEGER NOT NULL,
              encrypted_token TEXT NOT NULL, cursor TEXT NOT NULL DEFAULT '',
              last_sync INTEGER NOT NULL DEFAULT 0,
              provider_origin TEXT NOT NULL DEFAULT '',
              UNIQUE(origin, instance_id, user_id)
            )
        """.trimIndent())
        db.execSQL("""
            CREATE TABLE channels (
              server_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
              channel_id INTEGER NOT NULL, slug TEXT NOT NULL, name TEXT NOT NULL,
              PRIMARY KEY(server_id, slug)
            )
        """.trimIndent())
        db.execSQL("""
            CREATE TABLE messages (
              server_id TEXT NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
              id TEXT NOT NULL, channel_slug TEXT NOT NULL, source TEXT NOT NULL,
              title TEXT NOT NULL, body TEXT NOT NULL, priority TEXT NOT NULL,
              url TEXT NOT NULL, created_at TEXT NOT NULL, read_at TEXT,
              PRIMARY KEY(server_id, id)
            )
        """.trimIndent())
        db.execSQL("CREATE INDEX messages_newest ON messages(created_at DESC)")
        db.execSQL("CREATE INDEX messages_channel ON messages(server_id,channel_slug)")
    }

    override fun onUpgrade(db: SQLiteDatabase, oldVersion: Int, newVersion: Int) {
        if (oldVersion < 2) {
            db.execSQL("ALTER TABLE accounts ADD COLUMN provider_origin TEXT NOT NULL DEFAULT ''")
        }
    }

    fun theme(): ThemeChoice = runCatching {
        ThemeChoice.valueOf(settings.getString("theme", "SYSTEM") ?: "SYSTEM")
    }.getOrDefault(ThemeChoice.SYSTEM)

    fun setTheme(theme: ThemeChoice) {
        settings.edit().putString("theme", theme.name).apply()
    }

    @Synchronized
    fun servers(): List<ServerAccount> {
        val result = mutableListOf<ServerAccount>()
        readableDatabase.rawQuery(
            "SELECT id,origin,instance_id,label,username,user_id,encrypted_token,cursor,last_sync,provider_origin FROM accounts ORDER BY label COLLATE NOCASE",
            null
        ).use { c ->
            while (c.moveToNext()) {
                val token = runCatching { Crypto.decrypt(c.getString(6)) }.getOrDefault("")
                result += ServerAccount(
                    c.getString(0), c.getString(1), c.getString(2), c.getString(3),
                    c.getString(4), c.getLong(5), token, c.getString(7), c.getLong(8),
                    c.getString(9), c.getString(10), c.getString(9)
                )
            }
        }
        return result
    }

    @Synchronized
    fun upsertAccount(origin: String, instance: InstanceInfo, session: SessionInfo, label: String, providerOrigin: String = ""): ServerAccount {
        val existing = servers().firstOrNull {
            it.origin == origin && it.instanceId == instance.id && it.userId == session.userId &&
                it.providerOrigin == providerOrigin
        }
        val id = existing?.id ?: UUID.randomUUID().toString()
        val values = ContentValues().apply {
            put("id", id)
            put("origin", origin)
            put("instance_id", instance.id)
            put("label", label.trim().ifEmpty { instance.name })
            put("username", session.username)
            put("user_id", session.userId)
            put("encrypted_token", Crypto.encrypt(session.token))
            put("cursor", existing?.cursor ?: "")
            put("last_sync", existing?.lastSync ?: 0L)
            put("kind", kind)
            put("issuer_origin", issuerOrigin)
            put("provider_origin", providerOrigin)
        }
        if (existing == null) writableDatabase.insertOrThrow("accounts", null, values)
        else writableDatabase.update("accounts", values, "id=?", arrayOf(id))
        return servers().first { it.id == id }
    }

    @Synchronized
    fun deleteAccount(id: String) {
        writableDatabase.delete("accounts", "id=?", arrayOf(id))
    }

    @Synchronized
    fun renameAccount(id: String, label: String) {
        if (label.isBlank()) return
        writableDatabase.execSQL("UPDATE accounts SET label=? WHERE id=?", arrayOf(label.trim(), id))
    }

    @Synchronized
    fun replaceChannels(id: String, channels: List<ChannelInfo>) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            db.delete("channels", "server_id=?", arrayOf(id))
            for (ch in channels) {
                db.insertOrThrow("channels", null, ContentValues().apply {
                    put("server_id", id); put("channel_id", ch.id)
                    put("slug", ch.slug); put("name", ch.name)
                })
            }
            db.setTransactionSuccessful()
        } finally { db.endTransaction() }
    }

    /**
     * After a successful full re-sync, discard only local records whose channels
     * no longer exist on the server. Current-channel history and read state stay intact.
     */
    @Synchronized
    fun removeMessagesFromDeletedChannels(serverId: String, liveSlugs: Set<String>) {
        if (liveSlugs.isEmpty()) return
        val placeholders = liveSlugs.joinToString(",") { "?" }
        val args = (listOf(serverId) + liveSlugs.toList()).toTypedArray()
        writableDatabase.delete(
            "messages", "server_id=? AND channel_slug NOT IN ($placeholders)", args
        )
    }

    @Synchronized
    fun applyPage(serverId: String, page: Page) {
        val db = writableDatabase
        db.beginTransaction()
        try {
            for (notice in page.items) {
                // Preserve a locally marked read state if a stale response arrives.
                val existingRead = db.rawQuery(
                    "SELECT read_at FROM messages WHERE server_id=? AND id=?",
                    arrayOf(serverId, notice.id)
                ).use { c -> if (c.moveToFirst()) c.getString(0) else null }
                val values = ContentValues().apply {
                    put("server_id", serverId); put("id", notice.id)
                    put("channel_slug", notice.channel); put("source", notice.source)
                    put("title", notice.title); put("body", notice.body)
                    put("priority", notice.priority); put("url", notice.url)
                    put("created_at", notice.createdAt); put("read_at", existingRead ?: notice.readAt)
                }
                db.insertWithOnConflict("messages", null, values, SQLiteDatabase.CONFLICT_REPLACE)
            }
            // Commit cursor and messages together to avoid data loss on interruption.
            db.execSQL("UPDATE accounts SET cursor=? WHERE id=?", arrayOf(page.nextCursor, serverId))
            db.setTransactionSuccessful()
        } finally { db.endTransaction() }
    }

    @Synchronized
    fun markSynced(id: String) {
        writableDatabase.execSQL("UPDATE accounts SET last_sync=? WHERE id=?", arrayOf(System.currentTimeMillis(), id))
    }

    @Synchronized
    fun markRead(serverId: String, id: String) {
        writableDatabase.execSQL(
            "UPDATE messages SET read_at=? WHERE server_id=? AND id=?",
            arrayOf(java.time.Instant.now().toString(), serverId, id)
        )
    }

    @Synchronized
    fun markUnread(serverId: String, id: String) {
        writableDatabase.execSQL(
            "UPDATE messages SET read_at=NULL WHERE server_id=? AND id=?",
            arrayOf(serverId, id)
        )
    }

    @Synchronized
    fun channels(): List<ChannelInfo> {
        val result = mutableListOf<ChannelInfo>()
        readableDatabase.rawQuery("""
            SELECT c.server_id,s.label,c.channel_id,c.slug,c.name,
                (SELECT COUNT(*) FROM messages m
                 WHERE m.server_id=c.server_id AND m.channel_slug=c.slug AND m.read_at IS NULL)
            FROM channels c JOIN accounts s ON s.id=c.server_id ORDER BY s.label,c.name
        """.trimIndent(), null).use { c ->
            while (c.moveToNext()) result += ChannelInfo(
                c.getString(0), c.getString(1), c.getLong(2),
                c.getString(3), c.getString(4), c.getInt(5)
            )
        }
        return result
    }

    @Synchronized
    fun messages(): List<Notice> {
        val result = mutableListOf<Notice>()
        readableDatabase.rawQuery("""
            SELECT m.id,m.server_id,s.label,m.channel_slug,
              COALESCE(c.name,m.channel_slug),m.source,m.title,m.body,
              m.priority,m.url,m.created_at,m.read_at
            FROM messages m
            JOIN accounts s ON s.id=m.server_id
            LEFT JOIN channels c ON c.server_id=m.server_id AND c.slug=m.channel_slug
            ORDER BY m.created_at DESC,m.server_id,m.id DESC LIMIT 5000
        """.trimIndent(), null).use { c ->
            while (c.moveToNext()) result += Notice(
                id = c.getString(0), serverId = c.getString(1),
                serverLabel = c.getString(2), channel = c.getString(3),
                channelName = c.getString(4), source = c.getString(5),
                title = c.getString(6), body = c.getString(7),
                priority = c.getString(8), url = c.getString(9),
                createdAt = c.getString(10), readAt = c.getString(11)
            )
        }
        return result
    }
}
