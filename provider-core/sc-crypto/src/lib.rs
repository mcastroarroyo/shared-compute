//! NaCl `crypto_box` (X25519 + XSalsa20-Poly1305) sealing for the job envelope described
//! in `/protocol/envelope.md`. Mirrors `coordinator/internal/crypto`.

use base64::{engine::general_purpose::STANDARD as B64, Engine};
use crypto_box::{
    aead::{Aead, AeadCore, OsRng},
    PublicKey, SalsaBox, SecretKey,
};
use sc_protocol::{SealedPayload, SEAL_ALG};

#[derive(Debug, thiserror::Error)]
pub enum CryptoError {
    /// Deliberately generic: never leak why an open failed.
    #[error("decrypt")]
    Decrypt,
    #[error("invalid key: {0}")]
    Key(String),
}

/// An X25519 keypair.
pub struct Keypair {
    pub public: PublicKey,
    pub secret: SecretKey,
}

impl Keypair {
    /// Generate a fresh keypair from the OS RNG.
    pub fn generate() -> Self {
        let secret = SecretKey::generate(&mut OsRng);
        let public = secret.public_key();
        Keypair { public, secret }
    }
}

/// Decode a base64 32-byte X25519 public key.
pub fn parse_public(b64: &str) -> Result<PublicKey, CryptoError> {
    let raw = B64
        .decode(b64)
        .map_err(|e| CryptoError::Key(e.to_string()))?;
    let arr: [u8; 32] = raw
        .as_slice()
        .try_into()
        .map_err(|_| CryptoError::Key(format!("expected 32 bytes, got {}", raw.len())))?;
    Ok(PublicKey::from(arr))
}

/// Base64-encode a public key.
pub fn encode_public(k: &PublicKey) -> String {
    B64.encode(k.as_bytes())
}

/// Encrypt `plaintext` from `sender_secret` to `recipient_pub` with a fresh random nonce.
/// `epk_to_embed` is written to the payload's `epk` field (the coordinator's per-job
/// ephemeral public key, echoed in both directions).
pub fn seal(
    plaintext: &[u8],
    recipient_pub: &PublicKey,
    sender_secret: &SecretKey,
    epk_to_embed: &PublicKey,
) -> SealedPayload {
    let b = SalsaBox::new(recipient_pub, sender_secret);
    let nonce = SalsaBox::generate_nonce(&mut OsRng);
    let ct = b
        .encrypt(&nonce, plaintext)
        .expect("XSalsa20Poly1305 encryption is infallible for valid inputs");
    SealedPayload {
        alg: SEAL_ALG.to_string(),
        epk: B64.encode(epk_to_embed.as_bytes()),
        nonce: B64.encode(nonce.as_slice()),
        ciphertext: B64.encode(ct),
    }
}

/// Decrypt a sealed payload sent by `sender_pub` to `recipient_secret`.
pub fn open(
    p: &SealedPayload,
    sender_pub: &PublicKey,
    recipient_secret: &SecretKey,
) -> Result<Vec<u8>, CryptoError> {
    if p.alg != SEAL_ALG {
        return Err(CryptoError::Decrypt);
    }
    let nonce_raw = B64.decode(&p.nonce).map_err(|_| CryptoError::Decrypt)?;
    if nonce_raw.len() != 24 {
        return Err(CryptoError::Decrypt);
    }
    let ct = B64
        .decode(&p.ciphertext)
        .map_err(|_| CryptoError::Decrypt)?;
    let b = SalsaBox::new(sender_pub, recipient_secret);
    let nonce = crypto_box::Nonce::from_slice(&nonce_raw);
    b.decrypt(nonce, ct.as_slice())
        .map_err(|_| CryptoError::Decrypt)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trip_both_directions() {
        let provider = Keypair::generate();
        let ephem = Keypair::generate();

        // coordinator -> provider
        let req = br#"{"job_id":"x"}"#;
        let sealed = seal(req, &provider.public, &ephem.secret, &ephem.public);
        assert_eq!(sealed.epk, encode_public(&ephem.public));

        let epk = parse_public(&sealed.epk).unwrap();
        let got = open(&sealed, &epk, &provider.secret).unwrap();
        assert_eq!(got, req);

        // provider -> coordinator, sealed back to the same ephemeral key
        let chunk = br#"{"seq":0,"delta":"hi"}"#;
        let sc = seal(chunk, &epk, &provider.secret, &ephem.public);
        let got = open(&sc, &provider.public, &ephem.secret).unwrap();
        assert_eq!(got, chunk);
    }

    #[test]
    fn rejects_tamper_and_wrong_key() {
        let a = Keypair::generate();
        let b = Keypair::generate();
        let mut sealed = seal(b"secret", &b.public, &a.secret, &a.public);

        let mut ct = B64.decode(&sealed.ciphertext).unwrap();
        ct[0] ^= 0xff;
        sealed.ciphertext = B64.encode(&ct);
        assert!(matches!(
            open(&sealed, &a.public, &b.secret),
            Err(CryptoError::Decrypt)
        ));

        let good = seal(b"secret", &b.public, &a.secret, &a.public);
        assert!(matches!(
            open(&good, &b.public, &b.secret),
            Err(CryptoError::Decrypt)
        ));
    }
}
