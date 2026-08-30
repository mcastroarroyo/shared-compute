package dev.ayni.provider

import android.Manifest
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.animation.core.RepeatMode
import androidx.compose.animation.core.animateFloat
import androidx.compose.animation.core.animateFloatAsState
import androidx.compose.animation.core.infiniteRepeatable
import androidx.compose.animation.core.rememberInfiniteTransition
import androidx.compose.animation.core.tween
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.geometry.Offset
import androidx.compose.ui.geometry.Size
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.StrokeCap
import androidx.compose.ui.graphics.drawscope.Stroke
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.res.painterResource
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import kotlinx.coroutines.delay
import java.util.Locale
import kotlin.math.PI
import kotlin.math.cos
import kotlin.math.min
import kotlin.math.sin

private const val GAUGE_MAX = 40.0 // tokens/sec full-scale for a phone-class node

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContent { MaterialTheme(colorScheme = darkColorScheme()) { Surface { App() } } }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
private fun App() {
    val ctx = LocalContext.current
    val store = remember { ConfigStore(ctx) }
    var s by remember { mutableStateOf(store.load()) }
    val status by ProviderController.status.collectAsStateWithLifecycle()
    var showSettings by remember { mutableStateOf(!s.isConfigured) }

    val notifPerm = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) {}

    Column(
        Modifier.fillMaxSize().safeDrawingPadding().padding(20.dp)
            .verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(14.dp)
    ) {
        Surface(color = Color.White, shape = RoundedCornerShape(12.dp)) {
            Image(
                painter = painterResource(R.drawable.ayni_logo),
                contentDescription = "Ayni",
                modifier = Modifier
                    .height(52.dp)
                    .padding(horizontal = 16.dp, vertical = 10.dp),
            )
        }
        Text(
            "Provider node",
            style = MaterialTheme.typography.labelLarge,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        StatusPill(status)

        Tachometer(status)

        // Live earnings + throughput ledger.
        var uptime by remember { mutableStateOf("") }
        LaunchedEffect(status.startedAtMs, status.phase) {
            while (true) {
                uptime = if (status.startedAtMs == 0L) "—"
                else formatDuration(System.currentTimeMillis() - status.startedAtMs)
                delay(1000)
            }
        }
        val perHr = status.lastTps * 3600 / 1_000_000.0 * ProviderController.USD_PER_MTOK
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Stat("Est. earnings", "$" + String.format(Locale.US, "%.5f", status.earningsUsd),
                Modifier.weight(1f))
            Stat("At this rate", "$" + String.format(Locale.US, "%.4f", perHr) + "/hr",
                Modifier.weight(1f))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Stat("Tokens served", compact(status.tokensServed), Modifier.weight(1f))
            Stat("Jobs", status.jobsDone.toString(), Modifier.weight(1f))
        }
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Stat("Uptime", uptime, Modifier.weight(1f))
            Stat("Peak", String.format(Locale.US, "%.1f tok/s", status.peakTps), Modifier.weight(1f))
        }
        Text(
            "Earnings are a local estimate at $${String.format(Locale.US, "%.2f", ProviderController.USD_PER_MTOK)}/M output tokens. " +
                "Final payouts are settled by the coordinator.",
            style = MaterialTheme.typography.bodySmall,
            color = MaterialTheme.colorScheme.onSurfaceVariant,
        )

        val configured = s.isConfigured
        val running = status.phase == ProviderController.Phase.CONNECTING ||
            status.phase == ProviderController.Phase.REGISTERED ||
            status.phase == ProviderController.Phase.BLOCKED
        var confirmStop by remember { mutableStateOf(false) }
        val ayniNavy = Color(0xFF1B2A63)
        val stopRed = Color(0xFFB3261E)

        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Button(
                enabled = configured,
                colors = ButtonDefaults.buttonColors(
                    containerColor = if (running) ayniNavy else MaterialTheme.colorScheme.primary,
                    contentColor = if (running) Color.White else MaterialTheme.colorScheme.onPrimary,
                ),
                onClick = {
                    if (Build.VERSION.SDK_INT >= 33)
                        notifPerm.launch(Manifest.permission.POST_NOTIFICATIONS)
                    ProviderService.start(ctx)
                }
            ) { Text(if (running) "Sharing" else "Start") }

            Button(
                enabled = running,
                colors = ButtonDefaults.buttonColors(
                    containerColor = stopRed, contentColor = Color.White,
                    disabledContainerColor = MaterialTheme.colorScheme.surfaceVariant,
                ),
                onClick = { confirmStop = true }
            ) { Text("Stop") }
        }
        if (!configured) Text("Enter a registration token in Settings, then Start.")

        if (confirmStop) AlertDialog(
            onDismissRequest = { confirmStop = false },
            title = { Text("Stop sharing compute with Ayni?") },
            text = {
                Text(
                    "Your device will leave the Ayni network. Any job in progress is " +
                        "dropped and you stop earning until you start again."
                )
            },
            confirmButton = {
                TextButton(onClick = { confirmStop = false; ProviderService.stop(ctx) }) {
                    Text("Stop sharing", color = stopRed)
                }
            },
            dismissButton = {
                TextButton(onClick = { confirmStop = false }) { Text("Cancel") }
            },
        )

        Row(
            Modifier.fillMaxWidth(),
            verticalAlignment = Alignment.CenterVertically,
        ) {
            Text("Settings", style = MaterialTheme.typography.titleMedium, modifier = Modifier.weight(1f))
            TextButton(onClick = { showSettings = !showSettings }) {
                Text(if (showSettings) "Hide" else "Show")
            }
        }
        if (showSettings) {
            Field("Coordinator URL", s.coordinatorUrl) { s = s.copy(coordinatorUrl = it) }
            Field("Registration token", s.registrationToken) { s = s.copy(registrationToken = it) }
            Field("Model", s.model) { s = s.copy(model = it) }
            Field("Backend (mock / llama)", s.backend) { s = s.copy(backend = it) }
            Field("Manifest URL", s.manifestUrl) { s = s.copy(manifestUrl = it) }
            Field("Registry pubkey", s.registryPubkey) { s = s.copy(registryPubkey = it) }

            Toggle("Only while charging", s.onlyWhenCharging) { s = s.copy(onlyWhenCharging = it) }
            Toggle("Only on Wi-Fi", s.onlyOnWifi) { s = s.copy(onlyOnWifi = it) }
            Field(
                "Min battery %", s.minBatteryPct.toString(),
                keyboard = KeyboardType.Number
            ) { s = s.copy(minBatteryPct = it.toIntOrNull()?.coerceIn(0, 100) ?: s.minBatteryPct) }

            Button(onClick = { store.save(s) }) { Text("Save settings") }
        }
    }
}

@Composable
private fun StatusPill(status: ProviderController.Status) {
    val phase = status.phase.name.lowercase()
    ElevatedCard {
        Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
            Text("Status: $phase", style = MaterialTheme.typography.titleMedium)
            if (status.detail.isNotBlank()) Text(status.detail)
            if (status.phase == ProviderController.Phase.REGISTERED) {
                Text("provider ${status.providerId.take(8)} · tier ${status.trustTier}")
                Text(
                    if (status.jobsActive > 0) "● serving ${status.jobsActive} job(s)"
                    else "idle · ready"
                )
            }
        }
    }
}

/** Compute-sharing tachometer: needle sweeps to the last job's decode throughput. */
@Composable
private fun Tachometer(status: ProviderController.Status) {
    val registered = status.phase == ProviderController.Phase.REGISTERED
    val target = if (registered) status.lastTps.toFloat() else 0f
    val needle by animateFloatAsState(
        targetValue = target.coerceIn(0f, GAUGE_MAX.toFloat()),
        animationSpec = tween(700), label = "needle"
    )
    // Subtle "revving" wobble while a job is in flight.
    val wobble = if (status.jobsActive > 0) {
        val t = rememberInfiniteTransition(label = "rev")
        t.animateFloat(
            initialValue = -1.5f, targetValue = 1.5f,
            animationSpec = infiniteRepeatable(tween(280), RepeatMode.Reverse), label = "wob"
        ).value
    } else 0f

    val track = MaterialTheme.colorScheme.surfaceVariant
    val accent = MaterialTheme.colorScheme.primary
    val warn = Color(0xFFE0A030)
    val needleColor = MaterialTheme.colorScheme.onSurface

    val frac = (needle / GAUGE_MAX.toFloat()).coerceIn(0f, 1f)
    ElevatedCard {
        Box(
            Modifier.fillMaxWidth().height(190.dp).padding(horizontal = 16.dp, vertical = 12.dp),
            contentAlignment = Alignment.BottomCenter
        ) {
            // Half-circle speedometer: 180° (left) -> 270° (up) -> 360° (right).
            Canvas(Modifier.fillMaxSize()) {
                val stroke = 24f
                val pad = stroke / 2 + 4f
                val r = min(size.width / 2 - pad, size.height - pad)
                val center = Offset(size.width / 2, size.height - pad)
                val topLeft = Offset(center.x - r, center.y - r)
                val arcSize = Size(r * 2, r * 2)
                val startDeg = 180f
                val sweepDeg = 180f

                drawArc(track, startDeg, sweepDeg, false, topLeft, arcSize,
                    style = Stroke(stroke, cap = StrokeCap.Round))
                if (frac > 0f) {
                    drawArc(
                        if (frac > 0.8f) warn else accent,
                        startDeg, sweepDeg * frac, false, topLeft, arcSize,
                        style = Stroke(stroke, cap = StrokeCap.Round)
                    )
                }
                val ang = Math.toRadians((startDeg + sweepDeg * frac + wobble).toDouble())
                val nr = r - stroke / 2
                drawLine(
                    needleColor, center,
                    Offset(center.x + (nr * cos(ang)).toFloat(), center.y + (nr * sin(ang)).toFloat()),
                    strokeWidth = 6f, cap = StrokeCap.Round
                )
                drawCircle(needleColor, 10f, center)
            }
            Column(
                horizontalAlignment = Alignment.CenterHorizontally,
                modifier = Modifier.padding(bottom = 6.dp)
            ) {
                Text(
                    String.format(Locale.US, "%.1f", needle),
                    style = MaterialTheme.typography.headlineLarge,
                    fontWeight = FontWeight.Bold, fontFamily = FontFamily.Monospace,
                )
                Text("tokens / sec", style = MaterialTheme.typography.labelMedium)
            }
        }
    }
}

@Composable
private fun Stat(label: String, value: String, modifier: Modifier = Modifier) {
    ElevatedCard(modifier) {
        Column(Modifier.padding(14.dp)) {
            Text(value, style = MaterialTheme.typography.titleLarge, fontFamily = FontFamily.Monospace)
            Text(label, style = MaterialTheme.typography.labelMedium,
                color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
    }
}

private fun compact(n: Long): String = when {
    n >= 1_000_000 -> String.format(Locale.US, "%.2fM", n / 1_000_000.0)
    n >= 1_000 -> String.format(Locale.US, "%.1fk", n / 1_000.0)
    else -> n.toString()
}

private fun formatDuration(ms: Long): String {
    val sec = ms / 1000
    val h = sec / 3600; val m = (sec % 3600) / 60; val ss = sec % 60
    return if (h > 0) String.format(Locale.US, "%dh %02dm", h, m)
    else String.format(Locale.US, "%dm %02ds", m, ss)
}

@Composable
private fun Field(
    label: String, value: String,
    keyboard: KeyboardType = KeyboardType.Text,
    onChange: (String) -> Unit,
) {
    OutlinedTextField(
        value = value, onValueChange = onChange, label = { Text(label) },
        singleLine = true, modifier = Modifier.fillMaxWidth(),
        keyboardOptions = KeyboardOptions(keyboardType = keyboard),
    )
}

@Composable
private fun Toggle(label: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(
        Modifier.fillMaxWidth(), verticalAlignment = Alignment.CenterVertically
    ) {
        Text(label, Modifier.weight(1f))
        Switch(checked = checked, onCheckedChange = onChange)
    }
}
