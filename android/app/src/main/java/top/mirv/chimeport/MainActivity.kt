package top.mirv.chimeport

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Build
import android.os.Bundle
import android.content.Intent
import android.net.Uri
import kotlinx.coroutines.flow.MutableStateFlow
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.ui.platform.LocalContext
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.outlined.*
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.unit.dp
import androidx.compose.ui.unit.sp
import androidx.lifecycle.Lifecycle
import androidx.lifecycle.LifecycleEventObserver
import androidx.lifecycle.compose.LocalLifecycleOwner
import androidx.lifecycle.viewmodel.compose.viewModel
import top.mirv.chimeport.data.*

private val Ocean = Color(0xFF0B1828)
private val Aqua = Color(0xFF60D4C2)
private val Cloud = Color(0xFFF4F7FB)

private val lightColors = lightColorScheme(
    primary = Color(0xFF126B73), onPrimary = Color.White,
    primaryContainer = Color(0xFFD9F4F0), onPrimaryContainer = Color(0xFF064A51),
    secondary = Color(0xFF396684), background = Cloud,
    surface = Color.White, surfaceVariant = Color(0xFFE6EDF3),
    onSurface = Color(0xFF172539), onSurfaceVariant = Color(0xFF607080),
    outline = Color(0xFFC4D4DF)
)
private val darkColors = darkColorScheme(
    primary = Aqua, onPrimary = Color(0xFF092C33),
    primaryContainer = Color(0xFF1C4551), onPrimaryContainer = Color(0xFFC8F6EF),
    secondary = Color(0xFF90BFE2), background = Ocean,
    surface = Color(0xFF14263A), surfaceVariant = Color(0xFF21364B),
    onSurface = Color(0xFFE9F3F8), onSurfaceVariant = Color(0xFFA3BBCB),
    outline = Color(0xFF456075)
)

class MainActivity : ComponentActivity() {
    private val notificationIntent = MutableStateFlow<Pair<String, String>?>(null)

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        notificationIntent.value = extractNotice(intent)
        LiveNotifications.ensureChannel(this)
        setContent {
            val model: ChimeViewModel = viewModel()
            val target by notificationIntent.collectAsState()
            LaunchedEffect(target) {
                target?.let { (serverId, messageId) ->
                    model.openNotification(serverId, messageId)
                    notificationIntent.value = null
                }
            }
            val state by model.state.collectAsState()
            val dark = when (state.theme) {
                ThemeChoice.DARK -> true
                ThemeChoice.LIGHT -> false
                ThemeChoice.SYSTEM -> isSystemInDarkTheme()
            }
            MaterialTheme(colorScheme = if (dark) darkColors else lightColors) {
                Surface(color = MaterialTheme.colorScheme.background, modifier = Modifier.fillMaxSize()) {
                    ChimeApp(model, state)
                }
            }
        }
    }

    override fun onNewIntent(intent: Intent) {
        super.onNewIntent(intent)
        setIntent(intent)
        notificationIntent.value = extractNotice(intent)
    }

    private fun extractNotice(intent: Intent?): Pair<String, String>? {
        if (intent?.action != Intent.ACTION_VIEW) return null
        val uri: Uri = intent.data ?: return null
        if (uri.scheme != "chimeport" || uri.authority != "notice") return null
        val parts = uri.pathSegments
        if (parts.size != 2 || parts[0].isBlank() ||
            parts[1].isEmpty() || !parts[1].all { it.isDigit() }) return null
        return parts[0] to parts[1]
    }
}

@Composable
private fun ChimeApp(model: ChimeViewModel, state: AppState) {
    val owner = LocalLifecycleOwner.current
    val context = LocalContext.current
    val requestNotifications = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) { /* Permission status is also available in Android system settings. */ }
    LaunchedEffect(state.servers.isNotEmpty()) {
        val prefs = context.getSharedPreferences("chimeport_preferences", Context.MODE_PRIVATE)
        if (state.servers.isNotEmpty() && Build.VERSION.SDK_INT >= 33 &&
            context.checkSelfPermission(Manifest.permission.POST_NOTIFICATIONS) != PackageManager.PERMISSION_GRANTED &&
            !prefs.getBoolean("notification_permission_prompted", false)
        ) {
            prefs.edit().putBoolean("notification_permission_prompted", true).apply()
            requestNotifications.launch(Manifest.permission.POST_NOTIFICATIONS)
        }
    }
    DisposableEffect(owner, model) {
        val observer = LifecycleEventObserver { _, event ->
            when (event) {
                Lifecycle.Event.ON_START -> model.startRealtime()
                Lifecycle.Event.ON_STOP -> model.stopRealtime()
                Lifecycle.Event.ON_RESUME -> model.refresh()
                else -> Unit
            }
        }
        owner.lifecycle.addObserver(observer)
        if (owner.lifecycle.currentState.isAtLeast(Lifecycle.State.STARTED)) model.startRealtime()
        onDispose {
            owner.lifecycle.removeObserver(observer)
            model.stopRealtime()
        }
    }
    if (state.servers.isEmpty() || state.addingServer) {
        WelcomeScreen(state, model)
    } else {
        Dashboard(state, model)
    }
}

@Composable
private fun Dashboard(state: AppState, model: ChimeViewModel) {
    Scaffold(
        topBar = { ChimeTopBar(state, model) },
        bottomBar = {
            NavigationBar(containerColor = MaterialTheme.colorScheme.surface) {
                NavItem(Tab.INBOX, "Входящие", state.tab, Icons.Outlined.Notifications, model)
                NavItem(Tab.SEARCH, "Поиск", state.tab, Icons.Outlined.Search, model)
                NavItem(Tab.SERVERS, "Серверы", state.tab, Icons.Outlined.Dns, model)
                NavItem(Tab.SETTINGS, "Настройки", state.tab, Icons.Outlined.Tune, model)
            }
        }
    ) { padding ->
        Column(modifier = Modifier.fillMaxSize().padding(padding)) {
            if (state.syncError != null) {
                Surface(color = MaterialTheme.colorScheme.errorContainer,
                    modifier = Modifier.fillMaxWidth()) {
                    Row(Modifier.padding(horizontal = 16.dp, vertical = 10.dp),
                        verticalAlignment = Alignment.CenterVertically) {
                        Icon(Icons.Outlined.CloudOff, null,
                            tint = MaterialTheme.colorScheme.onErrorContainer,
                            modifier = Modifier.size(20.dp))
                        Spacer(Modifier.width(8.dp))
                        Text(state.syncError, modifier = Modifier.weight(1f),
                            style = MaterialTheme.typography.bodySmall,
                            color = MaterialTheme.colorScheme.onErrorContainer)
                        TextButton(onClick = model::refresh) { Text("Повторить") }
                    }
                }
            }
            if (state.message != null) {
                Surface(color = MaterialTheme.colorScheme.primaryContainer,
                    modifier = Modifier.fillMaxWidth()) {
                    Row(Modifier.padding(12.dp), verticalAlignment = Alignment.CenterVertically) {
                        Text(state.message, modifier = Modifier.weight(1f),
                            style = MaterialTheme.typography.bodySmall)
                        IconButton(onClick = model::dismissMessage) {
                            Icon(Icons.Outlined.Close, "Закрыть сообщение")
                        }
                    }
                }
            }
            when (state.tab) {
                Tab.INBOX -> InboxScreen(state, model, Modifier.weight(1f))
                Tab.SEARCH -> SearchScreen(state, model, Modifier.weight(1f))
                Tab.SERVERS -> ServersScreen(state, model, Modifier.weight(1f))
                Tab.SETTINGS -> SettingsScreen(state, model, Modifier.weight(1f))
            }
        }
    }
    state.openNotificationKey?.let { key ->
        state.messages.firstOrNull { it.key == key }?.let { notice ->
            NoticeDetail(notice, onDismiss = model::closeNotification) { read ->
                model.setReadState(notice, read)
                model.closeNotification()
            }
        }
    }
}

@Composable
private fun ChimeTopBar(state: AppState, model: ChimeViewModel) {
    Surface(color = MaterialTheme.colorScheme.surface,
        tonalElevation = 3.dp, shadowElevation = 1.dp) {
        Row(Modifier.fillMaxWidth().padding(horizontal = 18.dp, vertical = 10.dp),
            verticalAlignment = Alignment.CenterVertically) {
            Surface(color = MaterialTheme.colorScheme.primary, shape = RoundedCornerShape(14.dp),
                modifier = Modifier.size(42.dp)) {
                Box(contentAlignment = Alignment.Center) {
                    Text("C", color = MaterialTheme.colorScheme.onPrimary,
                        fontWeight = FontWeight.Black, fontSize = 23.sp)
                }
            }
            Spacer(Modifier.width(12.dp))
            Column(Modifier.weight(1f)) {
                Text("ChimePort", style = MaterialTheme.typography.titleLarge,
                    fontWeight = FontWeight.Bold, letterSpacing = (-0.5).sp)
                val connected = state.servers.count {
                    state.liveStatuses[it.id] == LiveStatus.CONNECTED
                }
                val liveLabel = when {
                    state.servers.isEmpty() -> "Свои сигналы. Свой сервер."
                    state.backgroundLive && BackgroundLiveService.running ->
                        "Фоновая доставка включена"
                    state.backgroundLive -> "Запуск фонового подключения…"
                    connected == state.servers.size -> "На связи · обновления автоматически"
                    state.liveStatuses.values.any { it == LiveStatus.LOGIN_REQUIRED } ->
                        "Требуется повторный вход"
                    state.liveStatuses.values.any { it == LiveStatus.RECONNECTING } ->
                        "Нет связи · переподключаемся"
                    else -> "Подключение к серверу…"
                }
                Text(liveLabel,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                    style = MaterialTheme.typography.labelSmall)
            }
            if (state.busy) {
                CircularProgressIndicator(Modifier.size(21.dp), strokeWidth = 2.dp)
            } else {
                IconButton(onClick = model::refresh) {
                    Icon(Icons.Outlined.Refresh, "Синхронизировать")
                }
            }
        }
    }
}

@Composable
private fun RowScope.NavItem(
    tab: Tab, label: String, selected: Tab,
    icon: androidx.compose.ui.graphics.vector.ImageVector, model: ChimeViewModel
) {
    NavigationBarItem(
        selected = tab == selected,
        onClick = { model.openTab(tab) },
        icon = { Icon(icon, contentDescription = label) },
        label = { Text(label, maxLines = 1) },
        alwaysShowLabel = true
    )
}
