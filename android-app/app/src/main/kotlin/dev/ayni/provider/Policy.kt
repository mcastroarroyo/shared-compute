package dev.ayni.provider

import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.BatteryManager

/** Evaluates the run-policy (charging / Wi-Fi / battery) against current device state. */
object Policy {
    data class State(val charging: Boolean, val onWifi: Boolean, val batteryPct: Int)

    fun sample(ctx: Context): State {
        val bm = ctx.getSystemService(Context.BATTERY_SERVICE) as BatteryManager
        val pct = bm.getIntProperty(BatteryManager.BATTERY_PROPERTY_CAPACITY)
        val status = ctx.registerReceiver(null, IntentFilter(Intent.ACTION_BATTERY_CHANGED))
            ?.getIntExtra(BatteryManager.EXTRA_STATUS, -1) ?: -1
        val charging = status == BatteryManager.BATTERY_STATUS_CHARGING ||
            status == BatteryManager.BATTERY_STATUS_FULL || bm.isCharging

        val cm = ctx.getSystemService(Context.CONNECTIVITY_SERVICE) as ConnectivityManager
        val caps = cm.getNetworkCapabilities(cm.activeNetwork)
        val onWifi = caps?.hasTransport(NetworkCapabilities.TRANSPORT_WIFI) == true ||
            caps?.hasTransport(NetworkCapabilities.TRANSPORT_ETHERNET) == true

        return State(charging, onWifi, pct)
    }

    /** Returns null if the policy is satisfied, else a human-readable reason it is blocked. */
    fun blockReason(s: ProviderSettings, st: State): String? = when {
        s.onlyWhenCharging && !st.charging -> "waiting for charger"
        s.onlyOnWifi && !st.onWifi -> "waiting for Wi-Fi"
        st.batteryPct in 0 until s.minBatteryPct -> "battery ${st.batteryPct}% < ${s.minBatteryPct}%"
        else -> null
    }
}
