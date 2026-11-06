package top.mirv.chimeport.data

data class ServerAccount(
    val id: String,
    val origin: String,
    val instanceId: String,
    val label: String,
    val username: String,
    val userId: Long,
    val token: String,
    val cursor: String,
    val lastSync: Long,
    // Empty for direct ChimePort accounts; TMS origin for delegated accounts.
    val providerOrigin: String = ""
)

data class ChannelInfo(
    val serverId: String,
    val serverLabel: String,
    val id: Long,
    val slug: String,
    val name: String,
    val unread: Int
)

data class Notice(
    val id: String,
    val serverId: String,
    val serverLabel: String,
    val channel: String,
    val channelName: String,
    val source: String,
    val title: String,
    val body: String,
    val priority: String,
    val url: String,
    val createdAt: String,
    val readAt: String?
) {
    val key: String get() = "$serverId:$id"
}

data class InstanceInfo(
    val id: String,
    val name: String,
    val providerOrigin: String = "",
    val serviceOrigin: String = ""
)
data class SessionInfo(val token: String, val username: String, val userId: Long)
data class Page(val items: List<Notice>, val nextCursor: String, val hasMore: Boolean)

enum class Tab { INBOX, SEARCH, SERVERS, SETTINGS }
enum class InboxMode { UNREAD, ALL, CHANNEL }
enum class ReadFilter { ALL, UNREAD, READ }
enum class LiveStatus { CONNECTING, CONNECTED, RECONNECTING, LOGIN_REQUIRED }
enum class ThemeChoice { SYSTEM, LIGHT, DARK }

data class AppState(
    val servers: List<ServerAccount> = emptyList(),
    val channels: List<ChannelInfo> = emptyList(),
    val messages: List<Notice> = emptyList(),
    val busy: Boolean = false,
    val checking: Boolean = false,
    val probed: InstanceInfo? = null,
    val probedOrigin: String = "",
    val probedKind: String = "chimeport",
    val probedDelivery: String = "",
    val loginError: String? = null,
    val syncError: String? = null,
    val message: String? = null,
    val tab: Tab = Tab.INBOX,
    val selectedServer: String? = null,
    val selectedChannel: String? = null,
    val inboxMode: InboxMode = InboxMode.UNREAD,
    val search: String = "",
    val searchServer: String? = null,
    val searchChannel: String? = null,
    val searchRead: ReadFilter = ReadFilter.ALL,
    val searchPriority: String? = null,
    val addingServer: Boolean = false,
    val theme: ThemeChoice = ThemeChoice.SYSTEM,
    val backgroundLive: Boolean = false,
    val liveStatuses: Map<String, LiveStatus> = emptyMap(),
    val alertSettingsRevision: Int = 0,
    val quietSettingsRevision: Int = 0,
    val openNotificationKey: String? = null
)
