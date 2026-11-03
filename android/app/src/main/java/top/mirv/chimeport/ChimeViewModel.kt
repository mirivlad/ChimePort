package top.mirv.chimeport

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.currentCoroutineContext
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
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
    private var foreground = false
    private val mutable = MutableStateFlow(AppState(theme = cache.theme(), backgroundLive = BackgroundLiveService.enabled(application)))
    val state: StateFlow<AppState> = mutable

    init {
        reload()
        viewModelScope.launch {
            ChangeBus.changed.collect { reload() }
        }
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

    /** Screen-visible SSE when background service is disabled; user-enabled service owns streams otherwise. */
    fun startRealtime() {
        foreground = true
        if (BackgroundLiveService.enabled(getApplication())) {
            stopOwnStreams()
            if (!BackgroundLiveService.running) {
                runCatching { BackgroundLiveService.enable(getApplication()) }.onFailure { error ->
                    mutable.update { it.copy(message = "Не удалось запустить фоновую доставку: " + describe(error)) }
                }
            }
            return
        }
        reconcileRealtime()
    }

    private fun stopOwnStreams() {
        liveJobs.values.forEach { it.cancel() }
        liveJobs.clear()
        eventStream.disconnectAll()
        mutable.update { it.copy(liveStatuses = emptyMap()) }
    }

    fun stopRealtime() {
        foreground = false
        stopOwnStreams()
    }

    fun setBackgroundLive(enabled: Boolean) {
        if (enabled) {
            try {
                BackgroundLiveService.enable(getApplication())
                stopOwnStreams()
                mutable.update { it.copy(backgroundLive = true, message = null) }
            } catch (error: Exception) {
                BackgroundLiveService.disable(getApplication())
                mutable.update { it.copy(backgroundLive = false,
                    message = "Не удалось включить фоновую доставку: " + describe(error)) }
                if (foreground) reconcileRealtime()
            }
        } else {
            BackgroundLiveService.disable(getApplication())
            mutable.update { it.copy(backgroundLive = false) }
            if (foreground) reconcileRealtime()
        }
    }

    private fun setLiveStatus(id: String, status: LiveStatus) {
        if (!foreground) return
        mutable.update { state ->
            state.copy(liveStatuses = state.liveStatuses + (id to status))
        }
    }

    private fun reconcileRealtime() {
        if (!foreground) return
        if (BackgroundLiveService.enabled(getApplication())) {
            stopOwnStreams()
            if (!BackgroundLiveService.running) {
                runCatching { BackgroundLiveService.enable(getApplication()) }
            }
            return
        }
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
        stopOwnStreams()
        startRealtime()
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
            try {
                eventStream.listen(
                    account,
                    onConnected = {
                        failures = 0
                        // Run one silent sync before interpreting a live event.
                        ServerSync.refresh(cache, id)
                        reload()
                        setLiveStatus(id, LiveStatus.CONNECTED)
                    },
                    onMessage = { lastId ->
                        // Await REST+SQLite before publishing the new UI state; no worker can
                        // be cancelled between showing a notification and rendering the feed.
                        val added = ServerSync.refresh(cache, id)
                        reload()
                        mutable.update { it.copy(syncError = null) }
                        added.filter { it.readAt == null }.take(12).forEach { notice ->
                            LiveNotifications.show(getApplication(), notice)
                        }
                    }
                )
            } catch (cancel: CancellationException) {
                throw cancel
            } catch (error: ApiError) {
                if (error.code == 401 || error.code == 403) {
                    setLiveStatus(id, LiveStatus.LOGIN_REQUIRED)
                    break
                }
                setLiveStatus(id, LiveStatus.RECONNECTING)
            } catch (error: Exception) {
                setLiveStatus(id, LiveStatus.RECONNECTING)
                mutable.update { it.copy(syncError = account.label + ": " + describe(error)) }
            }
            failures++
            delay((1000L shl failures.coerceAtMost(5)).coerceAtMost(30000L))
        }
    }

    fun openTab(tab: Tab) { mutable.update { it.copy(tab = tab) } }

    /** System notification clicks contain only a local connection ID and message ID. */
    fun openNotification(serverId: String, messageId: String) {
        if (cache.servers().none { it.id == serverId }) {
            mutable.update { it.copy(message = "Подключение для этого уведомления уже удалено.") }
            return
        }
        val key = serverId + ":" + messageId
        val alreadyCached = mutable.value.messages.any { it.key == key }
        mutable.update {
            it.copy(
                openNotificationKey = key, tab = Tab.INBOX,
                inboxMode = InboxMode.ALL, selectedServer = serverId, selectedChannel = null
            )
        }
        if (!alreadyCached) refresh()
    }

    fun closeNotification() {
        mutable.update { it.copy(openNotificationKey = null) }
    }

    fun configureAlert(channel: ChannelInfo, profile: AlertProfile) {
        try {
            AlertProfiles.save(getApplication(), channel.serverId, channel.slug, profile)
            mutable.update { it.copy(alertSettingsRevision = it.alertSettingsRevision + 1) }
        } catch (error: Exception) {
            mutable.update { it.copy(message = "Не удалось сохранить оповещения: " + describe(error)) }
        }
    }
    fun selectInbox(mode: InboxMode) {
        mutable.update { it.copy(
            tab = Tab.INBOX, inboxMode = mode,
            selectedChannel = null, selectedServer = null
        ) }
    }
    fun filterServer(id: String?) {
        mutable.update { it.copy(
            selectedServer = id, selectedChannel = null,
            inboxMode = InboxMode.ALL, tab = Tab.INBOX
        ) }
    }
    fun filterChannel(channel: ChannelInfo?) {
        mutable.update {
            it.copy(
                selectedChannel = channel?.let { c -> c.serverId + "::" + c.slug },
                selectedServer = channel?.serverId,
                inboxMode = if (channel == null) InboxMode.ALL else InboxMode.CHANNEL,
                tab = Tab.INBOX
            )
        }
    }
    fun search(text: String) { mutable.update { it.copy(search = text) } }
    fun searchServer(id: String?) { mutable.update {
        it.copy(searchServer = id, searchChannel = null)
    } }
    fun searchChannel(id: String?) { mutable.update { it.copy(searchChannel = id) } }
    fun searchRead(filter: ReadFilter) { mutable.update { it.copy(searchRead = filter) } }
    fun searchPriority(value: String?) { mutable.update { it.copy(searchPriority = value) } }
    fun resetSearch() { mutable.update { it.copy(
        search = "", searchServer = null, searchChannel = null,
        searchRead = ReadFilter.ALL, searchPriority = null
    ) } }
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
                    ServerSync.refresh(cache, account.id)
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

    fun refresh() {
        if (mutable.value.busy) return
        val accounts = cache.servers()
        if (accounts.isEmpty()) { reload(); return }
        mutable.update { it.copy(busy = true, syncError = null) }
        viewModelScope.launch {
            val errors = withContext(Dispatchers.IO) {
                accounts.mapNotNull { account ->
                    runCatching { ServerSync.refresh(cache, account.id) }.exceptionOrNull()
                        ?.let { account.label + ": " + describe(it) }
                }
            }
            reload()
            mutable.update { it.copy(busy = false, syncError = errors.takeIf { e -> e.isNotEmpty() }?.joinToString("\n")) }
        }
    }

    private val changingRead = mutableSetOf<String>()

    fun markRead(notice: Notice) = setReadState(notice, true)
    fun markUnread(notice: Notice) = setReadState(notice, false)

    fun setReadState(notice: Notice, shouldRead: Boolean) {
        val account = cache.servers().firstOrNull { it.id == notice.serverId } ?: return
        if (!changingRead.add(notice.key)) return
        viewModelScope.launch {
            try {
                withContext(Dispatchers.IO) {
                    if (shouldRead) {
                        Api.read(account, notice.id)
                        cache.markRead(notice.serverId, notice.id)
                    } else {
                        Api.unread(account, notice.id)
                        cache.markUnread(notice.serverId, notice.id)
                    }
                }
                reload()
                if (shouldRead) LiveNotifications.dismiss(getApplication(), notice)
                ChangeBus.emit()
            } catch (error: Exception) {
                mutable.update { it.copy(message =
                    "Не удалось изменить статус уведомления: " + describe(error)) }
            } finally {
                changingRead.remove(notice.key)
            }
        }
    }

    fun renameServer(id: String, label: String) {
        cache.renameAccount(id, label)
        reload()
    }

    fun removeServer(id: String) {
        val account = cache.servers().firstOrNull { it.id == id }
        // Local removal always works offline; remote logout is a best-effort cleanup.
        val slugs = cache.channels().filter { it.serverId == id }.map { it.slug }
        cache.deleteAccount(id)
        AlertProfiles.forgetServer(getApplication(), id, slugs)
        LiveNotifications.forgetServer(getApplication(), id)
        reload()
        ChangeBus.emit()
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
                    ServerSync.refresh(cache, updated.id)
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
