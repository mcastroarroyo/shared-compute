package catalog

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The registry signing public key pinned in model-registry/PUBKEY.
const pinnedPub = "p7UUs6aCFebGUfSvFV5Wczh7kYBCEW2tDRO+dV0IpDM="

// findRepoFile walks up from the test dir to locate a repo-relative path.
func findRepoFile(t *testing.T, rel string) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 6; i++ {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		dir = filepath.Dir(dir)
	}
	t.Skipf("%s not found (run publish-registry.sh first)", rel)
	return ""
}

// Confirms the Go canonicalization matches sc-manifest's: the real signature over the
// real manifest verifies.
func TestVerifiesRealSignedManifest(t *testing.T) {
	mPath := findRepoFile(t, ".registry/manifest.json")
	sPath := findRepoFile(t, ".registry/manifest.json.sig")

	mBytes, err := os.ReadFile(mPath)
	if err != nil {
		t.Fatal(err)
	}
	sigB64, err := os.ReadFile(sPath)
	if err != nil {
		t.Fatal(err)
	}
	pub, _ := base64.StdEncoding.DecodeString(pinnedPub)
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil {
		t.Fatal(err)
	}

	canon, err := canonicalJSON(mBytes)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), canon, sig) {
		t.Fatalf("signature did not verify — Go canonical form differs from sc-manifest")
	}

	// Tamper: any change to the manifest must break verification.
	tampered := strings.Replace(string(mBytes), "\"MICRO\"", "\"LARGE\"", 1)
	canon2, _ := canonicalJSON([]byte(tampered))
	if ed25519.Verify(ed25519.PublicKey(pub), canon2, sig) {
		t.Fatalf("tampered manifest still verified")
	}
}

func TestCanonicalSortsKeys(t *testing.T) {
	a, err := canonicalJSON([]byte(`{"b":1,"a":{"y":2,"x":3},"c":[3,1,2]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"x":3,"y":2},"b":1,"c":[3,1,2]}`
	if string(a) != want {
		t.Fatalf("got %s want %s", a, want)
	}
}
