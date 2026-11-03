package top.mirv.chimeport

import androidx.compose.foundation.layout.*
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
                Text("ChimePort · Android alpha 0.1.0",
                    style = MaterialTheme.typography.bodyMedium)
                Text(
                    "Все серверы независимы, сообщения хранятся у владельца сервера, " +
                        "а на этом устройстве — локальная копия истории.",
                    color = MaterialTheme.colorScheme.onSurfaceVariant
                )
                HorizontalDivider()
                Text(
                    "В этой версии уведомления обновляются, когда приложение открыто, " +
                        "либо по нажатию «Обновить». Звуки и доставка в фоне появятся " +
                        "в следующем этапе. Это пока не замена тревожной сигнализации.",
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
