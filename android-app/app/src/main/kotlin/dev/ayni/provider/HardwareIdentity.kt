package dev.ayni.provider

import android.os.Build
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import android.util.Log
import java.io.ByteArrayOutputStream
import java.nio.ByteBuffer
import java.security.KeyPairGenerator
import java.security.KeyStore
import java.security.Signature
import java.security.cert.X509Certificate

/**
 * Device identity key in the Android Keystore, hardware-backed (StrongBox when the
 * device has it, TEE otherwise), created once with a Key Attestation challenge.
 *
 * The key is EC P-256 (SIGN). We do not run crypto_box with it — the network identity
 * stays X25519 — instead we *bind* the X25519 static key to this attested key by
 * signing it, so the coordinator can raise the node to `device_attested` after
 * verifying the attestation certificate chain to Google's hardware root.
 */
object HardwareIdentity {
    private const val TAG = "ayni-provider"
    private const val ALIAS = "ayni-device-id"
    private const val KS = "AndroidKeyStore"
    // Domain separator for the binding signature; keep in lockstep with the coordinator.
    private val BIND_PREFIX = "ayni-bind-v1".toByteArray()

    data class Evidence(
        val kind: String,              // "android_key"
        val certChainDerB64: List<String>,
        val bindingSigB64: String,
        val nonceB64: String,
        val issuedAt: Long,
        val strongBox: Boolean,
    )

    /** True if this device exposes a dedicated StrongBox (e.g. Titan M on Pixel). */
    fun hasStrongBox(ctx: android.content.Context): Boolean =
        ctx.packageManager.hasSystemFeature("android.hardware.strongbox_keystore")

    /**
     * Produce attestation evidence binding [x25519StaticPk] (32 raw bytes) to the
     * hardware key. Creates the key on first call. Returns null if the platform
     * can't attest (very old / non-conformant devices) — caller falls back to Tier 0.
     */
    fun evidence(ctx: android.content.Context, x25519StaticPk: ByteArray): Evidence? = try {
        val strongBox = ensureKey(ctx)
        val ks = KeyStore.getInstance(KS).apply { load(null) }
        val chain = (ks.getCertificateChain(ALIAS) ?: return null)
            .map { it as X509Certificate }
        require(chain.isNotEmpty())

        val nonce = ByteArray(32).also { java.security.SecureRandom().nextBytes(it) }
        val issuedAt = System.currentTimeMillis() / 1000
        val signed = ByteArrayOutputStream().apply {
            write(BIND_PREFIX)
            write(x25519StaticPk)
            write(nonce)
            write(ByteBuffer.allocate(8).putLong(issuedAt).array())
        }.toByteArray()

        val sig = Signature.getInstance("SHA256withECDSA").run {
            initSign((ks.getEntry(ALIAS, null) as KeyStore.PrivateKeyEntry).privateKey)
            update(signed)
            sign()
        }

        Evidence(
            kind = "android_key",
            certChainDerB64 = chain.map { b64(it.encoded) },
            bindingSigB64 = b64(sig),
            nonceB64 = b64(nonce),
            issuedAt = issuedAt,
            strongBox = strongBox,
        )
    } catch (t: Throwable) {
        Log.w(TAG, "hardware attestation unavailable: ${t.message}")
        null
    }

    /** @return true if the key is StrongBox-backed. */
    private fun ensureKey(ctx: android.content.Context): Boolean {
        val ks = KeyStore.getInstance(KS).apply { load(null) }
        val wantStrongBox = hasStrongBox(ctx)
        if (ks.containsAlias(ALIAS)) {
            // Key attestation chains are fixed at key-generation time and the
            // RKP-issued intermediate is short-lived (weeks). Once any cert in the
            // chain is within a day of expiry the coordinator would reject the
            // evidence ("intermediate cert outside validity window") and demote
            // the device to Tier 0 — so mint a fresh key + chain instead of reusing.
            val soon = java.util.Date(System.currentTimeMillis() + 24L * 3600 * 1000)
            val chain = ks.getCertificateChain(ALIAS)?.map { it as X509Certificate }
            val stale = chain == null || chain.any { it.notAfter.before(soon) }
            if (!stale) return wantStrongBox
            Log.i(TAG, "attestation chain expired or expiring; regenerating hardware key")
            ks.deleteEntry(ALIAS)
        }

        // A stable per-install challenge is fine: freshness comes from the binding
        // signature over a per-session nonce + timestamp, checked by the coordinator.
        val challenge = ByteArray(24).also { java.security.SecureRandom().nextBytes(it) }
        fun spec(sb: Boolean) = KeyGenParameterSpec.Builder(
            ALIAS, KeyProperties.PURPOSE_SIGN or KeyProperties.PURPOSE_VERIFY
        )
            .setAlgorithmParameterSpec(java.security.spec.ECGenParameterSpec("secp256r1"))
            .setDigests(KeyProperties.DIGEST_SHA256)
            .setAttestationChallenge(challenge)
            // hasStrongBox() already implies API 28+ (the feature flag didn't exist
            // before then), but spell out the SDK check here too so lint's local
            // data-flow analysis — which can't see that guarantee — is satisfied.
            .apply { if (sb && Build.VERSION.SDK_INT >= Build.VERSION_CODES.P) setIsStrongBoxBacked(true) }
            .build()

        val gen = KeyPairGenerator.getInstance(KeyProperties.KEY_ALGORITHM_EC, KS)
        return try {
            gen.initialize(spec(wantStrongBox)); gen.generateKeyPair(); wantStrongBox
        } catch (e: Exception) {
            // StrongBox present but refused (size/attestation limits) — fall back to TEE.
            Log.w(TAG, "StrongBox key gen failed, using TEE: ${e.message}")
            gen.initialize(spec(false)); gen.generateKeyPair(); false
        }
    }

    private fun b64(b: ByteArray) = Base64.encodeToString(b, Base64.NO_WRAP)
}
