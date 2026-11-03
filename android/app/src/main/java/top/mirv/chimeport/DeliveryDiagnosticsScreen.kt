package top.mirv.chimeport

import android.app.NotificationManager
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.PowerManager
import android.provider.Settings
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import top.mirv.chimeport.data.AppState

private fun deliveryTime(epochMs: Long): String =
    if (epochMs <= 0L) "нет данных" else
        SimpleDateFormat("dd.MM.yyyy HH:mm:ss", Locale.getDefault()).format(Date(epochMs))

@Composable
fun DeliveryDiagnosticsScreen(
    state: AppState, model: ChimeViewModel, onBack: () -> Unit,
    modifier: Modifier = Modifier
) {
    val context = LocalContext.current
    var refresh by remember { mutableIntStateOf(0) }
    val running = BackgroundLiveService.running
    val notificationsAllowed = context.getSystemService(NotificationManager::class.java)
        .areNotificationsEnabled()
    val power = context.getSystemService(PowerManager::class.java)
    val batteryUnrestricted = power.isIgnoringBatteryOptimizations(context.packageName)
    val network = context.getSystemService(ConnectivityManager::class.java)
    val capabilities = network.getNetworkCapabilities(network.activeNetwork)
    val internetAvailable = capabilities?.hasCapability(
        NetworkCapabilities.NET_CAPABILITY_VALIDATED
    ) == true
    val boot = remember(refresh) { ConnectionDiagnostics.bootStatus(context) }
    val serviceStarted = remember(refresh) { ConnectionDiagnostics.serviceStartTime(context) }

    Column(modifier.fillMaxSize()) {
        Row(
            Modifier.fillMaxWidth().padding(start = 8.dp, end = 16.dp, top = 10.dp, bottom = 4.dp),
            verticalAlignment = Alignment.CenterVertically
        ) {
            IconButton(onClick = onBack) { Icon(Icons.Outlined.ArrowBack, "Назад") }
            Column(Modifier.weight(1f)) {
                Text("Диагностика доставки", style = MaterialTheme.typography.headlineSmall,
                    fontWeight = FontWeight.Bold)
                Text("Подключение, переподключения и состояние Android",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
        }
        LazyColumn(
            modifier = Modifier.weight(1f),
            contentPadding = PaddingValues(horizontal = 18.dp, vertical = 10.dp),
            verticalArrangement = Arrangement.spacedBy(12.dp)
        ) {
            item {
                ElevatedCard {
                    Column(Modifier.fillMaxWidth().padding(16.dp)) {
                        Text("Фоновая доставка",
                            style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                        Spacer(Modifier.height(6.dp))
                        Text(
                            if (!state.backgroundLive) "Выключена пользователем"
                            else if (running) "Служба ChimePort запущена"
                            else "Включена, но служба сейчас не работает"
                        )
                        Spacer(Modifier.height(7.dp))
                        Text(recommendedDeliveryAction(
                            state.backgroundLive, running,
                            state.servers.count { it.token.isNotBlank() }
                        ), style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant)
                        Spacer(Modifier.height(7.dp))
                        Text("Последний запуск службы: " + deliveryTime(serviceStarted),
                            style = MaterialTheme.typography.bodySmall)
                        Text("Последняя попытка автозапуска: " + deliveryTime(boot.first),
                            style = MaterialTheme.typography.bodySmall)
                        if (boot.second.isNotBlank()) Text(boot.second,
                            style = MaterialTheme.typography.bodySmall)
                    }
                }
            }
            item {
                ElevatedCard {
                    Column(Modifier.fillMaxWidth().padding(16.dp),
                        verticalArrangement = Arrangement.spacedBy(7.dp)) {
                        Text("Состояние телефона",
                            style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.Bold)
                        DiagnosticFact("Интернет", if (internetAvailable) "Есть доступ"
                            else "Подтверждённого доступа нет")
                        DiagnosticFact("Уведомления Android", if (notificationsAllowed)
                            "Разрешены" else "Заблокированы")
                        DiagnosticFact("Ограничения батареи", if (batteryUnrestricted)
                            "Оптимизация отключена" else "Android может ограничивать работу")
                        TextButton(onClick = {
                            runCatching {
                                context.startActivity(Intent(Settings.ACTION_IGNORE_BATTERY_OPTIMIZATION_SETTINGS)
                                    .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
                            }
                        }) {
                            Icon(Icons.Outlined.BatteryAlert, null, Modifier.size(18.dp))
                            Spacer(Modifier.width(7.dp))
                            Text("Настройки энергосбережения")
                        }
                    }
                }
            }
            item {
                Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                    Text("Серверы", style = MaterialTheme.typography.titleMedium,
                        fontWeight = FontWeight.Bold, modifier = Modifier.weight(1f))
                    TextButton(onClick = { refresh++ }) {
                        Icon(Icons.Outlined.Refresh, null, Modifier.size(18.dp))
                        Spacer(Modifier.width(5.dp))
                        Text("Обновить")
                    }
                }
            }
            if (state.servers.isEmpty()) {
                item {
                    Text("Подключённых серверов нет.",
                        color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
            }
            items(state.servers, key = { it.id }) { server ->
                val snapshot = remember(server.id, refresh) {
                    ConnectionDiagnostics.read(context, server.id)
                }
                ElevatedCard {
                    Column(Modifier.fillMaxWidth().padding(16.dp),
                        verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(server.label, style = MaterialTheme.typography.titleMedium,
                            fontWeight = FontWeight.Bold)
                        Text(if (!state.backgroundLive) "Фоновая доставка выключена" else when (snapshot.status) {
                            DeliveryStatus.CONNECTING -> "Соединение устанавливается"
                            DeliveryStatus.CONNECTED -> "Последнее состояние: на связи"
                            DeliveryStatus.RETRYING -> "Переподключение"
                            DeliveryStatus.LOGIN_REQUIRED -> "Нужен повторный вход"
                            DeliveryStatus.STOPPED -> "Соединение остановлено"
                        }, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        DiagnosticFact("Соединение", deliveryTime(snapshot.lastConnectedAt))
                        DiagnosticFact("Синхронизация", deliveryTime(snapshot.lastSyncAt))
                        DiagnosticFact("Последнее событие", deliveryTime(snapshot.lastEventAt))
                        if (snapshot.lastError.isNotEmpty()) {
                            DiagnosticFact("Последняя ошибка", snapshot.lastError +
                                " · " + deliveryTime(snapshot.lastErrorAt))
                        }
                        if (snapshot.retryAt > System.currentTimeMillis() &&
                            state.backgroundLive && snapshot.status == DeliveryStatus.RETRYING) {
                            Text("Следующая попытка: " + deliveryTime(snapshot.retryAt),
                                style = MaterialTheme.typography.bodySmall)
                        }
                    }
                }
            }
            item {
                Spacer(Modifier.height(5.dp))
                Button(
                    onClick = {
                        model.reconnectBackground()
                        refresh++
                    },
                    enabled = state.backgroundLive && state.servers.isNotEmpty(),
                    modifier = Modifier.fillMaxWidth()
                ) {
                    Icon(Icons.Outlined.Refresh, null)
                    Spacer(Modifier.width(8.dp))
                    Text("Переподключить")
                }
                Spacer(Modifier.height(8.dp))
                Text(
                    "Запуск после перезагрузки возможен, когда включена фоновая доставка. " +
                        "После принудительной остановки Android не присылает события приложению, " +
                        "пока вы не откроете его снова. Ограничения прошивки и экономии батареи " +
                        "могут помешать автозапуску. Ошибки здесь не содержат паролей или токенов.",
                    style = MaterialTheme.typography.bodySmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
            }
        }
    }
}

@Composable
private fun DiagnosticFact(name: String, value: String) {
    Row(Modifier.fillMaxWidth(), horizontalArrangement = Arrangement.SpaceBetween,
        verticalAlignment = Alignment.Top) {
        Text(name, style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            modifier = Modifier.widthIn(min = 100.dp).weight(1f))
        Text(value, style = MaterialTheme.typography.bodySmall,
            modifier = Modifier.weight(1.45f))
    }
}
