package top.mirv.chimeport

import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState

@Composable
fun ChannelsScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxSize()) {
        PageTitle(
            "Каналы",
            "Потоки уведомлений с ваших серверов",
            Modifier.padding(start = 20.dp, end = 20.dp, top = 24.dp, bottom = 20.dp)
        )
        if (state.channels.isEmpty()) {
            EmptyPanel(
                Icons.Outlined.GridView,
                "Каналов пока нет",
                "Создайте канал в веб-панели сервера, затем обновите приложение.",
                "Обновить", onAction = model::refresh
            )
        } else {
            LazyColumn(
                contentPadding = PaddingValues(horizontal = 16.dp, vertical = 4.dp),
                verticalArrangement = Arrangement.spacedBy(10.dp)
            ) {
                items(state.channels, key = { it.serverId + ":" + it.slug }) { channel ->
                    ElevatedCard(
                        modifier = Modifier.fillMaxWidth().clickable {
                            model.filterChannel(channel)
                        },
                        shape = RoundedCornerShape(16.dp),
                        colors = CardDefaults.elevatedCardColors(
                            containerColor = MaterialTheme.colorScheme.surface
                        )
                    ) {
                        Row(Modifier.fillMaxWidth().padding(18.dp),
                            verticalAlignment = Alignment.CenterVertically) {
                            Surface(
                                shape = RoundedCornerShape(13.dp),
                                color = MaterialTheme.colorScheme.primaryContainer,
                                modifier = Modifier.size(48.dp)
                            ) {
                                Box(contentAlignment = Alignment.Center) {
                                    Icon(
                                        Icons.Outlined.FolderOpen, null,
                                        tint = MaterialTheme.colorScheme.primary
                                    )
                                }
                            }
                            Spacer(Modifier.width(14.dp))
                            Column(Modifier.weight(1f)) {
                                Text(
                                    channel.name,
                                    fontWeight = FontWeight.SemiBold,
                                    style = MaterialTheme.typography.titleMedium
                                )
                                Spacer(Modifier.height(4.dp))
                                Text(
                                    channel.serverLabel + " · " + channel.slug,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    style = MaterialTheme.typography.bodySmall
                                )
                            }
                            if (channel.unread > 0) {
                                Badge { Text(channel.unread.toString()) }
                                Spacer(Modifier.width(10.dp))
                            }
                            Icon(
                                Icons.Outlined.ChevronRight,
                                contentDescription = "Открыть канал",
                                tint = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                    }
                }
            }
        }
    }
}
