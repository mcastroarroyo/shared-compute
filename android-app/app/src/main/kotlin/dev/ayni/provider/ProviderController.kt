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

    /**
     * Estimated payout rate for community-tier output tokens, USD per 1M tokens.
     * The coordinator's metering is the source of truth for real payouts (M9);
     * this is a local, clearly-labelled estimate so the operator sees value accruing.
     */
    const val USD_PER_MTOK = 0.15

    data class Status(
        val phase: Phase = Phase.STOPPED,
        val detail: String = "",
        val providerId: String = "",
        val trustTier: String = "",
        val jobsDone: Int = 0,
        val jobsActive: Int = 0,
        /** Cumulative completion tokens this node has served this session. */
        val tokensServed: Long = 0,
        /** Decode throughput of the most recent job, tokens/sec. */
        val lastTps: Double = 0.0,
        /** Best decode throughput seen this session, tokens/sec. */
        val peakTps: Double = 0.0,
        /** Wall-clock millis when the provider last reached CONNECTING. */
        val startedAtMs: Long = 0,
    ) {
        val earningsUsd: Double get() = tokensServed / 1_000_000.0 * USD_PER_MTOK
    }

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
                is SpEvent.JobFinished -> {
                    val tps = if (event.ok && event.decodeTps > 0f)
                        event.decodeTps.toDouble() else _status.value.lastTps
                    _status.value.copy(
                        jobsActive = (_status.value.jobsActive - 1).coerceAtLeast(0),
                        jobsDone = _status.value.jobsDone + 1,
                        tokensServed = _status.value.tokensServed + event.completionTokens.toLong(),
                        lastTps = tps,
                        peakTps = maxOf(_status.value.peakTps, tps),
                    )
                }
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
        _status.value = _status.value.copy(
            phase = Phase.CONNECTING, detail = "",
            startedAtMs = if (_status.value.startedAtMs == 0L)
                System.currentTimeMillis() else _status.value.startedAtMs,
        )
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
        // Keep session totals; just reset the live phase.
        _status.value = _status.value.copy(
            phase = Phase.STOPPED, detail = reason, jobsActive = 0, lastTps = 0.0,
        )
    }

    fun setBlocked(reason: String) {
        if (_status.value.phase != Phase.REGISTERED)
            _status.value = _status.value.copy(phase = Phase.BLOCKED, detail = reason)
    }
}
