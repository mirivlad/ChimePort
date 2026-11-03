package top.mirv.chimeport

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.channels.Channel
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import java.util.concurrent.ConcurrentHashMap
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.withContext
import top.mirv.chimeport.data.*
import java.io.IOException

class ChimeViewModel(application: Application) : AndroidViewModel(application) {
    private val cache = Cache(application)
    private val eventStream = EventStream()
    private val liveJobs = mutableMapOf<String, Job>()
    private val syncLocks = ConcurrentHashMap<String, Mutex>()
    private var foreground = false
    private val mutable = MutableStateFlow(AppState(theme = cache.theme()))
    val state: StateFlow<AppState> = mutable

    init {
        reload()
        if (mutable.value.servers.isNotEmpty()) refresh()
    }

    private fun reload() {
        val servers = cache.servers()
        mutable.update {
            it.copy(servers = servers, channels = cache.channels(), messages = cache.messages(),
                selectedServer = it.selectedServer?.takeIf { id -> servers.any { s -> s.id == id } },
                selectedChannel = it.selectedChannel?.takeIf { key ->
                    cache.channels().any { c -> c.serverId + "::" + c.slug == key }
                })
        }
    }

    /** Lifecycle-owned live streams: never run them as a persistent background service. */
    fun startRealtime() {
        foreground = true
        reconcileRealtime()
    }

    fun stopRealtime() {
        foreground = false
        liveJobs.values.forEach { it.cancel() }
        liveJobs.clear()
        eventStream.disconnectAll()
        mutable.update { it.copy(liveStatuses = emptyMap()) }
    }

    private fun setLiveStatus(id: String, status: LiveStatus) {
        if (!foreground) return
        mutable.update { state ->
            state.copy(liveStatuses = state.liveStatuses + (id to status))
        }
    }

    private fun reconcileRealtime() {
        if (!foreground) return
        val accounts = cache.servers()
        val ids = accounts.map { it.id }.toSet()
        liveJobs.keys.toList().filter { it !in ids }.forEach { id ->
            liveJobs.remove(id)?.cancel()
            eventStream.disconnect(id)
        }
        mutable.update { state -> state.copy(liveStatuses = state.liveStatuses.filterKeys { it in ids }) }
        accounts.forEach { account ->
            if (liveJobs[account.id]?.isActive != true) {
                liveJobs[account.id] = viewModelScope.launch { watchServer(account.id) }
            }
        }
    }

    private fun restartRealtime() {
        if (!foreground) return
        stopRealtime()
        startRealtime()
    }

    private suspend fun syncLocked(account: ServerAccount): List<Notice> = withContext(Dispatchers.IO) {
        syncLocks.getOrPut(account.id) { Mutex() }.withLock {
            // Always use the newest saved cursor rather than the original SSE snapshot.
            val current = cache.servers().firstOrNull { it.id == account.id }
            if (current == null) emptyList()
            else syncOne(current)
        }
    }

    private suspend fun watchServer(id: String) {
        var failures = 0
        while (foreground && currentCoroutineContext().isActive) {
            val account = withContext(Dispatchers.IO) {
                cache.servers().firstOrNull { it.id == id }
            } ?: break
            if (account.token.isBlank()) {
                setLiveStatus(id, LiveStatus.LOGIN_REQUIRED)
                break
            }
            setLiveStatus(id, if (failures == 0) LiveStatus.CONNECTING else LiveStatus.RECONNECTING)
            // Conflation prevents a burst of SSE hints from spawning unbounded refresh jobs.
            // REST cursor sync will retrieve all missed events, not merely the last SSE ID.
            val triggers = Channel<String>(Channel.CONFLATED)
            val worker = viewModelScope.launch {
                for (eventId in triggers) {
                    try {
                        val fresh = syncLocked(account)
                        reload()
                        // An initial connection and catch-up must be silent. Only the live event
                        // that was just signalled can produce a system notification.
                        if (eventId.isNotBlank()) {
                            fresh.firstOrNull { it.id == eventId }?.let { notice ->
                                LiveNotifications.show(getApplication(), notice)
                            }
                        }
                        mutable.update { it.copy(syncError = null) }
                    } catch (cancel: CancellationException) {
                        throw cancel
                    } catch (error: Exception) {
                        mutable.update {
                            it.copy(syncError = account.label + ": " + describe(error))
                        }
                    }
                }
            }
            try {
                eventStream.listen(
                    account,
                    onConnected = {
                        failures = 0
                        setLiveStatus(id, LiveStatus.CONNECTED)
                        // Covers the commit -> subscribe race and any offline backlog.
                        triggers.trySend("")
                    },
                    onMessage = { lastId -> triggers.trySend(lastId) }
                )
            } catch (cancel: CancellationException) {
                throw cancel
            } catch (error: ApiError) {
                if (error.code == 401 || error.code == 403) {
                    setLiveStatus(id, LiveStatus.LOGIN_REQUIRED)
                    break
                }
                setLiveStatus(id, LiveStatus.RECONNECTING)
            } catch (_: Exception) {
                setLiveStatus(id, LiveStatus.RECONNECTING)
            } finally {
                triggers.close()
                worker.cancel()
            }
            failures++
            delay((1000L shl failures.coerceAtMost(5)).coerceAtMost(30000L))
        }
    }

    fun openTab(tab: Tab) { mutable.update { it.copy(tab = tab) } }
    fun filterServer(id: String?) { mutable.update { it.copy(selectedServer = id, selectedChannel = null, tab = Tab.INBOX) } }
    fun filterChannel(channel: ChannelInfo?) {
        mutable.update {
            it.copy(selectedChannel = channel?.let { c -> c.serverId + "::" + c.slug },
                selectedServer = channel?.serverId, tab = Tab.INBOX)
        }
    }
    fun unreadOnly(enabled: Boolean) { mutable.update { it.copy(unreadOnly = enabled) } }
    fun search(text: String) { mutable.update { it.copy(search = text) } }
    fun dismissMessage() { mutable.update { it.copy(message = null) } }
    fun openAdd() {
        mutable.update { it.copy(addingServer = true, probed = null, probedOrigin = "", loginError = null) }
    }
    fun cancelAdd() { mutable.update { it.copy(addingServer = false, probed = null, loginError = null) } }
    fun clearProbe() { mutable.update { it.copy(probed = null, loginError = null) } }

    fun theme(choice: ThemeChoice) {
        cache.setTheme(choice)
        mutable.update { it.copy(theme = choice) }
    }

    fun verifyServer(raw: String) {
        mutable.update { it.copy(checking = true, probed = null, loginError = null) }
        viewModelScope.launch {
            val result = runCatching {
                withContext(Dispatchers.IO) {
                    val origin = Api.normalizeOrigin(raw)
                    origin to Api.probe(origin)
                }
            }
            mutable.update { s ->
                result.fold(
                    onSuccess = { (origin, instance) -> s.copy(checking = false, probed = instance, probedOrigin = origin) },
                    onFailure = { e -> s.copy(checking = false, loginError = describe(e)) }
                )
            }
        }
    }

    fun connect(username: String, password: String, label: String) {
        val current = mutable.value
        val instance = current.probed ?: return
        val origin = current.probedOrigin
        if (username.isBlank() || password.isBlank()) {
            mutable.update { it.copy(loginError = "Введите логин и пароль.") }
            return
        }
        mutable.update { it.copy(busy = true, loginError = null) }
        viewModelScope.launch {
            val result = runCatching {
                withContext(Dispatchers.IO) {
                    // Always verify again before handing credentials to this origin.
                    val fresh = Api.probe(origin)
                    if (fresh.id != instance.id) throw IOException("Сервер изменил идентификатор. Проверьте адрес.")
                    val login = Api.login(origin, username, password)
                    val account = cache.upsertAccount(origin, fresh, login,
                        label.ifBlank { origin.removePrefix("https://") })
                    syncOne(account)
                }
            }
            result.onSuccess {
                reload()
                mutable.update { it.copy(busy = false, addingServer = false, probed = null,
                    loginError = null, tab = Tab.INBOX, syncError = null) }
                reconcileRealtime()
            }.onFailure { e ->
                // Bad credentials must keep the login form open. A successful login with a
                // later sync error keeps the account and its local history instead.
                reload()
                val saved = cache.servers().any {
                    it.origin == origin && it.instanceId == instance.id &&
                        it.username == username.trim()
                }
                mutable.update {
                    it.copy(
                        busy = false,
                        addingServer = !saved,
                        loginError = if (saved) null else describe(e),
                        message = if (saved) "Сервер подключён, но синхронизация не завершилась: " + describe(e) else null,
                        tab = if (saved) Tab.SERVERS else it.tab
                    )
                }
            }
        }
    }

    private fun syncOne(server: ServerAccount): List<Notice> {
        val known = cache.messages().asSequence().filter { it.serverId == server.id }.map { it.id }.toMutableSet()
        val added = mutableListOf<Notice>()
        if (server.token.isEmpty()) throw IOException("Нужен повторный вход: локальный ключ сессии недоступен.")
        if (Api.probe(server.origin).id != server.instanceId) {
            throw IOException("Идентичность сервера изменилась. История не объединена — проверьте подключение.")
        }
        cache.replaceChannels(server.id, Api.channels(server))
        var cursor = server.cursor
        var count = 0
        do {
            val page = Api.messages(server, cursor)
            for (notice in page.items) {
                if (known.add(notice.id)) added.add(notice)
            }
            cache.applyPage(server.id, page)
            if (page.hasMore && page.nextCursor == cursor) {
                throw IOException("Сервер вернул повторный курсор синхронизации.")
            }
            cursor = page.nextCursor
            count++
            if (!page.hasMore) break
        } while (count < 500)
        cache.markSynced(server.id)
        if (count >= 500) throw IOException("Большая история: нужна дополнительная синхронизация.")
        return added
    }

    fun refresh() {
        if (mutable.value.busy) return
        val accounts = cache.servers()
        if (accounts.isEmpty()) { reload(); return }
        mutable.update { it.copy(busy = true, syncError = null) }
        viewModelScope.launch {
            val errors = withContext(Dispatchers.IO) {
                accounts.mapNotNull { account ->
                    runCatching { syncLocked(account) }.exceptionOrNull()
                        ?.let { account.label + ": " + describe(it) }
                }
            }
            reload()
            mutable.update { it.copy(busy = false, syncError = errors.takeIf { e -> e.isNotEmpty() }?.joinToString("\n")) }
        }
    }

    fun markRead(notice: Notice) {
        val account = cache.servers().firstOrNull { it.id == notice.serverId } ?: return
        viewModelScope.launch {
            val result = runCatching { withContext(Dispatchers.IO) {
                Api.read(account, notice.id)
                cache.markRead(notice.serverId, notice.id)
            } }
            result.onSuccess { reload() }
                .onFailure { e -> mutable.update { it.copy(message = "Не удалось отметить прочитанным: " + describe(e)) } }
        }
    }

    fun renameServer(id: String, label: String) {
        cache.renameAccount(id, label)
        reload()
    }

    fun removeServer(id: String) {
        val account = cache.servers().firstOrNull { it.id == id }
        // Local removal always works offline; remote logout is a best-effort cleanup.
        cache.deleteAccount(id)
        reload()
        reconcileRealtime()
        mutable.update { it.copy(message = "Подключение удалено с этого устройства.") }
        if (account != null) viewModelScope.launch(Dispatchers.IO) {
            runCatching { Api.logout(account) }
        }
    }

    fun relogin(id: String, password: String) {
        val account = cache.servers().firstOrNull { it.id == id } ?: return
        mutable.update { it.copy(busy = true, message = null) }
        viewModelScope.launch {
            val result = runCatching {
                withContext(Dispatchers.IO) {
                    if (Api.probe(account.origin).id != account.instanceId) {
                        throw IOException("Это другой сервер. Удалите подключение и добавьте новый адрес.")
                    }
                    val session = Api.login(account.origin, account.username, password)
                    if (session.userId != account.userId) throw IOException("Это другая учётная запись.")
                    val updated = cache.upsertAccount(account.origin, InstanceInfo(account.instanceId, "ChimePort"), session, account.label)
                    syncOne(updated)
                }
            }
            reload()
            if (result.isSuccess) restartRealtime()
            mutable.update {
                it.copy(busy = false, message = result.fold(
                    { "Подключение «" + account.label + "» восстановлено." },
                    { e -> "Повторный вход не удался: " + describe(e) }
                ))
            }
        }
    }

    companion object {
        private fun describe(e: Throwable): String =
            e.message?.takeIf { it.isNotBlank() } ?: "Не удалось выполнить запрос."
    }
}
