package top.mirv.chimeport

import android.app.Activity
import android.content.ActivityNotFoundException
import android.content.Context
import android.content.Intent
import android.net.Uri
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import android.media.MediaPlayer
import android.media.RingtoneManager
import androidx.compose.foundation.clickable
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
import top.mirv.chimeport.data.ChannelInfo

@Composable
fun AlertSettingsScreen(
    state: AppState,
    model: ChimeViewModel,
    onBack: () -> Unit,
    modifier: Modifier = Modifier
) {
    val context = LocalContext.current
    var selected by remember { mutableStateOf<ChannelInfo?>(null) }
    Column(modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 8.dp, end = 20.dp, top = 12.dp, bottom = 8.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = onBack) { Icon(Icons.Outlined.ArrowBack, "Назад") }
            Column(Modifier.weight(1f)) {
                Text("Оповещения", style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold)
                Text("Звуки и приоритеты по каналам",
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall)
            }
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(start = 18.dp, end = 18.dp, bottom = 30.dp),
            verticalArrangement = Arrangement.spacedBy(10.dp)
        ) {
            item {
                Text(
                    "Каждый сервер и канал настраивается отдельно. Android может изменять " +
                        "звук и вибрацию через системные настройки. Режим «Не беспокоить» сохраняется.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                Spacer(Modifier.height(10.dp))
            }
            state.servers.forEach { server ->
                item(key = "server-" + server.id) {
                    Row(Modifier.fillMaxWidth().padding(top = 8.dp, bottom = 5.dp),
                        verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.Dns, null, tint = MaterialTheme.colorScheme.primary)
                        Spacer(Modifier.width(9.dp))
                        Text(server.label, style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold)
                    }
                }
                val channels = state.channels.filter { it.serverId == server.id }
                if (channels.isEmpty()) {
                    item(key = "empty-" + server.id) {
                        Text("Пока нет каналов. Обновите сообщения на сервере.",
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                items(channels, key = { it.serverId + "::" + it.slug }) { channel ->
                    val profile = remember(
                        channel.serverId, channel.slug, state.alertSettingsRevision
                    ) { AlertProfiles.get(context, channel.serverId, channel.slug) }
                    ElevatedCard(onClick = { selected = channel }) {
                        Row(
                            Modifier.fillMaxWidth().padding(horizontal = 16.dp, vertical = 15.dp),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            Icon(Icons.Outlined.NotificationsActive, null,
                                tint = MaterialTheme.colorScheme.primary)
                            Spacer(Modifier.width(13.dp))
                            Column(Modifier.weight(1f)) {
                                Text(channel.name, fontWeight = FontWeight.SemiBold,
                                    style = MaterialTheme.typography.titleMedium)
                                Spacer(Modifier.height(3.dp))
                                Text(
                                    profile.sound.label + " · " +
                                        profile.level.label.lowercase() +
                                        if (profile.vibrate) " · вибрация" else "",
                                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                                    style = MaterialTheme.typography.bodySmall
                                )
                            }
                            Icon(Icons.Outlined.ChevronRight, "Настроить",
                                tint = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                    }
                }
            }
        }
    }
    selected?.let { channel ->
        val saved = remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            AlertProfiles.get(context, channel.serverId, channel.slug)
        }
        var sound by remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            mutableStateOf(saved.sound)
        }
        var level by remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            mutableStateOf(saved.level)
        }
        var vibrate by remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            mutableStateOf(saved.vibrate)
        }
        var ringtoneUri by remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            mutableStateOf(saved.ringtoneUri)
        }
        var ringtoneTitle by remember(channel.serverId, channel.slug, state.alertSettingsRevision) {
            mutableStateOf(saved.ringtoneTitle)
        }
        var pickerError by remember(channel.serverId, channel.slug) {
            mutableStateOf<String?>(null)
        }
        val picker = rememberLauncherForActivityResult(
            ActivityResultContracts.StartActivityForResult()
        ) { result ->
            if (result.resultCode == Activity.RESULT_OK) {
                @Suppress("DEPRECATION")
                val chosen: Uri? = result.data?.getParcelableExtra(
                    RingtoneManager.EXTRA_RINGTONE_PICKED_URI
                )
                when {
                    chosen == null ->
                        pickerError = "Мелодия не выбрана. Для тишины есть отдельный пункт."
                    chosen.scheme != "content" && chosen.scheme != "android.resource" ->
                        pickerError = "Android вернул неподдерживаемый адрес мелодии."
                    else -> {
                        ringtoneUri = chosen.toString()
                        ringtoneTitle = runCatching {
                            RingtoneManager.getRingtone(context, chosen)?.getTitle(context)
                                ?.takeIf { it.isNotBlank() }
                        }.getOrNull() ?: "Выбранный звук телефона"
                        sound = AlertSound.DEVICE
                        pickerError = null
                    }
                }
            }
        }
        val launchPicker: () -> Unit = {
            pickerError = null
            val current = ringtoneUri.takeIf { it.isNotBlank() }?.let(Uri::parse)
                ?: RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
            val intent = Intent(RingtoneManager.ACTION_RINGTONE_PICKER).apply {
                putExtra(RingtoneManager.EXTRA_RINGTONE_TYPE, RingtoneManager.TYPE_NOTIFICATION)
                putExtra(RingtoneManager.EXTRA_RINGTONE_TITLE, "Звук канала «" + channel.name + "»")
                putExtra(RingtoneManager.EXTRA_RINGTONE_SHOW_DEFAULT, true)
                putExtra(RingtoneManager.EXTRA_RINGTONE_SHOW_SILENT, false)
                putExtra(
                    RingtoneManager.EXTRA_RINGTONE_DEFAULT_URI,
                    RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
                )
                putExtra(RingtoneManager.EXTRA_RINGTONE_EXISTING_URI, current)
            }
            try {
                picker.launch(intent)
            } catch (_: ActivityNotFoundException) {
                pickerError = "На этом устройстве нет системного выбора звуков."
            } catch (_: SecurityException) {
                pickerError = "Android не разрешил открыть список звуков."
            }
        }
        AlertDialog(
            onDismissRequest = { selected = null },
            title = { Text(channel.name) },
            text = {
                Column(
                    Modifier.heightIn(max = 445.dp).verticalScroll(rememberScrollState())
                ) {
                    Text(channel.serverLabel + " · " + channel.slug,
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                    Spacer(Modifier.height(14.dp))
                    Text("Звук", style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold)
                    AlertSound.entries.forEach { option ->
                        val choose: () -> Unit = {
                            if (option == AlertSound.DEVICE) launchPicker() else sound = option
                        }
                        Row(
                            Modifier.fillMaxWidth().clickable(onClick = choose),
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            RadioButton(selected = sound == option, onClick = choose)
                            Column(Modifier.weight(1f)) {
                                Text(option.label)
                                if (option == AlertSound.DEVICE && ringtoneUri.isNotBlank()) {
                                    Text(
                                        ringtoneTitle.ifBlank { "Выбранный звук телефона" },
                                        style = MaterialTheme.typography.labelSmall,
                                        color = MaterialTheme.colorScheme.onSurfaceVariant
                                    )
                                }
                            }
                            when (option) {
                                AlertSound.DEVICE -> {
                                    IconButton(onClick = launchPicker) {
                                        Icon(Icons.Outlined.LibraryMusic, "Выбрать звук телефона")
                                    }
                                    if (ringtoneUri.isNotBlank()) {
                                        IconButton(onClick = {
                                            previewAlert(context, option, ringtoneUri)
                                        }) {
                                            Icon(Icons.Outlined.PlayArrow, "Прослушать")
                                        }
                                    }
                                }
                                AlertSound.SILENT -> Unit
                                else -> IconButton(onClick = { previewAlert(context, option) }) {
                                    Icon(Icons.Outlined.PlayArrow, "Прослушать " + option.label)
                                }
                            }
                        }
                    }
                    pickerError?.let { error ->
                        Text(
                            error, style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.error
                        )
                    }
                    Spacer(Modifier.height(8.dp))
                    Text("Приоритет", style = MaterialTheme.typography.titleSmall,
                        fontWeight = FontWeight.Bold)
                    AlertLevel.entries.forEach { option ->
                        Row(
                            Modifier.fillMaxWidth().clickable { level = option },
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            RadioButton(selected = level == option, onClick = { level = option })
                            Text(option.label)
                        }
                    }
                    Text(
                        "Тихие оповещения не воспроизводят звук и не вибрируют. " +
                            "Важные могут появляться поверх других приложений, " +
                            "если Android это разрешает.",
                        style = MaterialTheme.typography.bodySmall,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    Spacer(Modifier.height(8.dp))
                    Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                        Text("Вибрация", Modifier.weight(1f),
                            style = MaterialTheme.typography.bodyMedium)
                        Switch(checked = vibrate, onCheckedChange = { vibrate = it })
                    }
                }
            },
            confirmButton = {
                Button(onClick = {
                    model.configureAlert(
                        channel, AlertProfile(
                            sound = sound, vibrate = vibrate, level = level,
                            ringtoneUri = ringtoneUri, ringtoneTitle = ringtoneTitle
                        )
                    )
                    selected = null
                }) { Text("Сохранить") }
            },
            dismissButton = {
                TextButton(onClick = { selected = null }) { Text("Отмена") }
            }
        )
    }
}

private fun previewAlert(context: Context, choice: AlertSound, deviceUri: String = "") {
    runCatching {
        when (choice) {
            AlertSound.SILENT -> Unit
            AlertSound.DEVICE -> {
                if (deviceUri.isNotBlank()) {
                    RingtoneManager.getRingtone(context, Uri.parse(deviceUri))?.play()
                }
            }
            AlertSound.SYSTEM -> {
                RingtoneManager.getRingtone(
                    context, RingtoneManager.getDefaultUri(RingtoneManager.TYPE_NOTIFICATION)
                )?.play()
            }
            AlertSound.CHIME, AlertSound.SIGNAL -> {
                val resource = if (choice == AlertSound.CHIME) R.raw.chime_soft else R.raw.signal_pulse
                val player = MediaPlayer.create(context, resource) ?: return
                player.setOnCompletionListener { it.release() }
                player.setOnErrorListener { mp, _, _ -> mp.release(); true }
                player.start()
            }
        }
    }
}
