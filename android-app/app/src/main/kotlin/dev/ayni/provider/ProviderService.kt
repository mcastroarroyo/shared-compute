package dev.ayni.provider

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.Service
import android.content.Intent
import android.os.IBinder
import androidx.core.app.NotificationCompat
import kotlinx.coroutines.CoroutineScope
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch

/**
 * Foreground service: keeps the provider connected while the run-policy is satisfied,
 * pausing it when the phone is off charger / off Wi-Fi / low battery.
 */
class ProviderService : Service() {

    private val scope = CoroutineScope(Dispatchers.Default + Job())
    private lateinit var store: ConfigStore

    override fun onCreate() {
        super.onCreate()
        store = ConfigStore(this)
        createChannel()
        startForeground(NOTIF_ID, notification("starting…"))
        loop()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            ProviderController.stop("stopped by user")
            stopSelf()
            return START_NOT_STICKY
        }
        return START_STICKY
    }

    private fun loop() = scope.launch {
        var running = false
        while (isActive) {
            val s = store.load()
            if (!s.isConfigured) {
                ProviderController.stop("not configured")
                updateNotif("not configured — open the app")
            } else {
                val st = Policy.sample(this@ProviderService)
                val blocked = Policy.blockReason(s, st)
                if (blocked != null) {
                    if (running) { ProviderController.stop(blocked); running = false }
                    ProviderController.setBlocked(blocked)
                    updateNotif(blocked)
                } else if (!running) {
                    ProviderController.start(this@ProviderService, s)
                    running = true
                }
            }
            val phase = ProviderController.status.value
            if (running) {
                updateNotif(
                    when (phase.phase) {
                        ProviderController.Phase.REGISTERED ->
                            "online · ${phase.jobsDone} jobs · tier ${phase.trustTier}"
                        ProviderController.Phase.CONNECTING -> "connecting…"
                        ProviderController.Phase.ERROR -> "error: ${phase.detail}"
                        else -> phase.detail.ifBlank { "…" }
                    }
                )
                if (phase.phase == ProviderController.Phase.STOPPED ||
                    phase.phase == ProviderController.Phase.ERROR
                ) running = false
            }
            delay(15_000)
        }
    }

    override fun onDestroy() {
        scope.coroutineContext[Job]?.cancel()
        ProviderController.stop("service stopped")
        super.onDestroy()
    }

    override fun onBind(intent: Intent?): IBinder? = null

    private fun createChannel() {
        val nm = getSystemService(NotificationManager::class.java)
        nm.createNotificationChannel(
            NotificationChannel(CHANNEL, "Provider", NotificationManager.IMPORTANCE_LOW)
        )
    }

    private fun notification(text: String): Notification =
        NotificationCompat.Builder(this, CHANNEL)
            .setSmallIcon(android.R.drawable.stat_sys_upload)
            .setContentTitle("Ayni Provider")
            .setContentText(text)
            .setOngoing(true)
            .setContentIntent(
                android.app.PendingIntent.getActivity(
                    this, 0, Intent(this, MainActivity::class.java),
                    android.app.PendingIntent.FLAG_IMMUTABLE
                )
            )
            .build()

    private fun updateNotif(text: String) =
        getSystemService(NotificationManager::class.java).notify(NOTIF_ID, notification(text))

    companion object {
        const val CHANNEL = "provider"
        const val NOTIF_ID = 1
        const val ACTION_STOP = "dev.ayni.provider.STOP"

        fun start(ctx: android.content.Context) {
            ConfigStore(ctx).setSharingEnabled(true)
            ctx.startForegroundService(Intent(ctx, ProviderService::class.java))
        }

        fun stop(ctx: android.content.Context) {
            ConfigStore(ctx).setSharingEnabled(false)
            ctx.startService(Intent(ctx, ProviderService::class.java).setAction(ACTION_STOP))
        }
    }
}
