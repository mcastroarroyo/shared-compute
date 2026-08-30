package dev.ayni.provider

import android.content.Context
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import uniffi.sc_mobile.MobileConfig
import uniffi.sc_mobile.Provider
import uniffi.sc_mobile.ProviderListener
import uniffi.sc_mobile.SpEvent

/**
 * Owns the single native [Provider] instance and exposes its state to the UI.
 * The actual connection is driven by [ProviderService] once policy allows it.
 */
object ProviderController {
    enum class Phase { STOPPED, BLOCKED, CONNECTING, REGISTERED, ERROR }

    data class Status(
        val phase: Phase = Phase.STOPPED,
        val detail: String = "",
        val providerId: String = "",
        val trustTier: String = "",
        val jobsDone: Int = 0,
        val jobsActive: Int = 0,
    )

    private val _status = MutableStateFlow(Status())
    val status: StateFlow<Status> = _status.asStateFlow()

    @Volatile private var native: Provider? = null

    private val listener = object : ProviderListener {
        override fun onEvent(event: SpEvent) {
            _status.value = when (event) {
                is SpEvent.Connecting ->
                    _status.value.copy(phase = Phase.CONNECTING, detail = event.url)
                is SpEvent.Registered -> _status.value.copy(
                    phase = Phase.REGISTERED, detail = "",
                    providerId = event.providerId, trustTier = event.trustTier,
                )
                is SpEvent.JobStarted ->
                    _status.value.copy(jobsActive = _status.value.jobsActive + 1)
                is SpEvent.JobFinished -> _status.value.copy(
                    jobsActive = (_status.value.jobsActive - 1).coerceAtLeast(0),
                    jobsDone = _status.value.jobsDone + 1,
                )
                is SpEvent.Disconnected ->
                    _status.value.copy(phase = Phase.STOPPED, detail = event.reason)
                is SpEvent.Error ->
                    _status.value.copy(phase = Phase.ERROR, detail = event.message)
            }
        }
    }

    @Synchronized
    fun start(ctx: Context, s: ProviderSettings) {
        if (native?.isRunning() == true) return
        if (native == null) native = Provider()
        _status.value = Status(Phase.CONNECTING)
        val cfg = MobileConfig(
            coordinatorUrl = s.coordinatorUrl,
            registrationToken = s.registrationToken,
            model = s.model,
            dataDir = ctx.filesDir.absolutePath,
            backend = s.backend,
            manifestUrl = s.manifestUrl.ifBlank { null },
            registryPubkey = s.registryPubkey.ifBlank { null },
            maxContext = s.maxContext.toUInt(),
        )
        runCatching { native!!.start(cfg, listener) }
            .onFailure { _status.value = Status(Phase.ERROR, it.message ?: "start failed") }
    }

    @Synchronized
    fun stop(reason: String = "stopped") {
        native?.stop()
        _status.value = Status(Phase.STOPPED, reason)
    }

    fun setBlocked(reason: String) {
        if (_status.value.phase != Phase.REGISTERED)
            _status.value = _status.value.copy(phase = Phase.BLOCKED, detail = reason)
    }
}
