// Package wshub terminates provider WebSocket connections: registration, heartbeats, and
// routing of job frames back to the relay.
package wshub

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/attest"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/crypto"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/metrics"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/store"
)

const (
	registerTimeout = 10 * time.Second
	writeTimeout    = 10 * time.Second
	readLimitBytes  = 1 << 20 // 1 MiB; job chunks are small
)

// Hub handles /ws/provider.
type Hub struct {
	Cfg   config.Config
	Reg   *registry.Registry
	Job   *jobs.Manager
	Store store.Store
	Log   *slog.Logger
}

// conn wraps a websocket with a write mutex; concurrent writers are serialized.
type conn struct {
	ws *websocket.Conn
	mu sync.Mutex
}

func (c *conn) send(v any) error {
	raw, err := protocol.Marshal(v, "")
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
	defer cancel()
	return c.ws.Write(ctx, websocket.MessageText, raw)
}

// HandleProvider upgrades the request and serves one provider until it disconnects.
func (h *Hub) HandleProvider(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{})
	if err != nil {
		h.Log.Warn("ws accept failed", "err", err)
		return
	}
	ws.SetReadLimit(readLimitBytes)
	c := &conn{ws: ws}
	defer ws.CloseNow() //nolint:errcheck

	// --- registration ---
	regCtx, cancel := context.WithTimeout(r.Context(), registerTimeout)
	_, raw, err := ws.Read(regCtx)
	cancel()
	if err != nil {
		_ = ws.Close(websocket.StatusPolicyViolation, "no register frame")
		return
	}
	frame, err := protocol.Parse(raw)
	if err != nil || frame.Type != protocol.TypeRegister {
		_ = c.send(protocol.RegisterAck{Type: protocol.TypeRegisterAck, OK: false, Error: "protocol-version"})
		_ = ws.Close(websocket.StatusPolicyViolation, "expected register")
		return
	}
	var reg protocol.Register
	if err := frame.As(&reg); err != nil {
		_ = c.send(protocol.RegisterAck{Type: protocol.TypeRegisterAck, OK: false, Error: "internal"})
		return
	}
	if !h.Store.ValidateProviderToken(r.Context(), reg.RegistrationToken) {
		_ = c.send(protocol.RegisterAck{Type: protocol.TypeRegisterAck, OK: false, Error: "bad-token"})
		_ = ws.Close(websocket.StatusPolicyViolation, "bad token")
		return
	}
	staticPK, err := crypto.ParsePublicKey(reg.StaticPK)
	if err != nil {
		_ = c.send(protocol.RegisterAck{Type: protocol.TypeRegisterAck, OK: false, Error: "bad-key"})
		_ = ws.Close(websocket.StatusPolicyViolation, "bad key")
		return
	}

	// Trust tier: verify any attestation evidence; unverified evidence is not an
	// error — the node simply joins as community.
	tier := attest.TierCommunity
	if reg.Attestation != nil && reg.Attestation.Kind != "" && reg.Attestation.Kind != "none" {
		res, aerr := attest.Verify(&attest.Evidence{
			Kind:     reg.Attestation.Kind,
			Evidence: reg.Attestation.Evidence,
		}, staticPK)
		tier = res.Tier
		if aerr != nil {
			h.Log.Warn("attestation did not clear a higher tier",
				"kind", reg.Attestation.Kind, "err", aerr)
		} else if res.AndroidKey != nil {
			h.Log.Info("android key attestation verified",
				"security_level", res.AndroidKey.SecurityLevel,
				"verified_boot", res.AndroidKey.VerifiedBootState,
				"device_locked", res.AndroidKey.DeviceLocked)
		}
	}

	p := &registry.Provider{
		ID:           uuid.NewString(),
		StaticPK:     staticPK,
		Capabilities: reg.Capabilities,
		TrustTier:    tier,
		ConnectedAt:  time.Now(),
		LastSeen:     time.Now(),
		Send:         c.send,
	}
	h.Reg.Add(p)
	metrics.ProvidersConnected.Inc()
	if err := h.Store.UpsertProvider(r.Context(), store.ProviderRecord{
		ProviderID: p.ID,
		StaticPK:   reg.StaticPK,
		Platform:   reg.Capabilities.Platform,
		Arch:       reg.Capabilities.Arch,
		Backend:    reg.Capabilities.Backend,
	}); err != nil {
		h.Log.Warn("upsert provider failed", "err", err)
	}
	h.Log.Info("provider registered",
		"provider_id", p.ID,
		"platform", reg.Capabilities.Platform,
		"backend", reg.Capabilities.Backend,
		"models", len(reg.Capabilities.Models),
		"tier", tier,
	)
	defer func() {
		h.Reg.Remove(p.ID)
		metrics.ProvidersConnected.Dec()
		h.Log.Info("provider disconnected", "provider_id", p.ID)
	}()

	if err := c.send(protocol.RegisterAck{
		Type:             protocol.TypeRegisterAck,
		OK:               true,
		ProviderID:       p.ID,
		TrustTier:        tier,
		AcceptedModels:   reg.Capabilities.Models,
		HeartbeatSeconds: h.Cfg.HeartbeatSeconds,
	}); err != nil {
		return
	}

	// --- main loop ---
	missed := 0
	for {
		readCtx, rc := context.WithTimeout(r.Context(), time.Duration(h.Cfg.HeartbeatSeconds*3)*time.Second)
		_, raw, err := ws.Read(readCtx)
		rc()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				missed++
				if missed < 2 {
					continue
				}
			}
			return
		}
		missed = 0

		frame, err := protocol.Parse(raw)
		if err != nil {
			h.Log.Warn("bad frame from provider", "provider_id", p.ID, "err", err)
			continue
		}
		p.LastSeen = time.Now()
		h.dispatch(c, p, frame)
	}
}

func (h *Hub) dispatch(c *conn, p *registry.Provider, frame protocol.Frame) {
	switch frame.Type {
	case protocol.TypeHeartbeat:
		var hb protocol.Heartbeat
		if err := frame.As(&hb); err == nil {
			p.Telemetry = hb.Telemetry
		}
		_ = c.send(protocol.HeartbeatAck{
			Type:     protocol.TypeHeartbeatAck,
			ServerTS: time.Now().Unix(),
		})

	case protocol.TypeModelReady:
		var mr protocol.ModelReady
		_ = frame.As(&mr)
		h.Log.Info("model_ready", "provider_id", p.ID, "model", mr.Model, "ok", mr.OK, "err", mr.Error)

	case protocol.TypeBenchmarkReport:
		var br protocol.BenchmarkReport
		_ = frame.As(&br)
		pk := base64.StdEncoding.EncodeToString(p.StaticPK[:])
		fp := capability.Fingerprint{
			Model: br.Model, Backend: br.Backend,
			PrefillTPS: br.PrefillTPS, DecodeTPS: br.DecodeTPS,
			SustainedStartTPS: br.SustainedStartTPS, SustainedEndTPS: br.SustainedEndTPS,
			MemBandwidthGBps: br.MemBandwidthGBps, AvailableRAMMB: br.AvailableRAMMB,
			CPUCores: br.CPUCores, ThermalState: br.ThermalState,
		}
		h.Log.Info("benchmark_report", "provider_id", p.ID, "model", br.Model,
			"decode_tps", br.DecodeTPS, "sustained_end_tps", br.SustainedEndTPS,
			"mem_gbps", br.MemBandwidthGBps, "acu", capability.ACU(fp),
			"class", capability.Class(fp))
		if err := h.Store.UpsertNodeCapability(context.Background(), store.NodeCapabilityRow{
			StaticPK: pk, Model: br.Model, Backend: br.Backend,
			PrefillTPS: br.PrefillTPS, DecodeTPS: br.DecodeTPS,
			SustainedStartTPS: br.SustainedStartTPS, SustainedEndTPS: br.SustainedEndTPS,
			MemBandwidthGBps: br.MemBandwidthGBps, AvailableRAMMB: br.AvailableRAMMB,
			CPUCores: br.CPUCores, ThermalState: br.ThermalState,
		}); err != nil {
			h.Log.Warn("upsert node capability failed", "err", err)
		}

	case protocol.TypeJobChunk:
		h.deliver(p, frame, jobs.KindChunk)
	case protocol.TypeJobDone:
		h.deliver(p, frame, jobs.KindDone)
	case protocol.TypeJobError:
		h.deliver(p, frame, jobs.KindError)

	default:
		h.Log.Debug("ignoring frame", "type", frame.Type, "provider_id", p.ID)
	}
}

func (h *Hub) deliver(p *registry.Provider, frame protocol.Frame, kind jobs.Kind) {
	var probe struct {
		JobID string `json:"job_id"`
	}
	if err := json.Unmarshal(frame.Raw, &probe); err != nil || probe.JobID == "" {
		return
	}
	if !h.Job.Deliver(probe.JobID, p.ID, jobs.Event{Kind: kind, Raw: frame.Raw}) {
		h.Log.Debug("job frame for unknown/slow job", "provider_id", p.ID, "job_id", probe.JobID)
	}
}
