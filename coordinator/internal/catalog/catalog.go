// Package catalog serves the model list from the signed registry manifest
// (models.<domain>/manifest.json + .sig), verified with a pinned Ed25519 key. It mirrors
// provider-core/sc-manifest. When no manifest URL is configured, the catalog is empty and
// /v1/models falls back to the union of connected providers' advertised models.
package catalog

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

type ManifestFile struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}

type Model struct {
	ModelID         string         `json:"model_id"`
	Architecture    string         `json:"architecture"`
	Quantization    string         `json:"quantization"`
	HardwareClass   string         `json:"hardware_class"`
	ContextLength   int            `json:"context_length"`
	AggregateSHA256 string         `json:"aggregate_sha256"`
	Files           []ManifestFile `json:"files"`
	TotalBytes      int64          `json:"total_bytes"`
}

type manifest struct {
	Version     int     `json:"version"`
	GeneratedAt string  `json:"generated_at"`
	Models      []Model `json:"models"`
}

// Catalog holds the last successfully verified manifest and refreshes it in the
// background.
type Catalog struct {
	baseURL string
	pub     ed25519.PublicKey
	log     *slog.Logger
	http    *http.Client

	mu     sync.RWMutex
	models []Model
	loaded time.Time
}

// New builds a catalog. baseURL "" disables it (Enabled() == false).
func New(baseURL, pubKeyB64 string, log *slog.Logger) (*Catalog, error) {
	c := &Catalog{
		baseURL: baseURL,
		log:     log,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
	if baseURL == "" {
		return c, nil
	}
	raw, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("SC_REGISTRY_PUBKEY must be a base64 Ed25519 public key (32 bytes)")
	}
	c.pub = ed25519.PublicKey(raw)
	return c, nil
}

func (c *Catalog) Enabled() bool { return c.baseURL != "" }

// Models returns the currently loaded catalog (may be empty before the first refresh).
func (c *Catalog) Models() []Model {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Model, len(c.models))
	copy(out, c.models)
	return out
}

// ClassOf returns the hardware/model class for a model id, or "" if unknown.
func (c *Catalog) ClassOf(model string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, m := range c.models {
		if m.ModelID == model {
			return m.HardwareClass
		}
	}
	return ""
}

// Has reports whether model is in the loaded catalog.
func (c *Catalog) Has(model string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, m := range c.models {
		if m.ModelID == model {
			return true
		}
	}
	return false
}

// Refresh fetches manifest.json + manifest.json.sig, verifies the signature over the
// canonical form, and swaps in the new model list.
func (c *Catalog) Refresh(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	mBytes, err := c.get(ctx, "/manifest.json")
	if err != nil {
		return fmt.Errorf("fetch manifest: %w", err)
	}
	sigB64, err := c.get(ctx, "/manifest.json.sig")
	if err != nil {
		return fmt.Errorf("fetch signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(sigB64)))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	canon, err := canonicalJSON(mBytes)
	if err != nil {
		return fmt.Errorf("canonicalize: %w", err)
	}
	if !ed25519.Verify(c.pub, canon, sig) {
		return fmt.Errorf("manifest signature verification failed")
	}

	var m manifest
	if err := json.Unmarshal(mBytes, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	c.mu.Lock()
	c.models = m.Models
	c.loaded = time.Now()
	c.mu.Unlock()
	c.log.Info("catalog refreshed", "models", len(m.Models), "generated_at", m.GeneratedAt)
	return nil
}

// RunRefreshLoop refreshes now and then every interval until ctx is done.
func (c *Catalog) RunRefreshLoop(ctx context.Context, interval time.Duration) {
	if !c.Enabled() {
		return
	}
	if err := c.Refresh(ctx); err != nil {
		c.log.Warn("initial catalog refresh failed", "err", err)
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.Refresh(ctx); err != nil {
				c.log.Warn("catalog refresh failed", "err", err)
			}
		}
	}
}

func (c *Catalog) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", path, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

// canonicalJSON produces the same bytes as provider-core/sc-manifest's canonical form:
// recursively sorted object keys, compact separators, numbers preserved verbatim.
func canonicalJSON(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	// json.Marshal sorts map[string]any keys and emits compact output; json.Number
	// marshals as its literal text.
	return json.Marshal(v)
}
