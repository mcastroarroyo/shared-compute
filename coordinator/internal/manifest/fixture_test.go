package manifest

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteCrossLanguageFixture emits a deterministic signed manifest that the
// Rust node's sc-manifest test verifies. If the two canonicalizations ever
// diverge, the Rust side's signature check fails.
func TestWriteCrossLanguageFixture(t *testing.T) {
	seed := make([]byte, 32) // fixed all-zero seed -> deterministic key
	s, err := NewSigner("signer-v1", base64.StdEncoding.EncodeToString(seed))
	if err != nil || s == nil {
		t.Fatal(err)
	}
	dev := base64.StdEncoding.EncodeToString(make([]byte, 32))
	sm, err := s.Sign(WorkloadManifest{
		JobID: "job-1", LeaseID: "job-1", WorkloadID: "job-1", CustomerID: "",
		DevicePK:       dev,
		RuntimeID:      "llama.cpp",
		RuntimeVersion: "v0",
		RuntimeHash:    Sha256Hex("llama.cpp@v0"),
		ModelID:        "qwen2.5-0.5b",
		ModelHash:      Sha256Hex("qwen2.5-0.5b"),
		InputHash:      Sha256Hex("x"),
		ResourceLimits: DefaultLimits(256),
		CreatedAt:      1000,
		ExpiresAt:      1120,
		Nonce:          base64.StdEncoding.EncodeToString(make([]byte, 24)),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]any{
		"signed_manifest":    sm,
		"signer_public_b64":  s.PublicKeyB64(),
		"expected_device_pk": dev,
		"now":                1005,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	path := filepath.Join("testdata", "cross_language_fixture.json")
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
