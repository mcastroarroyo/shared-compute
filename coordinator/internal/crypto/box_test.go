package crypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	// Model the coordinator<->provider job exchange.
	provider, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	ephem, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// coordinator -> provider (job_request)
	req := []byte(`{"job_id":"x","messages":[{"role":"user","content":"hi"}]}`)
	sealed, err := Seal(req, provider.Public, ephem.Secret, ephem.Public)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.EPK != EncodeKey(ephem.Public) {
		t.Fatalf("epk not embedded")
	}
	epk, err := ParsePublicKey(sealed.EPK)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(sealed, epk, provider.Secret)
	if err != nil {
		t.Fatalf("provider open: %v", err)
	}
	if !bytes.Equal(got, req) {
		t.Fatalf("round-trip mismatch")
	}

	// provider -> coordinator (job_chunk), sealed back to the same ephemeral pk
	chunk := []byte(`{"job_id":"x","seq":0,"delta":"hello","done":false}`)
	sealedChunk, err := Seal(chunk, epk, provider.Secret, ephem.Public)
	if err != nil {
		t.Fatal(err)
	}
	gotChunk, err := Open(sealedChunk, provider.Public, ephem.Secret)
	if err != nil {
		t.Fatalf("coordinator open: %v", err)
	}
	if !bytes.Equal(gotChunk, chunk) {
		t.Fatalf("chunk round-trip mismatch")
	}
}

func TestOpenRejectsTamper(t *testing.T) {
	a, _ := GenerateKeyPair()
	b, _ := GenerateKeyPair()
	sealed, err := Seal([]byte("secret"), b.Public, a.Secret, a.Public)
	if err != nil {
		t.Fatal(err)
	}

	ct, _ := base64.StdEncoding.DecodeString(sealed.Ciphertext)
	ct[0] ^= 0xff
	tampered := sealed
	tampered.Ciphertext = base64.StdEncoding.EncodeToString(ct)

	if _, err := Open(tampered, a.Public, b.Secret); err != ErrDecrypt {
		t.Fatalf("expected ErrDecrypt for tampered ciphertext, got %v", err)
	}

	if _, err := Open(sealed, b.Public, b.Secret); err != ErrDecrypt {
		t.Fatalf("expected ErrDecrypt for wrong sender key, got %v", err)
	}
}

func TestParsePublicKeyValidation(t *testing.T) {
	if _, err := ParsePublicKey("not-base64!!!"); err == nil {
		t.Fatal("expected error for bad base64")
	}
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for wrong length")
	}
}
