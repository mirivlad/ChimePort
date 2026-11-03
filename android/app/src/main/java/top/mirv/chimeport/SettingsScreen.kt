package top.mirv.chimeport

import android.Manifest
import android.content.pm.PackageManager
import android.os.Build
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import top.mirv.chimeport.data.AppState
import top.mirv.chimeport.data.ThemeChoice

@Composable
fun SettingsScreen(state: AppState, model: ChimeViewModel, modifier: Modifier = Modifier) {
    Column(modifier.fillMaxSize().padding(horizontal = 20.dp, vertical = 24.dp)) {
        PageTitle("Настройки", "Как выглядит и работает ChimePort")
        Spacer(Modifier.height(24.dp))
        ElevatedCard(
            colors = CardDefaults.elevatedCardColors(
                containerColor = MaterialTheme.colorScheme.surface
            ),
            shape = RoundedCornerShape(18.dp)
        ) {
            Column(Modifier.fillMaxWidth().padding(18.dp)) {
                Text("Оформление", style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(8.dp))
                ThemeOption("Как в системе", ThemeChoice.SYSTEM, state.theme, model)
                ThemeOption("Светлая", ThemeChoice.LIGHT, state.theme, model)
                ThemeOption("Тёмная", ThemeChoice.DARK, state.theme, model)
            }
        }
        Spacer(Modifier.height(15.dp))
        val context = LocalContext.current
        ElevatedCard(
            colors = CardDefaults.elevatedCardColors(
                containerColor = MaterialTheme.colorScheme.surface),
            shape = RoundedCornerShape(18.dp)
        ) {
            Column(Modifier.fillMaxWidth().padding(18.dp)) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text("Фоновая доставка", style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold)
                        Spacer(Modifier.height(5.dp))
                        Text("Экспериментально: постоянное соединение с серверами, " +
                            "даже когда приложение свернуто или убрано из недавних. " +
                            "Android показывает постоянное уведомление; возможен расход батареи.",
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                    Switch(checked = state.backgroundLive,
                        onCheckedChange = model::setBackgroundLive)
                }
                Text("Принудительная остановка, ограничения прошивки или отключённый интернет " +
                    "могут прервать доставку. После перезапуска телефона откройте приложение.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        Spacer(Modifier.height(15.dp))
        var notificationsAllowed by remember {
            mutableStateOf(
                Build.VERSION.SDK_INT < 33 ||
                    context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) == PackageManager.PERMISSION_GRANTED
            )
        }
        val requestPermission = rememberLauncherForActivityResult(
            ActivityResultContracts.RequestPermission()
        ) { granted -> notificationsAllowed = granted }
        ElevatedCard(
            colors = CardDefaults.elevatedCardColors(containerColor = MaterialTheme.colorScheme.surface),
            shape = RoundedCornerShape(18.dp)
        ) {
            Column(Modifier.fillMaxWidth().padding(18.dp)) {
                Text("Системные уведомления", style = MaterialTheme.typography.titleMedium,
                    fontWeight = FontWeight.Bold)
                Spacer(Modifier.height(6.dp))
                Text(
                    if (notificationsAllowed)
                        "Разрешены. Новые события во время открытого приложения появляются в шторке Android."
                    else "Не разрешены. Можно включить, чтобы новые события появлялись в шторке.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.bodySmall
                )
                if (!notificationsAllowed && Build.VERSION.SDK_INT >= 33) {
                    Spacer(Modifier.height(10.dp))
                    Button(onClick = { requestPermission.launch(Manifest.permission.POST_NOTIFICATIONS) }) {
                        Text("Разрешить уведомления")
                    }
                }
            }
        }
        Spacer(Modifier.height(15.dp))
        ElevatedCard(
            colors = CardDefaults.elevatedCardColors(
                containerColor = MaterialTheme.colorScheme.surface
            ),
            shape = RoundedCornerShape(18.dp)
        ) {
            Column(
                Modifier.fillMaxWidth().padding(20.dp),
                verticalArrangement = Arrangement.spacedBy(9.dp)
            ) {
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Icon(Icons.Outlined.Info, null,
                        tint = MaterialTheme.colorScheme.primary)
                    Spacer(Modifier.width(10.dp))
                    Text("Об этом приложении", style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold)
                }
                Text("ChimePort · Android alpha 0.4.0",
                    style = MaterialTheme.typography.bodyMedium)
                Text(
                    "Все серверы независимы, сообщения хранятся у владельца сервера, " +
                        "а на этом устройстве — локальная копия истории.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                HorizontalDivider()
                Text(
                    "Когда приложение открыто, лента обновляется автоматически через SSE. " +
                        "В настройках можно включить экспериментальную фоновую доставку " +
                        "с постоянным уведомлением. Принудительная остановка Android " +
                        "может прервать соединение: это не аварийная сигнализация.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}

@Composable
private fun ThemeOption(
    name: String, choice: ThemeChoice, selected: ThemeChoice, model: ChimeViewModel
) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected = selected == choice, onClick = { model.theme(choice) })
        Text(name, modifier = Modifier.weight(1f),
            style = MaterialTheme.typography.bodyMedium)
    }
}
