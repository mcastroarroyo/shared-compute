// Package attest verifies provider hardware-attestation evidence and maps it to a
// trust tier. Tier 0 ("community") needs no evidence; Tier 1 ("device_attested")
// currently means a verified Android Key Attestation chain from a TEE/StrongBox key
// on a locked, verified-boot device, bound to the provider's X25519 static key.
package attest

import (
	"encoding/json"
	"errors"
)

// Tier names mirror the values the scheduler and API compare against.
const (
	TierCommunity      = "community"
	TierDeviceAttested = "device_attested"
)

// Result is the outcome of verifying a register frame's attestation block.
type Result struct {
	Tier string
	// Kind echoes the evidence kind that was verified ("none", "android_key", ...).
	Kind string
	// AndroidKey is populated when Kind == "android_key".
	AndroidKey *AndroidKeyResult
}

// AndroidKeyResult carries the salient facts extracted from a verified chain.
type AndroidKeyResult struct {
	SecurityLevel     string // "software" | "tee" | "strongbox"
	VerifiedBootState string // "verified" | "self_signed" | "unverified" | "failed"
	DeviceLocked      bool
}

// Evidence is the decoded `attestation` block from a register frame.
type Evidence struct {
	Kind     string          `json:"kind"`
	Evidence json.RawMessage `json:"evidence"`
}

// ErrUnattested is returned (wrapped) when evidence is absent or does not meet the
// bar for any tier above community; callers treat the node as community, not rejected.
var ErrUnattested = errors.New("no attestation that clears a higher tier")

// Verify inspects `ev` and returns the tier the provider is entitled to.
// staticPK is the provider's 32-byte X25519 public key (raw), which android_key
// evidence must be cryptographically bound to.
//
// It never returns an error for the "just community" case — only Result. A non-nil
// error means the evidence was malformed or claimed a tier it could not prove, which
// the caller may choose to log; the provider still joins as community.
func Verify(ev *Evidence, staticPK [32]byte) (Result, error) {
	if ev == nil || ev.Kind == "" || ev.Kind == "none" {
		return Result{Tier: TierCommunity, Kind: "none"}, nil
	}
	switch ev.Kind {
	case "android_key":
		ak, err := verifyAndroidKey(ev.Evidence, staticPK)
		if err != nil {
			return Result{Tier: TierCommunity, Kind: ev.Kind}, err
		}
		return Result{Tier: TierDeviceAttested, Kind: ev.Kind, AndroidKey: ak}, nil
	default:
		// play_integrity / tpm / vbs_enclave / avf / nvidia_cc / sev_snp / tdx: not yet.
		return Result{Tier: TierCommunity, Kind: ev.Kind}, ErrUnattested
	}
}
