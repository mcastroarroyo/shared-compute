package dev.ayni.provider

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

/**
 * Resumes sharing after a reboot if the operator had it running and the
 * daemon is configured. Without this, a phone that restarts (OS update, low
 * battery, manual reboot) silently drops out of the network until someone
 * opens the app and taps Start again.
 *
 * Calling startForegroundService() from a BOOT_COMPLETED receiver is one of
 * the documented exemptions to Android's background-start restrictions,
 * provided the service promptly calls startForeground() — ProviderService
 * does so in onCreate().
 */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != Intent.ACTION_BOOT_COMPLETED) return
        val store = ConfigStore(context)
        val s = store.load()
        if (s.isConfigured && store.isSharingEnabled()) {
            ProviderService.start(context)
        }
    }
}
