// Package relay orchestrates one inference job: pick a provider, seal the request to it,
// stream sealed chunks back, decrypt them, and forward plaintext deltas to the caller.
// The decrypted plaintext exists only as the argument to the onDelta callback and is
// never logged.
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/crypto"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/scheduler"
)

type Deps struct {
	Reg *registry.Registry
	Job *jobs.Manager
	Cfg config.Config
	Log *slog.Logger
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
	TrustTier    string
	JobID        string
}

// ErrProviderGone means the chosen provider dropped mid-job.
var ErrProviderGone = errors.New("provider disconnected during job")

// Execute runs req to completion, invoking onDelta for each decrypted token chunk.
// It blocks until the job finishes, errors, or ctx is cancelled (client disconnect),
// in which case a cancel is sent to the provider.
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

	ch := d.Job.Register(jobID)
	defer d.Job.Close(jobID)
	prov.AcquireSlot()
	defer prov.ReleaseSlot()

	deadline := d.Cfg.JobDeadlineMS
	if err := prov.Send(protocol.JobRequest{
		Type:       protocol.TypeJobRequest,
		JobID:      jobID,
		Model:      req.Model,
		DeadlineMS: deadline,
		Sealed:     sealed,
	}); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrProviderGone, err)
	}

	d.Log.Info("job dispatched", "job_id", jobID, "provider_id", prov.ID,
		"model", req.Model, "tier", prov.TrustTier)

	jobDeadline := time.NewTimer(time.Duration(deadline) * time.Millisecond)
	defer jobDeadline.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "client-disconnect"})
			return nil, ctx.Err()

		case <-jobDeadline.C:
			_ = prov.Send(protocol.Cancel{Type: protocol.TypeCancel, JobID: jobID, Reason: "deadline"})
			return nil, fmt.Errorf("job %s exceeded deadline", jobID)

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
				return &Result{
					Usage:        jd.Usage,
					FinishReason: jd.FinishReason,
					ProviderID:   prov.ID,
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
