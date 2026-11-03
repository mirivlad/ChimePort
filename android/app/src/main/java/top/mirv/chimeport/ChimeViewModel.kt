package top.mirv.chimeport

import android.app.Application
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.launch
import kotlinx.coroutines.withContext
import top.mirv.chimeport.data.*
import java.io.IOException

class ChimeViewModel(application: Application) : AndroidViewModel(application) {
    private val cache = Cache(application)
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

    private fun syncOne(server: ServerAccount) {
        if (server.token.isEmpty()) throw IOException("Нужен повторный вход: локальный ключ сессии недоступен.")
        if (Api.probe(server.origin).id != server.instanceId) {
            throw IOException("Идентичность сервера изменилась. История не объединена — проверьте подключение.")
        }
        cache.replaceChannels(server.id, Api.channels(server))
        var cursor = server.cursor
        var count = 0
        do {
            val page = Api.messages(server, cursor)
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
    }

    fun refresh() {
        if (mutable.value.busy) return
        val accounts = cache.servers()
        if (accounts.isEmpty()) { reload(); return }
        mutable.update { it.copy(busy = true, syncError = null) }
        viewModelScope.launch {
            val errors = withContext(Dispatchers.IO) {
                accounts.mapNotNull { account ->
                    runCatching { syncOne(account) }.exceptionOrNull()
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
