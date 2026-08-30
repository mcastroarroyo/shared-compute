package dev.ayni.provider

import android.content.Context

/** Operator-entered settings, persisted in SharedPreferences. */
data class ProviderSettings(
    val coordinatorUrl: String = "wss://api.ayni-ai.com/ws/provider",
    val registrationToken: String = "",
    val model: String = "qwen2.5-0.5b-instruct-q4_k_m",
    val backend: String = "mock", // "mock" until the llama Android backend ships
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

    fun load() = ProviderSettings(
        coordinatorUrl = sp.getString("url", null) ?: ProviderSettings().coordinatorUrl,
        registrationToken = sp.getString("token", "") ?: "",
        model = sp.getString("model", null) ?: ProviderSettings().model,
        backend = sp.getString("backend", null) ?: ProviderSettings().backend,
        manifestUrl = sp.getString("manifestUrl", null) ?: ProviderSettings().manifestUrl,
        registryPubkey = sp.getString("pubkey", null) ?: ProviderSettings().registryPubkey,
        maxContext = sp.getInt("maxCtx", ProviderSettings().maxContext),
        onlyWhenCharging = sp.getBoolean("charging", true),
        onlyOnWifi = sp.getBoolean("wifi", true),
        minBatteryPct = sp.getInt("minBat", 30),
    )

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
}
