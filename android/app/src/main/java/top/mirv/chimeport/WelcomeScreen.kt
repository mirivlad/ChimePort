package top.mirv.chimeport

import androidx.compose.foundation.background
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.ArrowBack
import androidx.compose.material.icons.outlined.CheckCircle
import androidx.compose.material.icons.outlined.NotificationsActive
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.text.style.TextAlign
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import top.mirv.chimeport.data.AppState

@Composable
fun WelcomeScreen(state: AppState, model: ChimeViewModel) {
    var address by rememberSaveable { mutableStateOf("") }
    var username by rememberSaveable { mutableStateOf("") }
    var password by remember { mutableStateOf("") }
    var label by rememberSaveable { mutableStateOf("Дом") }
    val canCancel = state.servers.isNotEmpty()

    Column(
        Modifier.fillMaxSize().verticalScroll(rememberScrollState())
            .padding(horizontal = 24.dp, vertical = 22.dp),
        horizontalAlignment = Alignment.CenterHorizontally
    ) {
        if (canCancel) {
            Row(Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically) {
                TextButton(onClick = model::cancelAdd) {
                    Icon(Icons.Outlined.ArrowBack, null)
                    Spacer(Modifier.width(6.dp))
                    Text("К серверам")
                }
            }
        }
        Spacer(Modifier.height(18.dp))
        Box(
            Modifier.size(92.dp)
                .background(
                    Brush.linearGradient(
                        listOf(MaterialTheme.colorScheme.primary,
                            MaterialTheme.colorScheme.secondary)
                    ),
                    RoundedCornerShape(28.dp)
                ),
            contentAlignment = Alignment.Center
        ) {
            Icon(
                Icons.Outlined.NotificationsActive, null,
                tint = MaterialTheme.colorScheme.onPrimary,
                modifier = Modifier.size(45.dp)
            )
        }
        Spacer(Modifier.height(22.dp))
        Text(
            "ChimePort", fontWeight = FontWeight.Black,
            fontSize = 34.sp, letterSpacing = (-1.2).sp
        )
        Spacer(Modifier.height(6.dp))
        Text(
            "Все важные сигналы — в одном месте.",
            style = MaterialTheme.typography.bodyLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center
        )
        Spacer(Modifier.height(24.dp))

        ElevatedCard(
            modifier = Modifier.fillMaxWidth(),
            shape = RoundedCornerShape(24.dp),
            colors = CardDefaults.elevatedCardColors(
                containerColor = MaterialTheme.colorScheme.surface
            )
        ) {
            Column(
                Modifier.fillMaxWidth().padding(22.dp),
                verticalArrangement = Arrangement.spacedBy(15.dp)
            ) {
                Text(
                    if (state.probed == null) "Подключить свой сервер" else
                        if (state.probedKind == "tms") "Вход через TMS" else "Войти в аккаунт",
                    style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold
                )
                if (state.probed == null) {
                    Text(
                        "Введите адрес сервера ChimePort или TMS. Для TMS отдельная учётная запись ChimePort не нужна.",
                        style = MaterialTheme.typography.bodyMedium,
                        color = MaterialTheme.colorScheme.onSurfaceVariant
                    )
                    OutlinedTextField(
                        address, { address = it; model.clearProbe() },
                        label = { Text("Адрес сервера") },
                        placeholder = { Text("https://chime.mirv.top") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Button(
                        enabled = !state.checking && address.isNotBlank(),
                        onClick = { model.verifyServer(address) },
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        if (state.checking) {
                            CircularProgressIndicator(
                                Modifier.size(18.dp), strokeWidth = 2.dp
                            )
                            Spacer(Modifier.width(10.dp))
                        }
                        Text("Проверить подключение")
                    }
                } else {
                    Surface(
                        color = MaterialTheme.colorScheme.primaryContainer,
                        shape = RoundedCornerShape(12.dp),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        Row(Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                            Icon(Icons.Outlined.CheckCircle, null,
                                tint = MaterialTheme.colorScheme.primary)
                            Spacer(Modifier.width(10.dp))
                            Column(Modifier.weight(1f)) {
                                Text(if (state.probedKind == "tms") "Сервер TMS найден" else "Сервер найден", fontWeight = FontWeight.Bold)
                                Text(
                                    state.probedOrigin,
                                    style = MaterialTheme.typography.bodySmall
                                )
                            }
                        }
                    }
                    OutlinedTextField(
                        label, { label = it },
                        label = { Text("Название для себя") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        username, { username = it },
                        label = { Text("Логин") },
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    OutlinedTextField(
                        password, { password = it },
                        label = { Text("Пароль") },
                        visualTransformation = PasswordVisualTransformation(),
                        singleLine = true,
                        modifier = Modifier.fillMaxWidth()
                    )
                    Button(
                        onClick = {
                            model.connect(username, password, label)
                            password = ""
                        },
                        enabled = !state.busy && username.isNotBlank() && password.isNotBlank(),
                        modifier = Modifier.fillMaxWidth()
                    ) {
                        if (state.busy) {
                            CircularProgressIndicator(Modifier.size(18.dp), strokeWidth = 2.dp)
                            Spacer(Modifier.width(10.dp))
                        }
                        Text(if (state.probedKind == "tms") "Войти через TMS" else "Войти в ChimePort")
                    }
                    TextButton(onClick = model::clearProbe) { Text("Изменить адрес сервера") }
                }
                if (state.loginError != null) {
                    Text(
                        state.loginError,
                        color = MaterialTheme.colorScheme.error,
                        style = MaterialTheme.typography.bodyMedium
                    )
                }
            }
        }
        Spacer(Modifier.height(24.dp))
        Text(
            "Сообщения остаются на вашем сервере. Приложение хранит локальную копию " +
                "для просмотра без интернета.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
            textAlign = TextAlign.Center
        )
    }
}
