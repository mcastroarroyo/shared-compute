// Package crypto wraps NaCl crypto_box (X25519 + XSalsa20-Poly1305) for the job-sealing
// envelope described in /protocol/envelope.md.
package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"golang.org/x/crypto/nacl/box"
)

// KeyPair is an X25519 keypair.
type KeyPair struct {
	Public [32]byte
	Secret [32]byte
}

// GenerateKeyPair returns a fresh X25519 keypair. The coordinator calls this once per job
// and zeroizes Secret via (KeyPair).Zero when the job ends.
func GenerateKeyPair() (KeyPair, error) {
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("generate keypair: %w", err)
	}
	return KeyPair{Public: *pub, Secret: *priv}, nil
}

// Zero wipes the secret key.
func (k *KeyPair) Zero() {
	for i := range k.Secret {
		k.Secret[i] = 0
	}
}

// ParsePublicKey decodes a base64 32-byte X25519 public key.
func ParsePublicKey(b64 string) ([32]byte, error) {
	var out [32]byte
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return out, fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != 32 {
		return out, fmt.Errorf("public key must be 32 bytes, got %d", len(raw))
	}
	copy(out[:], raw)
	return out, nil
}

// EncodeKey base64-encodes a 32-byte key.
func EncodeKey(k [32]byte) string { return base64.StdEncoding.EncodeToString(k[:]) }

// Seal encrypts plaintext from senderSecret to recipientPub with a fresh random nonce.
// epkToEmbed is placed in the payload's "epk" field (the coordinator's per-job ephemeral
// public key, echoed in both directions so the peer knows which key agreement to use).
func Seal(plaintext []byte, recipientPub, senderSecret, epkToEmbed [32]byte) (protocol.SealedPayload, error) {
	var nonce [24]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return protocol.SealedPayload{}, fmt.Errorf("nonce: %w", err)
	}
	sealed := box.Seal(nil, plaintext, &nonce, &recipientPub, &senderSecret)
	return protocol.SealedPayload{
		Alg:        protocol.SealAlg,
		EPK:        base64.StdEncoding.EncodeToString(epkToEmbed[:]),
		Nonce:      base64.StdEncoding.EncodeToString(nonce[:]),
		Ciphertext: base64.StdEncoding.EncodeToString(sealed),
	}, nil
}

// ErrDecrypt is returned for any failure to open a sealed payload. It is deliberately
// generic so no detail about the failure leaks to the peer.
var ErrDecrypt = errors.New("decrypt")

// Open decrypts a sealed payload sent by senderPub to recipientSecret.
func Open(p protocol.SealedPayload, senderPub, recipientSecret [32]byte) ([]byte, error) {
	if p.Alg != protocol.SealAlg {
		return nil, ErrDecrypt
	}
	nonceRaw, err := base64.StdEncoding.DecodeString(p.Nonce)
	if err != nil || len(nonceRaw) != 24 {
		return nil, ErrDecrypt
	}
	ct, err := base64.StdEncoding.DecodeString(p.Ciphertext)
	if err != nil {
		return nil, ErrDecrypt
	}
	var nonce [24]byte
	copy(nonce[:], nonceRaw)
	out, ok := box.Open(nil, ct, &nonce, &senderPub, &recipientSecret)
	if !ok {
		return nil, ErrDecrypt
	}
	return out, nil
}
