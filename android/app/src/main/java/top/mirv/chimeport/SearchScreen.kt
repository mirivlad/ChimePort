package top.mirv.chimeport

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState
import top.mirv.chimeport.data.Notice
import top.mirv.chimeport.data.ReadFilter

@Composable
fun SearchScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    var selectedKey by remember { mutableStateOf<String?>(null) }
    var filtersOpen by remember { mutableStateOf(false) }
    val query = state.search.trim()
    val filtered = state.messages.filter { message ->
        (query.isBlank() || listOf(
            message.title, message.body, message.source,
            message.channelName, message.channel, message.serverLabel
        ).any { it.contains(query, ignoreCase = true) }) &&
        (state.searchServer == null || message.serverId == state.searchServer) &&
        (state.searchChannel == null ||
            message.serverId + "::" + message.channel == state.searchChannel) &&
        (state.searchPriority == null || message.priority == state.searchPriority) &&
        when (state.searchRead) {
            ReadFilter.ALL -> true
            ReadFilter.UNREAD -> message.readAt == null
            ReadFilter.READ -> message.readAt != null
        }
    }
    val channels = state.channels.filter {
        state.searchServer == null || it.serverId == state.searchServer
    }
    Column(modifier.fillMaxSize()) {
        Column(Modifier.padding(horizontal = 16.dp, vertical = 14.dp)) {
            PageTitle("Поиск", "По сохранённой истории ваших серверов")
            Spacer(Modifier.height(12.dp))
            OutlinedTextField(
                value = state.search, onValueChange = model::search,
                label = { Text("Найти уведомление") },
                leadingIcon = { Icon(Icons.Outlined.Search, null) },
                trailingIcon = {
                    if (state.search.isNotEmpty()) IconButton(onClick = { model.search("") }) {
                        Icon(Icons.Outlined.Close, "Очистить поиск")
                    }
                },
                singleLine = true, modifier = Modifier.fillMaxWidth()
            )
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                TextButton(onClick = { filtersOpen = !filtersOpen }) {
                    Icon(Icons.Outlined.Tune, null, Modifier.size(18.dp))
                    Spacer(Modifier.width(7.dp))
                    Text(if (filtersOpen) "Скрыть фильтры" else "Фильтры")
                }
                TextButton(onClick = model::resetSearch) { Text("Сбросить") }
                Spacer(Modifier.weight(1f))
                Text(
                    "Найдено: " + filtered.size,
                    style = MaterialTheme.typography.labelMedium,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    modifier = Modifier.padding(top = 16.dp)
                )
            }
            if (filtersOpen) {
                Text("Сервер", style = MaterialTheme.typography.labelMedium)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item {
                        FilterChip(selected = state.searchServer == null,
                            onClick = { model.searchServer(null) }, label = { Text("Все") })
                    }
                    items(state.servers, key = { it.id }) { server ->
                        FilterChip(selected = state.searchServer == server.id,
                            onClick = { model.searchServer(server.id) },
                            label = { Text(server.label) })
                    }
                }
                Text("Канал", style = MaterialTheme.typography.labelMedium)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    item {
                        FilterChip(selected = state.searchChannel == null,
                            onClick = { model.searchChannel(null) }, label = { Text("Все") })
                    }
                    items(channels, key = { it.serverId + "::" + it.slug }) { channel ->
                        FilterChip(
                            selected = state.searchChannel == channel.serverId + "::" + channel.slug,
                            onClick = { model.searchChannel(channel.serverId + "::" + channel.slug) },
                            label = { Text(channel.name +
                                if (state.servers.size > 1) " · " + channel.serverLabel else "") }
                        )
                    }
                }
                Text("Статус", style = MaterialTheme.typography.labelMedium)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(listOf(
                        ReadFilter.ALL to "Все",
                        ReadFilter.UNREAD to "Непрочитанные",
                        ReadFilter.READ to "Прочитанные"
                    )) { (value, label) ->
                        FilterChip(selected = state.searchRead == value,
                            onClick = { model.searchRead(value) }, label = { Text(label) })
                    }
                }
                Text("Приоритет", style = MaterialTheme.typography.labelMedium)
                LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    items(listOf(
                        null to "Любой", "urgent" to "Срочно",
                        "high" to "Важно", "normal" to "Обычный", "low" to "Низкий"
                    )) { (value, label) ->
                        FilterChip(selected = state.searchPriority == value,
                            onClick = { model.searchPriority(value) }, label = { Text(label) })
                    }
                }
            }
        }
        if (state.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (filtered.isEmpty()) {
            Box(Modifier.weight(1f)) {
                EmptyPanel(Icons.Outlined.SearchOff,
                    "Ничего не найдено",
                    "Измени запрос или фильтры. Поиск выполняется по уже загруженной истории.",
                    "Очистить фильтры", model::resetSearch)
            }
        } else {
            LazyColumn(
                modifier = Modifier.weight(1f).fillMaxWidth(),
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 8.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                itemsIndexed(filtered, key = { _, notice -> notice.key }) { _, message ->
                    SwipeNoticeCard(
                        message = message,
                        showServer = state.servers.size > 1,
                        onOpen = { selectedKey = message.key },
                        onChangeRead = { model.setReadState(message, it) }
                    )
                }
            }
        }
    }
    selectedKey?.let { key ->
        state.messages.firstOrNull { it.key == key }?.let { message ->
            NoticeDetail(message, onDismiss = { selectedKey = null }) { read ->
                model.setReadState(message, read)
                selectedKey = null
            }
        }
    }
}
