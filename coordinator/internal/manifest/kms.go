package manifest

import (
	"context"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
)

// KMSSigner signs Workload Manifests with a Cloud KMS EC_SIGN_ED25519 key
// version. The private key never leaves KMS; the coordinator only holds the
// public key and the right to call AsymmetricSign (roles/cloudkms.signerVerifier).
type KMSSigner struct {
	id         string
	keyVersion string // projects/.../locations/.../keyRings/.../cryptoKeys/.../cryptoKeyVersions/N
	client     *kms.KeyManagementClient
	pub        ed25519.PublicKey
	timeout    time.Duration
}

// NewKMSSigner connects to Cloud KMS with Application Default Credentials and
// fetches the key version's public key. keyVersion must be a full
// cryptoKeyVersions resource name.
func NewKMSSigner(ctx context.Context, id, keyVersion string) (*KMSSigner, error) {
	if !strings.Contains(keyVersion, "/cryptoKeyVersions/") {
		return nil, fmt.Errorf("kms signer: key must be a cryptoKeyVersions resource name")
	}
	if id == "" {
		id = "signer-kms-v1"
	}
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("kms client: %w", err)
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := client.GetPublicKey(cctx, &kmspb.GetPublicKeyRequest{Name: keyVersion})
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("kms get public key: %w", err)
	}
	if resp.GetAlgorithm() != kmspb.CryptoKeyVersion_EC_SIGN_ED25519 {
		_ = client.Close()
		return nil, fmt.Errorf("kms signer: key algorithm %s is not EC_SIGN_ED25519", resp.GetAlgorithm())
	}
	block, _ := pem.Decode([]byte(resp.GetPem()))
	if block == nil {
		_ = client.Close()
		return nil, fmt.Errorf("kms signer: public key is not PEM")
	}
	pubAny, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("kms signer: parse public key: %w", err)
	}
	pub, ok := pubAny.(ed25519.PublicKey)
	if !ok {
		_ = client.Close()
		return nil, fmt.Errorf("kms signer: public key is not Ed25519")
	}
	return &KMSSigner{id: id, keyVersion: keyVersion, client: client, pub: pub, timeout: 10 * time.Second}, nil
}

func (k *KMSSigner) ID() string           { return k.id }
func (k *KMSSigner) PublicKeyB64() string { return base64.StdEncoding.EncodeToString(k.pub) }

// Sign canonicalizes the manifest and asks KMS for an Ed25519 signature over the
// full data (Ed25519 signs the message, not a digest). The result is verified
// locally against the cached public key before it is trusted.
func (k *KMSSigner) Sign(m WorkloadManifest) (SignedManifest, error) {
	cb, err := prepare(&m)
	if err != nil {
		return SignedManifest{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), k.timeout)
	defer cancel()
	resp, err := k.client.AsymmetricSign(ctx, &kmspb.AsymmetricSignRequest{Name: k.keyVersion, Data: cb})
	if err != nil {
		return SignedManifest{}, fmt.Errorf("kms sign: %w", err)
	}
	sig := resp.GetSignature()
	if len(sig) != ed25519.SignatureSize || !ed25519.Verify(k.pub, cb, sig) {
		return SignedManifest{}, fmt.Errorf("kms sign: signature failed local verification")
	}
	return SignedManifest{Manifest: m, SignerID: k.id, Signature: base64.StdEncoding.EncodeToString(sig)}, nil
}

// Close releases the KMS client.
func (k *KMSSigner) Close() error { return k.client.Close() }
