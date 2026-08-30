package attest

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func tryParseChain(ev androidKeyEvidence) ([]*x509.Certificate, error) {
	chain := make([]*x509.Certificate, 0, len(ev.CertChainDERB64))
	for i, b := range ev.CertChainDERB64 {
		der, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, fmt.Errorf("cert[%d] b64: %w", i, err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("cert[%d]: %w", i, err)
		}
		chain = append(chain, c)
	}
	return chain, nil
}

func parseChain(t *testing.T, ev androidKeyEvidence) []*x509.Certificate {
	t.Helper()
	chain, err := tryParseChain(ev)
	if err != nil {
		t.Fatalf("parse chain: %v", err)
	}
	return chain
}

func loadFixtureChain(t *testing.T) androidKeyEvidence {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "pixel9pf_chain.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var ev androidKeyEvidence
	if err := json.Unmarshal(b, &ev); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	if len(ev.CertChainDERB64) != 5 {
		t.Fatalf("want 5 certs, got %d", len(ev.CertChainDERB64))
	}
	return ev
}

func TestPixelChainVerifiesToGoogleRoot(t *testing.T) {
	ev := loadFixtureChain(t)
	chain := parseChain(t, ev)
	if err := verifyChainToGoogleRoot(chain); err != nil {
		t.Fatalf("chain should verify: %v", err)
	}
}

func TestPixelKeyDescription(t *testing.T) {
	ev := loadFixtureChain(t)
	chain := parseChain(t, ev)
	kd, err := parseKeyDescription(chain[0])
	if err != nil {
		t.Fatalf("parseKeyDescription: %v", err)
	}
	if got := securityLevelName(kd.securityLevel); got != "strongbox" {
		t.Errorf("security level = %q, want strongbox", got)
	}
	if got := verifiedBootName(kd.verifiedBootState); got != "verified" {
		t.Errorf("verified boot = %q, want verified", got)
	}
	if !kd.deviceLocked {
		t.Errorf("deviceLocked = false, want true")
	}
}

func TestTamperedLeafFailsChain(t *testing.T) {
	ev := loadFixtureChain(t)
	der, _ := base64.StdEncoding.DecodeString(ev.CertChainDERB64[0])
	der[len(der)-1] ^= 0x01 // flip a signature byte
	ev.CertChainDERB64[0] = base64.StdEncoding.EncodeToString(der)
	chain, err := tryParseChain(ev)
	if err != nil {
		return // parse itself may reject it — also acceptable
	}
	if err := verifyChainToGoogleRoot(chain); err == nil {
		t.Fatal("tampered leaf should not verify")
	}
}

func TestVerifyNoneIsCommunity(t *testing.T) {
	for _, ev := range []*Evidence{nil, {Kind: ""}, {Kind: "none"}} {
		r, err := Verify(ev, [32]byte{})
		if err != nil || r.Tier != TierCommunity {
			t.Errorf("Verify(%v) = %+v, %v; want community/nil", ev, r, err)
		}
	}
}

func TestVerifyUnknownKindStaysCommunity(t *testing.T) {
	r, err := Verify(&Evidence{Kind: "play_integrity"}, [32]byte{})
	if r.Tier != TierCommunity {
		t.Errorf("tier = %q, want community", r.Tier)
	}
	if err == nil {
		t.Error("want a (non-fatal) error explaining why no higher tier")
	}
}

func TestVerifyAndroidKeyFixtureReachesDeviceAttested(t *testing.T) {
	// The fixture has no binding signature (captured before that path existed), so
	// full Verify() can't run end to end; this guards the chain + policy portion.
	ev := loadFixtureChain(t)
	chain := parseChain(t, ev)
	if err := verifyChainToGoogleRoot(chain); err != nil {
		t.Fatalf("chain: %v", err)
	}
	kd, err := parseKeyDescription(chain[0])
	if err != nil {
		t.Fatalf("kd: %v", err)
	}
	if kd.securityLevel < 1 || kd.verifiedBootState != 0 || !kd.deviceLocked {
		t.Fatalf("policy inputs not satisfied: %+v", *kd)
	}
}
