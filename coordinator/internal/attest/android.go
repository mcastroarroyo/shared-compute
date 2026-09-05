package attest

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"golang.org/x/crypto/cryptobyte"
	casn1 "golang.org/x/crypto/cryptobyte/asn1"
)

//go:embed roots/google_hw_attestation_roots.pem
var googleRootsPEM []byte

// bindPrefix must match HardwareIdentity.BIND_PREFIX on the Android side.
var bindPrefix = []byte("ayni-bind-v1")

// keyAttestationExtOID is Google's "android key attestation" certificate extension.
var keyAttestationExtOID = []int{1, 3, 6, 1, 4, 1, 11129, 2, 1, 17}

// rootOfTrustTag is context-tag 704 in the KeyMint AuthorizationList.
const rootOfTrustTag = 704

// freshnessWindow bounds how stale the binding signature's timestamp may be.
const freshnessWindow = 5 * time.Minute

type androidKeyEvidence struct {
	CertChainDERB64 []string `json:"cert_chain_der_b64"`
	BindingSigB64   string   `json:"binding_sig_b64"`
	NonceB64        string   `json:"nonce_b64"`
	IssuedAt        int64    `json:"issued_at"`
}

var googleRoots = mustParseRoots(googleRootsPEM)

func mustParseRoots(pemBytes []byte) []*x509.Certificate {
	var out []*x509.Certificate
	rest := pemBytes
	for {
		var blk *pem.Block
		blk, rest = pem.Decode(rest)
		if blk == nil {
			break
		}
		if blk.Type != "CERTIFICATE" {
			continue
		}
		c, err := x509.ParseCertificate(blk.Bytes)
		if err != nil {
			panic("attest: bad embedded root: " + err.Error())
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		panic("attest: no embedded roots")
	}
	return out
}

func verifyAndroidKey(raw json.RawMessage, staticPK [32]byte) (*AndroidKeyResult, error) {
	var ev androidKeyEvidence
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, fmt.Errorf("android_key evidence: %w", err)
	}
	if len(ev.CertChainDERB64) < 2 {
		return nil, fmt.Errorf("android_key: chain too short (%d)", len(ev.CertChainDERB64))
	}

	chain := make([]*x509.Certificate, 0, len(ev.CertChainDERB64))
	for i, b := range ev.CertChainDERB64 {
		der, err := base64.StdEncoding.DecodeString(b)
		if err != nil {
			return nil, fmt.Errorf("android_key: cert[%d] base64: %w", i, err)
		}
		c, err := x509.ParseCertificate(der)
		if err != nil {
			return nil, fmt.Errorf("android_key: cert[%d] parse: %w", i, err)
		}
		chain = append(chain, c)
	}
	leaf := chain[0]

	if err := verifyChainToGoogleRoot(chain); err != nil {
		return nil, err
	}
	if err := verifyBinding(leaf, ev, staticPK); err != nil {
		return nil, err
	}

	kd, err := parseKeyDescription(leaf)
	if err != nil {
		return nil, err
	}
	res := &AndroidKeyResult{
		SecurityLevel:     securityLevelName(kd.securityLevel),
		VerifiedBootState: verifiedBootName(kd.verifiedBootState),
		DeviceLocked:      kd.deviceLocked,
	}
	if kd.securityLevel < 1 {
		return res, fmt.Errorf("android_key: security level %q is not hardware-backed", res.SecurityLevel)
	}
	if kd.verifiedBootState != 0 {
		return res, fmt.Errorf("android_key: verified boot state %q", res.VerifiedBootState)
	}
	if !kd.deviceLocked {
		return res, fmt.Errorf("android_key: bootloader unlocked")
	}
	return res, nil
}

// verifyChainToGoogleRoot checks each cert is signed by its successor and that the
// tail chains to (or is) a pinned Google hardware-attestation root. Android leaf
// certs routinely outlive their own NotAfter, so leaf expiry is not fatal; CA
// validity is enforced.
func verifyChainToGoogleRoot(chain []*x509.Certificate) error {
	return verifyChainToGoogleRootAt(chain, time.Now())
}

// verifyChainToGoogleRootAt is verifyChainToGoogleRoot evaluated at an explicit
// instant. Production always uses the wall clock (a phone presents a freshly
// provisioned chain on every registration — Google's RKP intermediates live
// ~2 weeks); tests pin `now` inside a captured fixture's validity window.
func verifyChainToGoogleRootAt(chain []*x509.Certificate, now time.Time) error {
	for i := 0; i < len(chain)-1; i++ {
		if err := chain[i].CheckSignatureFrom(chain[i+1]); err != nil {
			return fmt.Errorf("android_key: cert[%d] not signed by cert[%d]: %w", i, i+1, err)
		}
		if i > 0 && (now.Before(chain[i].NotBefore) || now.After(chain[i].NotAfter)) {
			return fmt.Errorf("android_key: intermediate cert[%d] outside validity window", i)
		}
	}
	tail := chain[len(chain)-1]
	for _, root := range googleRoots {
		if tail.Equal(root) {
			return nil
		}
		if err := tail.CheckSignatureFrom(root); err == nil {
			if now.Before(root.NotBefore) || now.After(root.NotAfter) {
				return fmt.Errorf("android_key: pinned root outside validity window")
			}
			return nil
		}
	}
	return fmt.Errorf("android_key: chain does not terminate at a pinned Google root")
}

func verifyBinding(leaf *x509.Certificate, ev androidKeyEvidence, staticPK [32]byte) error {
	if d := time.Since(time.Unix(ev.IssuedAt, 0)); d < -freshnessWindow || d > freshnessWindow {
		return fmt.Errorf("android_key: binding timestamp stale (%s off)", d.Round(time.Second))
	}
	nonce, err := base64.StdEncoding.DecodeString(ev.NonceB64)
	if err != nil || len(nonce) < 16 {
		return fmt.Errorf("android_key: bad nonce")
	}
	sig, err := base64.StdEncoding.DecodeString(ev.BindingSigB64)
	if err != nil {
		return fmt.Errorf("android_key: bad binding sig b64: %w", err)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("android_key: leaf key is %T, want ECDSA", leaf.PublicKey)
	}
	msg := make([]byte, 0, len(bindPrefix)+32+len(nonce)+8)
	msg = append(msg, bindPrefix...)
	msg = append(msg, staticPK[:]...)
	msg = append(msg, nonce...)
	var ts [8]byte
	binary.BigEndian.PutUint64(ts[:], uint64(ev.IssuedAt))
	msg = append(msg, ts[:]...)

	h := sha256.Sum256(msg)
	if !ecdsa.VerifyASN1(pub, h[:], sig) {
		return fmt.Errorf("android_key: binding signature does not verify against the attested key")
	}
	return nil
}

type keyDescription struct {
	securityLevel     int
	verifiedBootState int
	deviceLocked      bool
}

func parseKeyDescription(leaf *x509.Certificate) (*keyDescription, error) {
	var extn []byte
	for _, e := range leaf.Extensions {
		if e.Id.String() == oidString(keyAttestationExtOID) {
			extn = e.Value
			break
		}
	}
	if extn == nil {
		return nil, fmt.Errorf("android_key: leaf has no key-attestation extension")
	}

	input := cryptobyte.String(extn)
	var kd cryptobyte.String
	if !input.ReadASN1(&kd, casn1.SEQUENCE) {
		return nil, fmt.Errorf("android_key: KeyDescription is not a SEQUENCE")
	}
	var attVersion int
	if !kd.ReadASN1Integer(&attVersion) {
		return nil, fmt.Errorf("android_key: bad attestationVersion")
	}
	var secLevel int
	if !readEnum(&kd, &secLevel) {
		return nil, fmt.Errorf("android_key: bad attestationSecurityLevel")
	}
	var kmVersion int
	kd.ReadASN1Integer(&kmVersion)
	var kmSecLevel int
	readEnum(&kd, &kmSecLevel)
	var challenge, uniqueID cryptobyte.String
	kd.ReadASN1(&challenge, casn1.OCTET_STRING)
	kd.ReadASN1(&uniqueID, casn1.OCTET_STRING)
	var swEnforced, teeEnforced cryptobyte.String
	if !kd.ReadASN1(&swEnforced, casn1.SEQUENCE) || !kd.ReadASN1(&teeEnforced, casn1.SEQUENCE) {
		return nil, fmt.Errorf("android_key: bad AuthorizationList")
	}

	rot, ok := findContextTagged(teeEnforced, rootOfTrustTag)
	if !ok {
		rot, ok = findContextTagged(swEnforced, rootOfTrustTag)
	}
	if !ok {
		return nil, fmt.Errorf("android_key: no RootOfTrust in AuthorizationList")
	}

	out := &keyDescription{securityLevel: secLevel}
	body := cryptobyte.String(rot)
	var rotSeq cryptobyte.String
	if !body.ReadASN1(&rotSeq, casn1.SEQUENCE) {
		return nil, fmt.Errorf("android_key: RootOfTrust is not a SEQUENCE")
	}
	var vbKey cryptobyte.String
	if !rotSeq.ReadASN1(&vbKey, casn1.OCTET_STRING) {
		return nil, fmt.Errorf("android_key: RootOfTrust missing verifiedBootKey")
	}
	if !rotSeq.ReadASN1Boolean(&out.deviceLocked) {
		return nil, fmt.Errorf("android_key: RootOfTrust missing deviceLocked")
	}
	if !readEnum(&rotSeq, &out.verifiedBootState) {
		return nil, fmt.Errorf("android_key: RootOfTrust missing verifiedBootState")
	}
	return out, nil
}

// findContextTagged scans a DER SEQUENCE body for a context-class element whose tag
// number equals want (supports multi-byte / high tag numbers, which cryptobyte's
// uint8 Tag cannot represent), returning its content octets.
func findContextTagged(seq []byte, want uint64) ([]byte, bool) {
	b := seq
	for len(b) > 0 {
		if len(b) < 2 {
			return nil, false
		}
		first := b[0]
		class := first >> 6
		i := 1
		tagNum := uint64(first & 0x1f)
		if tagNum == 0x1f { // high-tag-number form
			tagNum = 0
			for {
				if i >= len(b) {
					return nil, false
				}
				c := b[i]
				i++
				tagNum = tagNum<<7 | uint64(c&0x7f)
				if c&0x80 == 0 {
					break
				}
			}
		}
		if i >= len(b) {
			return nil, false
		}
		l := int(b[i])
		i++
		if l&0x80 != 0 { // long-form length
			n := l & 0x7f
			if n == 0 || n > 4 || i+n > len(b) {
				return nil, false
			}
			l = 0
			for k := 0; k < n; k++ {
				l = l<<8 | int(b[i])
				i++
			}
		}
		if i+l > len(b) {
			return nil, false
		}
		content := b[i : i+l]
		if class == 0b10 && tagNum == want {
			return content, true
		}
		b = b[i+l:]
	}
	return nil, false
}

// readEnum reads an ASN.1 ENUMERATED (tag 0x0A) as an int.
func readEnum(s *cryptobyte.String, out *int) bool {
	var v cryptobyte.String
	if !s.ReadASN1(&v, casn1.Tag(10)) {
		return false
	}
	n := 0
	for _, c := range v {
		n = n<<8 | int(c)
	}
	*out = n
	return true
}

func oidString(oid []int) string {
	s := ""
	for i, n := range oid {
		if i > 0 {
			s += "."
		}
		s += fmt.Sprintf("%d", n)
	}
	return s
}

func securityLevelName(n int) string {
	switch n {
	case 0:
		return "software"
	case 1:
		return "tee"
	case 2:
		return "strongbox"
	default:
		return fmt.Sprintf("unknown(%d)", n)
	}
}

func verifiedBootName(n int) string {
	switch n {
	case 0:
		return "verified"
	case 1:
		return "self_signed"
	case 2:
		return "unverified"
	case 3:
		return "failed"
	default:
		return fmt.Sprintf("unknown(%d)", n)
	}
}
