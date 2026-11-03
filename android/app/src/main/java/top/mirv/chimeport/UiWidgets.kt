package top.mirv.chimeport

import androidx.compose.foundation.background
import androidx.compose.foundation.border
import androidx.compose.foundation.clickable
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.key
import androidx.compose.runtime.rememberUpdatedState
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.vector.ImageVector
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.Notice
import java.time.Instant
import java.time.ZoneId
import java.time.format.DateTimeFormatter
import java.util.Locale

@Composable
fun PageTitle(title: String, subtitle: String, modifier: Modifier = Modifier) {
    Column(modifier) {
        Text(title, style = MaterialTheme.typography.headlineMedium, fontWeight = FontWeight.Bold)
        Spacer(Modifier.height(4.dp))
        Text(subtitle, style = MaterialTheme.typography.bodyMedium,
            color = MaterialTheme.colorScheme.onSurfaceVariant)
    }
}

@Composable
fun EmptyPanel(
    icon: ImageVector, title: String, body: String,
    action: String? = null, onAction: (() -> Unit)? = null
) {
    Column(
        Modifier.fillMaxWidth().padding(top = 52.dp, start = 26.dp, end = 26.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        Surface(shape = RoundedCornerShape(24.dp),
            color = MaterialTheme.colorScheme.primaryContainer,
            modifier = Modifier.size(86.dp)) {
            Box(contentAlignment = Alignment.Center) {
                Icon(icon, contentDescription = null, modifier = Modifier.size(35.dp),
                    tint = MaterialTheme.colorScheme.primary)
            }
        }
        Spacer(Modifier.height(24.dp))
        Text(title, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.SemiBold)
        Spacer(Modifier.height(8.dp))
        Text(body, color = MaterialTheme.colorScheme.onSurfaceVariant,
            style = MaterialTheme.typography.bodyMedium)
        if (action != null && onAction != null) {
            Spacer(Modifier.height(23.dp))
            Button(onClick = onAction) { Text(action) }
        }
    }
}

@Composable
fun PriorityTag(priority: String) {
    val (background, foreground, text) = when (priority) {
        "urgent" -> Triple(Color(0xFF66323B), Color(0xFFFFD2D2), "СРОЧНО")
        "high" -> Triple(Color(0xFF5C4827), Color(0xFFFFDE9A), "ВАЖНО")
        "low" -> Triple(MaterialTheme.colorScheme.surfaceVariant,
            MaterialTheme.colorScheme.onSurfaceVariant, "низкий")
        else -> Triple(MaterialTheme.colorScheme.primaryContainer,
            MaterialTheme.colorScheme.onPrimaryContainer, "обычный")
    }
    Surface(color = background, shape = RoundedCornerShape(7.dp)) {
        Text(text, Modifier.padding(horizontal = 8.dp, vertical = 4.dp), color = foreground,
            style = MaterialTheme.typography.labelSmall, fontWeight = FontWeight.SemiBold)
    }
}

fun eventTime(raw: String): String = runCatching {
    Instant.parse(raw).atZone(ZoneId.systemDefault())
        .format(DateTimeFormatter.ofPattern("HH:mm · d MMM", Locale("ru")))
}.getOrDefault(raw)

fun eventDay(raw: String): String = runCatching {
    Instant.parse(raw).atZone(ZoneId.systemDefault())
        .format(DateTimeFormatter.ofPattern("d MMMM yyyy", Locale("ru")))
}.getOrDefault("Другие")

@Composable
fun NoticeCard(notice: Notice, onOpen: () -> Unit, showServer: Boolean) {
    ElevatedCard(
        modifier = Modifier.fillMaxWidth().clickable(onClick = onOpen),
        shape = RoundedCornerShape(18.dp),
        colors = CardDefaults.elevatedCardColors(
            containerColor = MaterialTheme.colorScheme.surface
        ),
        elevation = CardDefaults.elevatedCardElevation(defaultElevation = 1.dp)
    ) {
        Row(Modifier.fillMaxWidth().padding(17.dp), verticalAlignment = Alignment.Top) {
            Box(
                Modifier.width(4.dp).height(53.dp).background(
                    if (notice.readAt == null) MaterialTheme.colorScheme.primary
                    else MaterialTheme.colorScheme.outline.copy(alpha = 0.33f),
                    RoundedCornerShape(5.dp))
            )
            Spacer(Modifier.width(14.dp))
            Column(Modifier.weight(1f)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Text(notice.title, Modifier.weight(1f),
                        style = MaterialTheme.typography.titleMedium,
                        fontWeight = if (notice.readAt == null) FontWeight.Bold else FontWeight.Medium,
                        maxLines = 2, overflow = TextOverflow.Ellipsis)
                    if (notice.readAt == null) {
                        Spacer(Modifier.width(8.dp))
                        Box(Modifier.size(8.dp).background(
                            MaterialTheme.colorScheme.primary, RoundedCornerShape(50)))
                    }
                }
                if (notice.body.isNotBlank()) {
                    Spacer(Modifier.height(7.dp))
                    Text(notice.body, style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 3, overflow = TextOverflow.Ellipsis)
                }
                Spacer(Modifier.height(13.dp))
                Row(verticalAlignment = Alignment.CenterVertically,
                    horizontalArrangement = Arrangement.spacedBy(7.dp)) {
                    PriorityTag(notice.priority)
                    Text(notice.channelName, style = MaterialTheme.typography.labelSmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant,
                        maxLines = 1, overflow = TextOverflow.Ellipsis)
                }
                Spacer(Modifier.height(9.dp))
                Text(
                    (if (showServer) notice.serverLabel + " · " else "") +
                        notice.source + "  ·  " + eventTime(notice.createdAt),
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    maxLines = 2, overflow = TextOverflow.Ellipsis
                )
            }
        }
    }
}

@Composable
fun SectionTitle(text: String, description: String? = null) {
    Column(Modifier.padding(bottom = 16.dp)) {
        Text(text, style = MaterialTheme.typography.titleLarge, fontWeight = FontWeight.Bold)
        if (description != null) {
            Spacer(Modifier.height(5.dp))
            Text(description, color = MaterialTheme.colorScheme.onSurfaceVariant,
                style = MaterialTheme.typography.bodySmall)
        }
    }
}


/** The same message can be marked read and unread repeatedly. */
@Composable
fun SwipeNoticeCard(
    message: Notice,
    showServer: Boolean,
    onOpen: () -> Unit,
    onChangeRead: (Boolean) -> Unit
) {
    val isRead = message.readAt != null
    // SwipeToDismissBox remembers the first confirm callback for its saved state.
    // Always use the newest message status and action after changing tabs.
    val currentRead by rememberUpdatedState(isRead)
    val currentChangeRead by rememberUpdatedState(onChangeRead)
    val currentOpen by rememberUpdatedState(onOpen)

    // A new gesture state after each status change also clears any old swipe offset.
    key(message.key, isRead) {
        val swipeState = rememberSwipeToDismissBoxState(
            confirmValueChange = { direction ->
                val swipe = when (direction) {
                    SwipeToDismissBoxValue.EndToStart -> ReadSwipeDirection.LEFT
                    SwipeToDismissBoxValue.StartToEnd -> ReadSwipeDirection.RIGHT
                    SwipeToDismissBoxValue.Settled -> null
                }
                swipe?.let { readStateAfterSwipe(currentRead, it) }?.let(currentChangeRead)
                false // Keep the card in place until the API has persisted the new state.
            },
            positionalThreshold = { distance -> distance * 0.30f }
        )
        SwipeToDismissBox(
            state = swipeState,
            enableDismissFromStartToEnd = isRead,
            enableDismissFromEndToStart = !isRead,
            backgroundContent = {
                Box(
                    Modifier.fillMaxSize()
                        .background(
                            if (isRead) MaterialTheme.colorScheme.secondaryContainer
                            else MaterialTheme.colorScheme.primaryContainer,
                            RoundedCornerShape(18.dp)
                        )
                        .padding(horizontal = 22.dp),
                    contentAlignment = if (isRead) Alignment.CenterStart else Alignment.CenterEnd
                ) {
                    Row(
                        verticalAlignment = Alignment.CenterVertically,
                        horizontalArrangement = Arrangement.spacedBy(8.dp)
                    ) {
                        Icon(
                            if (isRead) Icons.Outlined.MarkEmailUnread else Icons.Outlined.DoneAll,
                            contentDescription = null
                        )
                        Text(
                            if (isRead) "В непрочитанные" else "Прочитано",
                            style = MaterialTheme.typography.labelLarge
                        )
                    }
                }
            }
        ) {
            NoticeCard(message, currentOpen, showServer)
        }
    }
}
