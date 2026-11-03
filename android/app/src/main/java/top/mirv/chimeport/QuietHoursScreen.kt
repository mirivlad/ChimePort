package top.mirv.chimeport

import android.app.TimePickerDialog
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
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

enum class QuietScope { GLOBAL, SERVER, CHANNEL }

data class QuietTarget(
    val scope: QuietScope,
    val title: String,
    val serverId: String = "",
    val channelSlug: String = ""
) {
    val key: String get() = scope.name + ":" + serverId + ":" + channelSlug
}

private fun loadQuiet(context: android.content.Context, target: QuietTarget): QuietRule =
    when (target.scope) {
        QuietScope.GLOBAL -> QuietSchedules.global(context)
        QuietScope.SERVER -> QuietSchedules.server(context, target.serverId)
        QuietScope.CHANNEL -> QuietSchedules.channel(context, target.serverId, target.channelSlug)
    }

private fun quietLabel(rule: QuietRule): String = when (rule.mode) {
    QuietMode.INHERIT -> "Наследует настройки выше"
    QuietMode.OFF -> "Не беспокоить по расписанию выключено"
    QuietMode.SCHEDULE -> "Каждый день " + formatQuietMinute(rule.startMinutes) +
        "–" + formatQuietMinute(rule.endMinutes) +
        if (rule.allowUrgent) " · срочные со звуком" else " · все без звука"
}

@Composable
fun QuietHoursScreen(
    state: AppState,
    model: ChimeViewModel,
    onBack: () -> Unit,
    modifier: Modifier = Modifier
) {
    val context = LocalContext.current
    var selected by remember { mutableStateOf<QuietTarget?>(null) }
    Column(modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 8.dp, end = 18.dp, top = 12.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = onBack) { Icon(Icons.Outlined.ArrowBack, "Назад") }
            Column(Modifier.weight(1f)) {
                Text("Тихие часы", style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold)
                Text("Общее расписание, серверы и каналы",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(start = 18.dp, end = 18.dp, bottom = 28.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            item {
                Text(
                    "Новые сообщения продолжают поступать и остаются непрочитанными. " +
                        "В период тишины они появляются в шторке без звука и вибрации. " +
                        "Расписание действует по местному времени телефона.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                Spacer(Modifier.height(10.dp))
            }
            val global = QuietTarget(QuietScope.GLOBAL, "Все серверы")
            item {
                QuietRow(global, QuietSchedules.global(context)) { selected = global }
            }
            state.servers.forEach { server ->
                item(key = "heading-" + server.id) {
                    Row(
                        Modifier.fillMaxWidth().padding(top = 12.dp, bottom = 3.dp),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Icon(Icons.Outlined.Dns, null, tint = MaterialTheme.colorScheme.primary)
                        Spacer(Modifier.width(8.dp))
                        Text(server.label, style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold)
                    }
                }
                val target = QuietTarget(QuietScope.SERVER, "Сервер «" + server.label + "»",
                    server.id)
                item(key = "server-" + server.id) {
                    QuietRow(target, QuietSchedules.server(context, server.id)) {
                        selected = target
                    }
                }
                val channels = state.channels.filter { it.serverId == server.id }
                if (channels.isEmpty()) {
                    item(key = "empty-" + server.id) {
                        Text("Каналов пока нет", style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                items(channels, key = { it.serverId + "::" + it.slug }) { channel ->
                    val channelTarget = QuietTarget(
                        QuietScope.CHANNEL, "Канал «" + channel.name + "»",
                        channel.serverId, channel.slug
                    )
                    QuietRow(
                        channelTarget,
                        QuietSchedules.channel(context, channel.serverId, channel.slug),
                        indent = true
                    ) { selected = channelTarget }
                }
            }
            item {
                Spacer(Modifier.height(8.dp))
                Text(
                    "Исключение для срочных относится только к событиям с приоритетом " +
                        "urgent и не обходит системный режим Android «Не беспокоить». " +
                        "Служебное уведомление фонового подключения остаётся видимым.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
    selected?.let { target ->
        key(target.key, state.quietSettingsRevision) {
            QuietRuleEditor(
                target = target,
                initial = loadQuiet(context, target),
                onDismiss = { selected = null },
                onSave = { rule ->
                    model.configureQuiet(target, rule)
                    selected = null
                }
            )
        }
    }
}

@Composable
private fun QuietRow(
    target: QuietTarget, rule: QuietRule, indent: Boolean = false, onClick: () -> Unit
) {
    ElevatedCard(onClick = onClick, modifier = Modifier.padding(start = if (indent) 13.dp else 0.dp)) {
        Row(
            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 14.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            Icon(
                when (target.scope) {
                    QuietScope.GLOBAL -> Icons.Outlined.Public
                    QuietScope.SERVER -> Icons.Outlined.Dns
                    QuietScope.CHANNEL -> Icons.Outlined.NotificationsNone
                },
                null, tint = MaterialTheme.colorScheme.primary
            )
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text(target.title, style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.SemiBold)
                Spacer(Modifier.height(3.dp))
                Text(
                    quietLabel(rule),
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
            Icon(Icons.Outlined.ChevronRight, "Настроить",
                tint = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

@Composable
private fun QuietRuleEditor(
    target: QuietTarget,
    initial: QuietRule,
    onDismiss: () -> Unit,
    onSave: (QuietRule) -> Unit
) {
    val context = LocalContext.current
    var mode by remember { mutableStateOf(initial.mode) }
    var from by remember { mutableIntStateOf(initial.startMinutes) }
    var to by remember { mutableIntStateOf(initial.endMinutes) }
    var allowUrgent by remember { mutableStateOf(initial.allowUrgent) }

    val modes = if (target.scope == QuietScope.GLOBAL) {
        listOf(QuietMode.OFF, QuietMode.SCHEDULE)
    } else QuietMode.entries

    AlertDialog(
        onDismissRequest = onDismiss,
        title = { Text(target.title) },
        text = {
            Column(
                Modifier.heightIn(max = 470.dp).verticalScroll(rememberScrollState())
            ) {
                Text("Режим", style = MaterialTheme.typography.titleSmall,
                    fontWeight = FontWeight.Bold)
                modes.forEach { option ->
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(selected = mode == option, onClick = { mode = option })
                        TextButton(
                            onClick = { mode = option },
                            modifier = Modifier.weight(1f)
                        ) {
                            Text(option.label, modifier = Modifier.fillMaxWidth())
                        }
                    }
                }
                if (mode == QuietMode.SCHEDULE) {
                    Spacer(Modifier.height(10.dp))
                    Text("Ежедневно", style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold)
                    Row(
                        Modifier.fillMaxWidth(),
                        horizontalArrangement = Arrangement.spacedBy(12.dp)
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("Начало", style = MaterialTheme.typography.labelSmall)
                            OutlinedButton(onClick = {
                                TimePickerDialog(context, { _, hour, minute ->
                                    from = hour * 60 + minute
                                }, from / 60, from % 60, true).show()
                            }) { Text(formatQuietMinute(from)) }
                        }
                        Column(Modifier.weight(1f)) {
                            Text("Конец", style = MaterialTheme.typography.labelSmall)
                            OutlinedButton(onClick = {
                                TimePickerDialog(context, { _, hour, minute ->
                                    to = hour * 60 + minute
                                }, to / 60, to % 60, true).show()
                            }) { Text(formatQuietMinute(to)) }
                        }
                    }
                    Text(
                        if (from == to)
                            "Одинаковое время означает тишину круглосуточно."
                        else "Интервал через полночь поддерживается автоматически.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(10.dp))
                    Row(
                        Modifier.fillMaxWidth(),
                        verticalAlignment = Alignment.CenterVertically
                    ) {
                        Column(Modifier.weight(1f)) {
                            Text("Срочные — со звуком",
                                style = MaterialTheme.typography.bodyMedium)
                            Text(
                                "Только приоритет urgent; системный DND сохраняется.",
                                style = MaterialTheme.typography.bodySmall,
                                color = MaterialTheme.colorScheme.onSurfaceVariant
                            )
                        }
                        Switch(checked = allowUrgent, onCheckedChange = { allowUrgent = it })
                    }
                }
                if (mode == QuietMode.INHERIT) {
                    Text(
                        "Будет действовать расписание сервера, а для него — общее расписание.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                }
            }
        },
        confirmButton = {
            Button(onClick = {
                onSave(
                    QuietRule(mode = mode, startMinutes = from,
                        endMinutes = to, allowUrgent = allowUrgent)
                )
            }) { Text("Сохранить") }
        },
        dismissButton = {
            TextButton(onClick = onDismiss) { Text("Отмена") }
        }
    )
}
