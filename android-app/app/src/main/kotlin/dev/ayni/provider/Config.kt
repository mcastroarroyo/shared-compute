package dev.ayni.provider

import android.content.Context

/** Operator-entered settings, persisted in SharedPreferences. */
data class ProviderSettings(
    val coordinatorUrl: String = "wss://api.ayni-ai.com/ws/provider",
    val registrationToken: String = "",
    val model: String = "qwen2.5-0.5b-instruct-q4_k_m",
    val backend: String = "llama", // "mock" for a no-download connectivity check
    val manifestUrl: String = "https://models.ayni-ai.com",
    val registryPubkey: String = "p7UUs6aCFebGUfSvFV5Wczh7kYBCEW2tDRO+dV0IpDM=",
    val maxContext: Int = 8192,
    // policy
    val onlyWhenCharging: Boolean = true,
    val onlyOnWifi: Boolean = true,
    val minBatteryPct: Int = 30,
) {
    val isConfigured get() = registrationToken.isNotBlank() && coordinatorUrl.isNotBlank()
}

class ConfigStore(context: Context) {
    private val sp = context.getSharedPreferences("provider", Context.MODE_PRIVATE)

    fun load(): ProviderSettings {
        val d = ProviderSettings()
        fun str(key: String, default: String) = sp.getString(key, null)?.ifBlank { null } ?: default
        return ProviderSettings(
        coordinatorUrl = str("url", d.coordinatorUrl),
        registrationToken = sp.getString("token", "") ?: "",
        model = str("model", d.model),
        backend = str("backend", d.backend),
        manifestUrl = str("manifestUrl", d.manifestUrl),
        registryPubkey = str("pubkey", d.registryPubkey),
        maxContext = sp.getInt("maxCtx", d.maxContext),
        onlyWhenCharging = sp.getBoolean("charging", true),
        onlyOnWifi = sp.getBoolean("wifi", true),
        minBatteryPct = sp.getInt("minBat", 30),
        )
    }

    fun save(s: ProviderSettings) = sp.edit().apply {
        putString("url", s.coordinatorUrl)
        putString("token", s.registrationToken)
        putString("model", s.model)
        putString("backend", s.backend)
        putString("manifestUrl", s.manifestUrl)
        putString("pubkey", s.registryPubkey)
        putInt("maxCtx", s.maxContext)
        putBoolean("charging", s.onlyWhenCharging)
        putBoolean("wifi", s.onlyOnWifi)
        putInt("minBat", s.minBatteryPct)
    }.apply()

    // Tracks operator intent ("I want this device sharing") independently of the
    // settings form's Save button, so BootReceiver can resume sharing after a
    // reboot without needing the whole ProviderSettings round-trip.
    fun setSharingEnabled(v: Boolean) = sp.edit().putBoolean("sharingEnabled", v).apply()
    fun isSharingEnabled(): Boolean = sp.getBoolean("sharingEnabled", false)
}
