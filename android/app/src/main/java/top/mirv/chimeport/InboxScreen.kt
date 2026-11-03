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
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState
import top.mirv.chimeport.data.InboxMode
import top.mirv.chimeport.data.Notice

@Composable
fun InboxScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    var selectedKey by remember { mutableStateOf<String?>(null) }
    val shown = state.messages.filter { message ->
        when (state.inboxMode) {
            InboxMode.UNREAD -> message.readAt == null
            InboxMode.ALL -> state.selectedServer == null || message.serverId == state.selectedServer
            InboxMode.CHANNEL -> message.serverId + "::" + message.channel == state.selectedChannel
        }
    }
    val unread = state.messages.count { it.readAt == null }
    Column(modifier.fillMaxSize()) {
        Column(Modifier.fillMaxWidth().padding(start = 16.dp, end = 16.dp, top = 14.dp)) {
            PageTitle("Входящие", "$unread непрочитанных · " + state.messages.size + " в истории")
            Spacer(Modifier.height(11.dp))
            LazyRow(
                horizontalArrangement = Arrangement.spacedBy(8.dp),
                contentPadding = PaddingValues(end = 12.dp)
            ) {
                item {
                    FilterChip(
                        selected = state.inboxMode == InboxMode.UNREAD,
                        onClick = { model.selectInbox(InboxMode.UNREAD) },
                        label = { Text("Непрочитанные · " + unread) }
                    )
                }
                item {
                    FilterChip(
                        selected = state.inboxMode == InboxMode.ALL && state.selectedServer == null,
                        onClick = { model.selectInbox(InboxMode.ALL) },
                        label = { Text("Все") }
                    )
                }
                items(state.channels, key = { it.serverId + "::" + it.slug }) { channel ->
                    val key = channel.serverId + "::" + channel.slug
                    FilterChip(
                        selected = state.inboxMode == InboxMode.CHANNEL && state.selectedChannel == key,
                        onClick = { model.filterChannel(channel) },
                        label = { Text(channel.name +
                            if (state.servers.size > 1) " · " + channel.serverLabel else "") }
                    )
                }
            }
            if (state.inboxMode == InboxMode.ALL && state.selectedServer != null) {
                val serverName = state.servers.firstOrNull { it.id == state.selectedServer }?.label
                TextButton(onClick = { model.selectInbox(InboxMode.ALL) }) {
                    Text("Сервер: " + (serverName ?: "Все") + " · Сбросить")
                }
            }
            Spacer(Modifier.height(4.dp))
        }
        if (state.busy) LinearProgressIndicator(Modifier.fillMaxWidth())
        if (shown.isEmpty()) {
            Box(Modifier.weight(1f).fillMaxWidth()) {
                val nothingEver = state.messages.isEmpty()
                EmptyPanel(
                    Icons.Outlined.MarkEmailUnread,
                    if (nothingEver) "Здесь пока тихо"
                    else if (state.inboxMode == InboxMode.UNREAD) "Всё прочитано"
                    else "В этой вкладке нет уведомлений",
                    if (nothingEver) "Отправьте первое сообщение с сервера — оно появится автоматически."
                    else "Другие сообщения доступны во вкладке «Все» или в поиске.",
                    if (nothingEver) "Обновить" else null,
                    if (nothingEver) model::refresh else null
                )
            }
        } else {
            LazyColumn(
                modifier = Modifier.weight(1f).fillMaxWidth(),
                contentPadding = PaddingValues(start = 16.dp, end = 16.dp, bottom = 24.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                itemsIndexed(shown, key = { _, message -> message.key }) { index, message ->
                    val day = eventDay(message.createdAt)
                    if (index == 0 || day != eventDay(shown[index - 1].createdAt)) {
                        Text(day, style = MaterialTheme.typography.labelLarge,
                            fontWeight = FontWeight.SemiBold,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(top = 10.dp, bottom = 3.dp, start = 7.dp))
                    }
                    SwipeNoticeCard(
                        message = message, showServer = state.servers.size > 1,
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

@Composable
fun NoticeDetail(message: Notice, onDismiss: () -> Unit, onSetRead: (Boolean) -> Unit) {
    val context = LocalContext.current
    val currentlyRead = message.readAt != null
    AlertDialog(
        onDismissRequest = onDismiss,
        icon = { Icon(Icons.Outlined.Notifications, null) },
        title = { Text(message.title, style = MaterialTheme.typography.titleLarge) },
        text = {
            Column(verticalArrangement = Arrangement.spacedBy(12.dp)) {
                if (message.body.isNotBlank()) Text(message.body, style = MaterialTheme.typography.bodyLarge)
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
            Button(onClick = { onSetRead(!currentlyRead) }) {
                Text(if (currentlyRead) "В непрочитанные" else "Отметить прочитанным")
            }
        },
        dismissButton = { TextButton(onClick = onDismiss) { Text("Закрыть") } }
    )
}
