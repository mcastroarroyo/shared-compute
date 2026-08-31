package manifest

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
)

func newTestSigner(t *testing.T) (*Signer, map[string]ed25519.PublicKey) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		t.Fatal(err)
	}
	s, err := NewSigner("signer-v1", base64.StdEncoding.EncodeToString(seed))
	if err != nil || s == nil {
		t.Fatalf("NewSigner: %v", err)
	}
	pub, _ := base64.StdEncoding.DecodeString(s.PublicKeyB64())
	return s, map[string]ed25519.PublicKey{s.ID(): pub}
}

func sampleManifest(devicePK string, now int64) WorkloadManifest {
	return WorkloadManifest{
		JobID: "job-1", LeaseID: "job-1", WorkloadID: "job-1", CustomerID: "",
		DevicePK:       devicePK,
		RuntimeID:      "llama.cpp",
		RuntimeVersion: "v0",
		RuntimeHash:    Sha256Hex("llama.cpp@v0"),
		ModelID:        "qwen2.5-0.5b-instruct-q4_k_m",
		ModelHash:      Sha256Hex("qwen2.5-0.5b-instruct-q4_k_m"),
		InputHash:      Sha256Hex("sealed-input"),
		ResourceLimits: DefaultLimits(256),
		CreatedAt:      now,
		ExpiresAt:      now + 120,
		Nonce:          NewNonce(),
	}
}

func device() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.StdEncoding.EncodeToString(b)
}

func TestSignAndVerifyRoundTrip(t *testing.T) {
	s, trusted := newTestSigner(t)
	dev := device()
	sm, err := s.Sign(sampleManifest(dev, 1000))
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(sm, trusted, DefaultNodePolicy(), dev, 1005); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestVerifyRejectsHostileCases(t *testing.T) {
	s, trusted := newTestSigner(t)
	dev := device()
	base, err := s.Sign(sampleManifest(dev, 1000))
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(SignedManifest) (SignedManifest, string, int64){
		"post-signature tamper": func(sm SignedManifest) (SignedManifest, string, int64) {
			sm.Manifest.ModelID = "evil.gguf"
			return sm, dev, 1005
		},
		"wrong device": func(sm SignedManifest) (SignedManifest, string, int64) {
			return sm, device(), 1005
		},
		"expired": func(sm SignedManifest) (SignedManifest, string, int64) {
			return sm, dev, 2000
		},
		"created in the future": func(sm SignedManifest) (SignedManifest, string, int64) {
			return sm, dev, 100
		},
		"unknown signer": func(sm SignedManifest) (SignedManifest, string, int64) {
			sm.SignerID = "signer-evil"
			return sm, dev, 1005
		},
		"forged signature": func(sm SignedManifest) (SignedManifest, string, int64) {
			sm.Signature = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
			return sm, dev, 1005
		},
	}
	for name, mutate := range cases {
		sm := base
		mutated, expDev, now := mutate(sm)
		if err := Verify(mutated, trusted, DefaultNodePolicy(), expDev, now); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: expected ErrInvalid, got %v", name, err)
		}
	}
}

func TestVerifyRejectsResourceLimitsOverCeiling(t *testing.T) {
	s, trusted := newTestSigner(t)
	dev := device()
	m := sampleManifest(dev, 1000)
	m.ResourceLimits.MaxMemoryMB = 999_999 // over the node ceiling
	sm, err := s.Sign(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(sm, trusted, DefaultNodePolicy(), dev, 1005); !errors.Is(err, ErrInvalid) {
		t.Fatalf("oversized memory limit accepted: %v", err)
	}
}

func TestSignStampsFixedVocabulary(t *testing.T) {
	s, _ := newTestSigner(t)
	m := sampleManifest(device(), 1000)
	m.Operation = "execute-shell"
	m.NetworkPolicy = "CUSTOM"
	sm, err := s.Sign(m) // Sign overwrites these to the only allowed values
	if err != nil {
		t.Fatal(err)
	}
	if sm.Manifest.Operation != OperationInference || sm.Manifest.NetworkPolicy != NetworkPolicyNone {
		t.Fatalf("Sign did not normalize the fixed vocabulary: %+v", sm.Manifest)
	}
}

func TestNewSignerEmptyIsDisabled(t *testing.T) {
	s, err := NewSigner("", "")
	if err != nil || s != nil {
		t.Fatalf("empty key should disable signing, got signer=%v err=%v", s, err)
	}
}

// TestCanonicalBytesStable guards the cross-language signing contract: the
// canonical form must not depend on map ordering or whitespace.
func TestCanonicalBytesStable(t *testing.T) {
	m := sampleManifest("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", 1000)
	a, err := canonicalBytes(m)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := canonicalBytes(m)
	if string(a) != string(b) {
		t.Fatal("canonicalBytes is not deterministic")
	}
	if a[0] != '{' || a[len(a)-1] != '}' || contains(a, '\n') {
		t.Fatalf("canonical form is not compact JSON: %q", a)
	}
}

func contains(b []byte, c byte) bool {
	for _, x := range b {
		if x == c {
			return true
		}
	}
	return false
}
