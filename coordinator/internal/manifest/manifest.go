// Package manifest signs a per-job Workload Manifest v1: an Ed25519-signed
// authorization that binds one job to one device, runtime, model, resource
// envelope, and expiry. The provider node verifies it against the coordinator's
// published signing key and its own immutable local ceilings before running
// anything.
//
// This is the Go counterpart of security/ayni_security/manifest.py. The canonical
// bytes are the compact, field-order JSON of WorkloadManifest with HTML escaping
// off, so Go's encoding/json and Rust's serde_json produce identical output.
package manifest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the only manifest version this build emits or accepts.
const Version = 1

// Fixed vocabulary — the node rejects anything else.
const (
	OperationInference = "inference"
	NetworkPolicyNone  = "NONE"
)

// ResourceLimits is the per-job envelope. Every field is a hard cap the node
// enforces; none may exceed the node's immutable NodeSafetyPolicy ceiling.
type ResourceLimits struct {
	MaxRuntimeMS       int `json:"max_runtime_ms"`
	MaxMemoryMB        int `json:"max_memory_mb"`
	MaxStorageMB       int `json:"max_storage_mb"`
	MaxInputBytes      int `json:"max_input_bytes"`
	MaxOutputTokens    int `json:"max_output_tokens"`
	MaxCPUDutyCyclePct int `json:"max_cpu_duty_cycle_pct"`
}

// WorkloadManifest is the signed body. Field order here IS the canonical byte
// order; keep it identical to the Rust struct.
type WorkloadManifest struct {
	ManifestVersion int            `json:"manifest_version"`
	JobID           string         `json:"job_id"`
	LeaseID         string         `json:"lease_id"`
	WorkloadID      string         `json:"workload_id"`
	CustomerID      string         `json:"customer_id"`
	DevicePK        string         `json:"device_pk"` // base64 std, 32 bytes
	RuntimeID       string         `json:"runtime_id"`
	RuntimeVersion  string         `json:"runtime_version"`
	RuntimeHash     string         `json:"runtime_hash"` // lowercase sha-256 hex
	ModelID         string         `json:"model_id"`
	ModelHash       string         `json:"model_hash"` // lowercase sha-256 hex
	Operation       string         `json:"operation"`
	InputHash       string         `json:"input_hash"` // lowercase sha-256 hex
	ResourceLimits  ResourceLimits `json:"resource_limits"`
	NetworkPolicy   string         `json:"network_policy"`
	CreatedAt       int64          `json:"created_at"`
	ExpiresAt       int64          `json:"expires_at"`
	Nonce           string         `json:"nonce"` // base64 std, 24 bytes
}

// SignedManifest is what travels on the wire inside a JobRequest.
type SignedManifest struct {
	Manifest  WorkloadManifest `json:"manifest"`
	SignerID  string           `json:"signer_id"`
	Signature string           `json:"signature"` // base64 std Ed25519
}

// canonicalBytes is the exact input to Sign/Verify: compact JSON in struct field
// order, HTML escaping off, no trailing newline. Rust's serde_json produces the
// same bytes for the mirror struct.
func canonicalBytes(m WorkloadManifest) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Signer holds the coordinator's isolated manifest key.
type Signer struct {
	id  string
	key ed25519.PrivateKey
	pub ed25519.PublicKey
}

// NewSigner parses a base64 (std, no padding tolerated either) 32-byte Ed25519
// seed. An empty seed returns (nil, nil): manifest signing is simply disabled.
func NewSigner(id, seedB64 string) (*Signer, error) {
	if seedB64 == "" {
		return nil, nil
	}
	seed, err := decodeB64(seedB64)
	if err != nil {
		return nil, fmt.Errorf("manifest signing key: %w", err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("manifest signing key: want %d-byte seed, got %d", ed25519.SeedSize, len(seed))
	}
	if id == "" {
		id = "signer-v1"
	}
	key := ed25519.NewKeyFromSeed(seed)
	return &Signer{id: id, key: key, pub: key.Public().(ed25519.PublicKey)}, nil
}

// ID is the signer identity carried in every SignedManifest.
func (s *Signer) ID() string { return s.id }

// PublicKeyB64 is what the node needs to verify. Served at GET /v1/manifest-key.
func (s *Signer) PublicKeyB64() string { return base64.StdEncoding.EncodeToString(s.pub) }

// Sign fills in structural defaults and returns a SignedManifest.
func (s *Signer) Sign(m WorkloadManifest) (SignedManifest, error) {
	m.ManifestVersion = Version
	m.Operation = OperationInference
	m.NetworkPolicy = NetworkPolicyNone
	if err := validateStructure(m); err != nil {
		return SignedManifest{}, err
	}
	cb, err := canonicalBytes(m)
	if err != nil {
		return SignedManifest{}, err
	}
	sig := ed25519.Sign(s.key, cb)
	return SignedManifest{
		Manifest:  m,
		SignerID:  s.id,
		Signature: base64.StdEncoding.EncodeToString(sig),
	}, nil
}

// --- verification (also used in tests; the real enforcement lives in the node) ---

// ErrInvalid is the umbrella error for any manifest rejection.
var ErrInvalid = errors.New("workload manifest invalid")

func invalid(reason string) error { return fmt.Errorf("%w: %s", ErrInvalid, reason) }

// NodeSafetyPolicy is the immutable local ceiling set. The coordinator cannot
// raise these; the node ships them compiled in.
type NodeSafetyPolicy struct {
	MaxRuntimeMS        int
	MaxMemoryMB         int
	MaxStorageMB        int
	MaxInputBytes       int
	MaxOutputTokens     int
	MaxCPUDutyCyclePct  int
	MaxLeaseSeconds     int64
	MaxClockSkewSeconds int64
}

// DefaultNodePolicy mirrors security/ayni_security/manifest.py NodeSafetyPolicy.
func DefaultNodePolicy() NodeSafetyPolicy {
	return NodeSafetyPolicy{
		MaxRuntimeMS: 120_000, MaxMemoryMB: 8_192, MaxStorageMB: 16_384,
		MaxInputBytes: 1_048_576, MaxOutputTokens: 4_096, MaxCPUDutyCyclePct: 80,
		MaxLeaseSeconds: 300, MaxClockSkewSeconds: 30,
	}
}

// Verify checks a SignedManifest against trusted signer keys, a node policy, the
// device it is bound to, and the clock. A ReplayGuard-style nonce check is the
// caller's job.
func Verify(sm SignedManifest, trusted map[string]ed25519.PublicKey, pol NodeSafetyPolicy, expectedDevicePK string, now int64) error {
	pub, ok := trusted[sm.SignerID]
	if !ok {
		return invalid("unknown signer")
	}
	if err := validateStructure(sm.Manifest); err != nil {
		return err
	}
	sig, err := decodeB64(sm.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return invalid("bad signature encoding")
	}
	cb, err := canonicalBytes(sm.Manifest)
	if err != nil {
		return invalid("canonicalization failed")
	}
	if !ed25519.Verify(pub, cb, sig) {
		return invalid("signature does not verify")
	}
	m := sm.Manifest
	if expectedDevicePK != "" && m.DevicePK != expectedDevicePK {
		return invalid("bound to a different device")
	}
	if now+pol.MaxClockSkewSeconds < m.CreatedAt {
		return invalid("created in the future")
	}
	if now >= m.ExpiresAt {
		return invalid("expired")
	}
	if m.ExpiresAt-m.CreatedAt > pol.MaxLeaseSeconds+pol.MaxClockSkewSeconds {
		return invalid("lease longer than the node ceiling")
	}
	rl := m.ResourceLimits
	over := rl.MaxRuntimeMS > pol.MaxRuntimeMS || rl.MaxMemoryMB > pol.MaxMemoryMB ||
		rl.MaxStorageMB > pol.MaxStorageMB || rl.MaxInputBytes > pol.MaxInputBytes ||
		rl.MaxOutputTokens > pol.MaxOutputTokens || rl.MaxCPUDutyCyclePct > pol.MaxCPUDutyCyclePct
	if over {
		return invalid("resource_limits exceed an immutable node ceiling")
	}
	return nil
}

func validateStructure(m WorkloadManifest) error {
	if m.ManifestVersion != Version {
		return invalid("unsupported manifest_version")
	}
	if m.Operation != OperationInference {
		return invalid("operation not allowed")
	}
	if m.NetworkPolicy != NetworkPolicyNone {
		return invalid("network_policy not allowed")
	}
	for _, id := range []string{m.JobID, m.LeaseID, m.WorkloadID} {
		if id == "" {
			return invalid("missing job/lease/workload id")
		}
	}
	if !isB64Bytes(m.DevicePK, 32) {
		return invalid("device_pk must be 32 base64 bytes")
	}
	if !isB64Bytes(m.Nonce, 24) {
		return invalid("nonce must be 24 base64 bytes")
	}
	for _, h := range []string{m.RuntimeHash, m.ModelHash, m.InputHash} {
		if !isSHA256Hex(h) {
			return invalid("runtime/model/input hash must be lowercase sha-256")
		}
	}
	if m.ModelID == "" || m.RuntimeID == "" {
		return invalid("missing runtime_id or model_id")
	}
	rl := m.ResourceLimits
	for _, v := range []int{rl.MaxRuntimeMS, rl.MaxMemoryMB, rl.MaxStorageMB, rl.MaxInputBytes, rl.MaxOutputTokens, rl.MaxCPUDutyCyclePct} {
		if v <= 0 {
			return invalid("resource_limits values must be positive")
		}
	}
	if rl.MaxCPUDutyCyclePct > 100 {
		return invalid("max_cpu_duty_cycle_pct over 100")
	}
	return nil
}

// --- small helpers (no bytes/strings import to keep the surface tiny) ---

func decodeB64(s string) ([]byte, error) { return base64.StdEncoding.DecodeString(s) }

func isB64Bytes(s string, n int) bool {
	raw, err := base64.StdEncoding.DecodeString(s)
	return err == nil && len(raw) == n
}

func isSHA256Hex(s string) bool {
	if len(s) != 64 {
		return false
	}
	_, err := hex.DecodeString(s)
	if err != nil {
		return false
	}
	for _, c := range s {
		if c >= 'A' && c <= 'F' {
			return false
		}
	}
	return true
}

// Sha256Hex is exported for callers that derive placeholder hashes.
func Sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// NewNonce returns 24 random base64 bytes.
func NewNonce() string {
	var b [24]byte
	_, _ = rand.Read(b[:])
	return base64.StdEncoding.EncodeToString(b[:])
}

// DefaultLimits is the envelope the coordinator attaches when a model has no
// tighter requirement. Well within DefaultNodePolicy.
func DefaultLimits(maxOutputTokens int) ResourceLimits {
	if maxOutputTokens <= 0 || maxOutputTokens > 4096 {
		maxOutputTokens = 4096
	}
	return ResourceLimits{
		MaxRuntimeMS: 120_000, MaxMemoryMB: 6_144, MaxStorageMB: 12_288,
		MaxInputBytes: 262_144, MaxOutputTokens: maxOutputTokens, MaxCPUDutyCyclePct: 75,
	}
}

// LeaseSeconds clamps a deadline (ms) to a sane manifest lease length.
func LeaseSeconds(deadlineMS int64) int64 {
	s := deadlineMS / 1000
	if s < 30 {
		s = 30
	}
	if s > 240 {
		s = 240
	}
	return s
}
