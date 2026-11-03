package top.mirv.chimeport

import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState
import top.mirv.chimeport.data.ServerAccount

@Composable
fun ServersScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    var edit by remember { mutableStateOf<ServerAccount?>(null) }
    var reauth by remember { mutableStateOf<ServerAccount?>(null) }
    var confirmRemove by remember { mutableStateOf<ServerAccount?>(null) }

    Column(modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 20.dp, end = 16.dp, top = 22.dp, bottom = 18.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            PageTitle(
                "Серверы", state.servers.size.toString() + " подключений",
                Modifier.weight(1f)
            )
            FilledTonalButton(onClick = model::openAdd) {
                Icon(Icons.Outlined.Add, null)
                Spacer(Modifier.width(5.dp))
                Text("Добавить")
            }
        }
        LazyColumn(
            contentPadding = PaddingValues(horizontal = 16.dp, vertical = 6.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            items(state.servers, key = { it.id }) { server ->
                ElevatedCard(
                    shape = RoundedCornerShape(19.dp),
                    colors = CardDefaults.elevatedCardColors(
                        containerColor = MaterialTheme.colorScheme.surface
                    )
                ) {
                    Column(
                        Modifier.fillMaxWidth().padding(19.dp),
                        verticalArrangement = Arrangement.spacedBy(10.dp)
                    ) {
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Surface(
                                color = MaterialTheme.colorScheme.primaryContainer,
                                shape = RoundedCornerShape(13.dp),
                                modifier = Modifier.size(46.dp)
                            ) {
                                Box(contentAlignment = Alignment.Center) {
                                    Icon(Icons.Outlined.Dns, null,
                                        tint = MaterialTheme.colorScheme.primary)
                                }
                            }
                            Spacer(Modifier.width(13.dp))
                            Column(Modifier.weight(1f)) {
                                Text(server.label, style = MaterialTheme.typography.titleMedium,
                                    fontWeight = FontWeight.Bold)
                                Text(server.origin, style = MaterialTheme.typography.bodySmall,
                                    color = MaterialTheme.colorScheme.onSurfaceVariant)
                            }
                            IconButton(onClick = { edit = server }) {
                                Icon(Icons.Outlined.Edit, "Переименовать сервер")
                            }
                        }
                        Text(
                            "Пользователь: " + server.username,
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                        Text(
                            if (server.lastSync > 0)
                                "Синхронизировано: " +
                                    java.text.DateFormat.getDateTimeInstance(
                                        java.text.DateFormat.SHORT,
                                        java.text.DateFormat.SHORT
                                    ).format(java.util.Date(server.lastSync))
                            else "Ещё не синхронизировалось",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant
                        )
                        HorizontalDivider()
                        Row(
                            Modifier.fillMaxWidth(),
                            horizontalArrangement = Arrangement.SpaceBetween,
                            verticalAlignment = Alignment.CenterVertically
                        ) {
                            TextButton(onClick = { model.filterServer(server.id) }) {
                                Icon(Icons.Outlined.Inbox, null, Modifier.size(18.dp))
                                Spacer(Modifier.width(4.dp))
                                Text("Входящие")
                            }
                            TextButton(onClick = { reauth = server }) {
                                Icon(Icons.Outlined.Login, null, Modifier.size(18.dp))
                                Spacer(Modifier.width(4.dp))
                                Text("Войти снова")
                            }
                            IconButton(onClick = { confirmRemove = server }) {
                                Icon(
                                    Icons.Outlined.DeleteOutline, "Удалить подключение",
                                    tint = MaterialTheme.colorScheme.error
                                )
                            }
                        }
                    }
                }
            }
        }
    }
    edit?.let { server ->
        var value by remember(server.id) { mutableStateOf(server.label) }
        AlertDialog(
            onDismissRequest = { edit = null },
            title = { Text("Название сервера") },
            text = {
                OutlinedTextField(
                    value, { value = it },
                    label = { Text("Метка подключения") },
                    singleLine = true
                )
            },
            confirmButton = {
                Button(enabled = value.isNotBlank(), onClick = {
                    model.renameServer(server.id, value)
                    edit = null
                }) { Text("Сохранить") }
            },
            dismissButton = { TextButton(onClick = { edit = null }) { Text("Отмена") } }
        )
    }
    reauth?.let { server ->
        var password by remember(server.id) { mutableStateOf("") }
        AlertDialog(
            onDismissRequest = { reauth = null },
            title = { Text("Повторный вход") },
            text = {
                Column {
                    Text("Сервер: " + server.origin + "\nУчётная запись: " + server.username)
                    Spacer(Modifier.height(12.dp))
                    OutlinedTextField(
                        value = password,
                        onValueChange = { password = it },
                        label = { Text("Пароль") },
                        visualTransformation = PasswordVisualTransformation(),
                        singleLine = true
                    )
                }
            },
            confirmButton = {
                Button(enabled = password.isNotBlank() && !state.busy, onClick = {
                    model.relogin(server.id, password)
                    reauth = null
                }) { Text("Войти") }
            },
            dismissButton = { TextButton(onClick = { reauth = null }) { Text("Отмена") } }
        )
    }
    confirmRemove?.let { server ->
        AlertDialog(
            onDismissRequest = { confirmRemove = null },
            title = { Text("Удалить подключение?") },
            text = {
                Text(
                    "Локальная история и сессия «" + server.label +
                        "» будут удалены с телефона. Сообщения на сервере останутся."
                )
            },
            confirmButton = {
                Button(onClick = {
                    model.removeServer(server.id)
                    confirmRemove = null
                }) { Text("Удалить") }
            },
            dismissButton = { TextButton(onClick = { confirmRemove = null }) { Text("Отмена") } }
        )
    }
}
