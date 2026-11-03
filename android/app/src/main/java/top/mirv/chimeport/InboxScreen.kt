package top.mirv.chimeport

import android.content.Intent
import android.net.Uri
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.LazyRow
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.lazy.itemsIndexed
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState
import top.mirv.chimeport.data.Notice

@Composable
fun InboxScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    var selected by remember { mutableStateOf<Notice?>(null) }
    val filtered = remember(
        state.messages, state.selectedServer, state.selectedChannel,
        state.unreadOnly, state.search
    ) {
        state.messages.filter { msg ->
            (state.selectedServer == null || msg.serverId == state.selectedServer) &&
                (state.selectedChannel == null || msg.serverId + "::" + msg.channel == state.selectedChannel) &&
                (!state.unreadOnly || msg.readAt == null) &&
                (state.search.isBlank() || listOf(
                    msg.title, msg.body, msg.source, msg.channelName, msg.serverLabel
                ).any { it.contains(state.search.trim(), ignoreCase = true) })
        }
    }
    val unread = state.messages.count { it.readAt == null }
    Column(modifier.fillMaxSize()) {
        Column(Modifier.fillMaxWidth().padding(start = 20.dp, end = 20.dp, top = 22.dp)) {
            PageTitle("Входящие", "$unread непрочитанных · " + state.messages.size + " в истории")
            Spacer(Modifier.height(18.dp))
            OutlinedTextField(
                value = state.search, onValueChange = model::search,
                label = { Text("Найти в загруженной истории") },
                leadingIcon = { Icon(Icons.Outlined.Search, null) },
                singleLine = true, modifier = Modifier.fillMaxWidth()
            )
            Spacer(Modifier.height(11.dp))
            LazyRow(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                item {
                    FilterChip(
                        selected = state.selectedServer == null,
                        onClick = { model.filterServer(null) },
                        label = { Text("Все серверы") }
                    )
                }
                items(state.servers, key = { it.id }) { account ->
                    FilterChip(
                        selected = state.selectedServer == account.id,
                        onClick = { model.filterServer(account.id) },
                        label = { Text(account.label) }
                    )
                }
            }
            Row(verticalAlignment = Alignment.CenterVertically) {
                FilterChip(
                    selected = state.unreadOnly,
                    onClick = { model.unreadOnly(!state.unreadOnly) },
                    leadingIcon = {
                        if (state.unreadOnly) Icon(
                            Icons.Outlined.CheckCircle, null,
                            modifier = Modifier.size(18.dp)
                        )
                    },
                    label = { Text("Только непрочитанные") }
                )
                Spacer(Modifier.weight(1f))
                if (state.selectedChannel != null) {
                    TextButton(onClick = { model.filterChannel(null) }) { Text("Сбросить канал") }
                }
            }
        }
        if (state.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (filtered.isEmpty()) {
            Box(Modifier.fillMaxSize().weight(1f)) {
                EmptyPanel(
                    Icons.Outlined.MarkEmailUnread,
                    if (state.messages.isEmpty()) "Здесь пока тихо" else "Ничего не найдено",
                    if (state.messages.isEmpty())
                        "Отправьте уведомление из скрипта или Home Assistant. Пока приложение открыто, лента обновится сама."
                    else "Попробуйте другой фильтр или поисковый запрос.",
                    if (state.messages.isEmpty()) "Обновить ленту" else "Сбросить фильтры",
                    onAction = {
                        if (state.messages.isEmpty()) model.refresh()
                        else {
                            model.search("")
                            model.unreadOnly(false)
                            model.filterChannel(null)
                        }
                    }
                )
            }
        } else {
            LazyColumn(
                Modifier.fillMaxSize().weight(1f),
                contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                itemsIndexed(filtered, key = { _, m -> m.key }) { index, msg ->
                    val day = eventDay(msg.createdAt)
                    if (index == 0 || day != eventDay(filtered[index - 1].createdAt)) {
                        Text(
                            day,
                            style = MaterialTheme.typography.labelLarge,
                            fontWeight = FontWeight.SemiBold,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 12.dp, bottom = 4.dp, start = 7.dp)
                        )
                    }
                    NoticeCard(msg, onOpen = { selected = msg }, showServer = state.servers.size > 1)
                }
            }
        }
    }
    val message = selected
    if (message != null) {
        NoticeDetail(message, onDismiss = { selected = null }, onRead = {
            model.markRead(message)
            selected = null
        })
    }
}

@Composable
private fun NoticeDetail(message: Notice, onDismiss: () -> Unit, onRead: () -> Unit) {
    val context = LocalContext.current
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.Notifications, null) },
        title = { Text(message.title, style = MaterialTheme.typography.titleLarge) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (message.body.isNotBlank()) {
                    Text(message.body, style = MaterialTheme.typography.bodyLarge)
                }
                PriorityTag(message.priority)
                HorizontalDivider()
                Text(
                    "Канал: " + message.channelName + "\n" +
                        "Источник: " + message.source + "\n" +
                        "Сервер: " + message.serverLabel + "\n" +
                        "Время: " + eventTime(message.createdAt),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                if (message.url.startsWith("https://")) {
                    TextButton(onClick = {
                        context.startActivity(Intent(Intent.ACTION_VIEW, Uri.parse(message.url)))
                    }) {
                        Icon(Icons.Outlined.OpenInNew, null)
                        Spacer(Modifier.width(6.dp))
                        Text("Открыть ссылку")
                    }
                }
            }
        },
        confirmButton = {
            if (message.readAt == null) Button(onClick = onRead) { Text("Отметить прочитанным") }
            else TextButton(onClick = onDismiss) { Text("Закрыть") }
        },
        dismissButton = {
            if (message.readAt == null) TextButton(onClick = onDismiss) { Text("Позже") }
        }
    )
}
