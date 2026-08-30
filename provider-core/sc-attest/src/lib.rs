//! Attestation provider abstraction and the Tier 0 `NullAttestation`.
//!
//! Later milestones add `AndroidKeystoreAttestation` (M6), `TpmAttestation` +
//! `VbsEnclaveAttestation` (M7), and `NvidiaCcAttestation` / `SevSnpAttestation` /
//! `AvfAttestation` (M8). The coordinator's policy engine maps evidence → trust tier.

use sc_protocol::{Attestation, SecurityCapabilities};

/// Produces the `attestation` block sent in `register` and reports the device's
/// self-declared security posture.
pub trait AttestationProvider: Send + Sync {
    fn attestation(&self) -> Attestation;
    fn security_capabilities(&self) -> SecurityCapabilities;
}

/// Tier 0: no hardware attestation. The device owner is not prevented from inspecting
/// plaintext; the network still enforces transport + payload encryption.
#[derive(Debug, Default, Clone, Copy)]
pub struct NullAttestation;

impl AttestationProvider for NullAttestation {
    fn attestation(&self) -> Attestation {
        Attestation {
            kind: "none".to_string(),
            evidence: None,
        }
    }

    fn security_capabilities(&self) -> SecurityCapabilities {
        SecurityCapabilities::default()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn null_attestation_claims_nothing() {
        let a = NullAttestation;
        assert_eq!(a.attestation().kind, "none");
        let s = a.security_capabilities();
        assert!(!s.hardware_key && !s.confidential_gpu);
    }
}
