package dev.ayni.provider

import android.Manifest
import android.content.Intent
import android.os.Build
import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.compose.setContent
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.*
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.foundation.verticalScroll
import androidx.compose.material3.*
import androidx.compose.runtime.*
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.collectAsStateWithLifecycle

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

    val notifPerm = rememberLauncherForActivityResult(
        ActivityResultContracts.RequestPermission()
    ) {}

    Column(
        Modifier.fillMaxSize().padding(20.dp).verticalScroll(rememberScrollState()),
        verticalArrangement = Arrangement.spacedBy(12.dp)
    ) {
        Text("Ayni Provider", style = MaterialTheme.typography.headlineSmall)

        ElevatedCard {
            Column(Modifier.padding(16.dp), verticalArrangement = Arrangement.spacedBy(4.dp)) {
                val phase = status.phase.name.lowercase()
                Text("Status: $phase", style = MaterialTheme.typography.titleMedium)
                if (status.detail.isNotBlank()) Text(status.detail)
                if (status.phase == ProviderController.Phase.REGISTERED) {
                    Text("provider ${status.providerId.take(8)} · tier ${status.trustTier}")
                    Text("jobs done: ${status.jobsDone}   active: ${status.jobsActive}")
                }
            }
        }

        val configured = s.isConfigured
        Row(horizontalArrangement = Arrangement.spacedBy(10.dp)) {
            Button(
                enabled = configured,
                onClick = {
                    if (Build.VERSION.SDK_INT >= 33)
                        notifPerm.launch(Manifest.permission.POST_NOTIFICATIONS)
                    ProviderService.start(ctx)
                }
            ) { Text("Start") }
            OutlinedButton(onClick = { ProviderService.stop(ctx) }) { Text("Stop") }
        }
        if (!configured) Text("Enter a registration token below, then Start.")

        Text("Settings", style = MaterialTheme.typography.titleMedium)
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

