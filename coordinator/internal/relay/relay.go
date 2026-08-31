// Package relay orchestrates one inference job: pick a provider, seal the request to it,
// stream sealed chunks back, decrypt them, and forward plaintext deltas to the caller.
// The decrypted plaintext exists only as the argument to the onDelta callback and is
// never logged.
package relay

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/crypto"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/manifest"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
)

type Deps struct {
	Reg      *registry.Registry
	Job      *jobs.Manager
	Cfg      config.Config
	Log      *slog.Logger
	Manifest *manifest.Signer // nil unless SC_MANIFEST_SIGNING_KEY is set
}

type Request struct {
	Model      string
	Messages   []protocol.ChatMessage
	Params     protocol.SamplingParams
	MinTier    string
	MinContext int    // from the model's catalog entry; 0 = don't care
	HWClass    string // model's required hardware class; "" = don't care
}

type Result struct {
	Usage        protocol.Usage
	FinishReason string
	ProviderID   string
	ProviderPK   string // base64 X25519 static key — the durable payout identity
	TrustTier    string
	JobID        string
}

// ErrProviderGone means the chosen provider dropped mid-job.
var ErrProviderGone = errors.New("provider disconnected during job")

// ErrInvalidProviderResult means the assigned provider violated the result
// protocol: a frame for the wrong job, an out-of-order chunk sequence, output
// past the token ceiling, or an impossible prompt-token claim (threat model
// AYNI-004).
var ErrInvalidProviderResult = errors.New("provider returned an invalid result")

// ErrDeadline means the job ran past Cfg.JobDeadlineMS.
var ErrDeadline = errors.New("job exceeded deadline")

// Execute picks the best provider for req and runs it to completion, invoking
// onDelta for each decrypted token chunk. It blocks until the job finishes,
// errors, or ctx is cancelled (client disconnect), in which case a cancel is
// sent to the provider.
func Execute(ctx context.Context, d Deps, req Request, onDelta func(string) error) (*Result, error) {
	prov, err := scheduler.Pick(d.Reg, scheduler.Requirements{
		Model:      req.Model,
		MinTier:    req.MinTier,
		MinContext: req.MinContext,
		HWClass:    req.HWClass,
	})
	if err != nil {
		return nil, err
	}
	return ExecuteOn(ctx, d, prov, req, onDelta)
}

// ExecuteOn is Execute against an already-chosen provider. Batch fan-out uses it
// to place each sub-job itself instead of re-running the scheduler per item.
func ExecuteOn(ctx context.Context, d Deps, prov *registry.Provider, req Request, onDelta func(string) error) (*Result, error) {
	ephem, err := crypto.GenerateKeyPair()
	if err != nil {
		return nil, fmt.Errorf("ephemeral key: %w", err)
	}
	defer ephem.Zero()

	jobID := uuid.NewString()
	pt := protocol.JobRequestPlaintext{
		JobID:    jobID,
		Model:    req.Model,
		Messages: req.Messages,
		Params:   req.Params,
		Stream:   true,
	}
	ptRaw, err := json.Marshal(pt)
	if err != nil {
		return nil, fmt.Errorf("marshal job plaintext: %w", err)
	}
	sealed, err := crypto.Seal(ptRaw, prov.StaticPK, ephem.Secret, ephem.Public)
	if err != nil {
		return nil, fmt.Errorf("seal job: %w", err)
	}

	ch := d.Job.Register(jobID, prov.ID)
	defer d.Job.Close(jobID)
	prov.AcquireSlot()
	defer prov.ReleaseSlot()

	deadline := d.Cfg.JobDeadlineMS
	jr := protocol.JobRequest{
		Type:       protocol.TypeJobRequest,
		JobID:      jobID,
		Model:      req.Model,
		DeadlineMS: deadline,
		Sealed:     sealed,
	}
	if d.Manifest != nil {
		if mraw, merr := signJobManifest(d.Manifest, jobID, prov, req, ptRaw, deadline); merr != nil {
			d.Log.Warn("manifest sign failed", "job_id", jobID, "err", merr)
		} else {
			jr.Manifest = mraw
		}
	}
	if err := prov.Send(jr); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderGone, err)
	}

	d.Log.Info("job dispatched", "job_id", jobID, "provider_id", prov.ID,
		"model", req.Model, "tier", prov.TrustTier)

	jobDeadline := time.NewTimer(time.Duration(deadline) * time.Millisecond)
	defer jobDeadline.Stop()

	// The coordinator counts completion tokens itself, from the authenticated,
	// strictly-ordered chunk stream — the provider's self-reported usage is
	// advisory. maxPromptUnits is a coordinator-authored ceiling (not tokenizer
	// accurate) so a provider cannot claim an unbounded prompt-token count.
	expectedSeq := 0
	maxPromptUnits := 0
	for _, m := range req.Messages {
		maxPromptUnits += len(m.Role) + len(m.Content) + 16
	}

	for {
		select {
		case <-ctx.Done():
			_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "client-disconnect"})
			return nil, ctx.Err()

		case <-jobDeadline.C:
			_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "deadline"})
			return nil, fmt.Errorf("%w: job %s", ErrDeadline, jobID)

		case ev, ok := <-ch:
			if !ok {
				return nil, ErrProviderGone
			}
			switch ev.Kind {
			case jobs.KindChunk:
				var jc protocol.JobChunk
				if err := json.Unmarshal(ev.Raw, &jc); err != nil {
					return nil, fmt.Errorf("bad job_chunk: %w", err)
				}
				plain, err := crypto.Open(jc.Sealed, prov.StaticPK, ephem.Secret)
				if err != nil {
					_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "superseded"})
					return nil, crypto.ErrDecrypt
				}
				var cp protocol.JobChunkPlaintext
				if err := json.Unmarshal(plain, &cp); err != nil {
					return nil, fmt.Errorf("bad chunk plaintext: %w", err)
				}
				overCeiling := req.Params.MaxTokens > 0 && expectedSeq >= req.Params.MaxTokens
				if jc.JobID != jobID || cp.JobID != jobID || cp.Seq != expectedSeq || overCeiling {
					_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "invalid-result"})
					return nil, ErrInvalidProviderResult
				}
				expectedSeq++
				if cp.Delta != "" {
					if err := onDelta(cp.Delta); err != nil {
						_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "client-disconnect"})
						return nil, err
					}
				}

			case jobs.KindDone:
				var jd protocol.JobDone
				if err := json.Unmarshal(ev.Raw, &jd); err != nil {
					return nil, fmt.Errorf("bad job_done: %w", err)
				}
				if jd.JobID != jobID || jd.Usage.PromptTokens < 0 || jd.Usage.PromptTokens > maxPromptUnits {
					return nil, ErrInvalidProviderResult
				}
				// Completion count is the coordinator's own tally of the ordered
				// chunk stream; the provider's figure is replaced, not trusted.
				jd.Usage.CompletionTokens = expectedSeq
				jd.Usage.TotalTokens = jd.Usage.PromptTokens + expectedSeq
				return &Result{
					Usage:        jd.Usage,
					FinishReason: jd.FinishReason,
					ProviderID:   prov.ID,
					ProviderPK:   base64.StdEncoding.EncodeToString(prov.StaticPK[:]),
					TrustTier:    prov.TrustTier,
					JobID:        jobID,
				}, nil

			case jobs.KindError:
				var je protocol.JobError
				_ = json.Unmarshal(ev.Raw, &je)
				return nil, fmt.Errorf("provider job error: %s", je.Code)
			}
		}
	}
}

// signJobManifest builds and signs a Workload Manifest v1 for one job. Hashes
// are deterministic derivations for v1 (the node structurally validates them);
// real registry hashes land with the signed model-registry integration.
func signJobManifest(s *manifest.Signer, jobID string, prov *registry.Provider, req Request, ptRaw []byte, deadlineMS int64) (json.RawMessage, error) {
	now := time.Now().Unix()
	maxOut := req.Params.MaxTokens
	sm, err := s.Sign(manifest.WorkloadManifest{
		JobID:          jobID,
		LeaseID:        jobID,
		WorkloadID:     jobID,
		CustomerID:     "", // no consumer PII in the manifest
		DevicePK:       base64.StdEncoding.EncodeToString(prov.StaticPK[:]),
		RuntimeID:      "llama.cpp",
		RuntimeVersion: "v0",
		RuntimeHash:    manifest.Sha256Hex("llama.cpp@v0"),
		ModelID:        req.Model,
		ModelHash:      manifest.Sha256Hex(req.Model),
		InputHash:      manifest.Sha256Hex(string(ptRaw)),
		ResourceLimits: manifest.DefaultLimits(maxOut),
		CreatedAt:      now,
		ExpiresAt:      now + manifest.LeaseSeconds(deadlineMS),
		Nonce:          manifest.NewNonce(),
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(sm)
}

// ErrCode maps an Execute/ExecuteOn error to a stable short code and a
// human-readable message. Shared by the chat and batch handlers so a failure
// reads the same whichever surface produced it.
func ErrCode(err error) (code, msg string) {
	switch {
	case errors.Is(err, scheduler.ErrNoProvider):
		return "no_provider", "no provider is currently serving this model"
	case errors.Is(err, scheduler.ErrTierUnmet):
		return "trust_tier_unmet", "no connected provider meets the requested trust level"
	case errors.Is(err, scheduler.ErrCapsUnmet):
		return "capabilities_unmet", "no connected provider meets the model's resource requirements"
	case errors.Is(err, ErrProviderGone):
		return "provider_gone", "the assigned provider disconnected"
	case errors.Is(err, ErrInvalidProviderResult):
		return "invalid_provider_result", "the assigned provider returned an invalid result"
	case errors.Is(err, ErrDeadline):
		return "deadline", "the job exceeded the coordinator deadline"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "client_closed", "the request was cancelled"
	default:
		return "internal", "the request could not be completed"
	}
}
