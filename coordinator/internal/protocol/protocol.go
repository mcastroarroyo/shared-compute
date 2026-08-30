// Package protocol is the Go mirror of /protocol/schemas (contract v0).
// Any change here must be mirrored in provider-core/sc-protocol and the JSON Schemas,
// with a bump to /protocol/VERSION, in the same change.
package protocol

import "encoding/json"

// Version is the wire protocol version. Sent as the "v" field on every message.
const Version = "0"

// Message types (the "type" field).
const (
	TypeRegister        = "register"
	TypeRegisterAck     = "register_ack"
	TypeHeartbeat       = "heartbeat"
	TypeHeartbeatAck    = "heartbeat_ack"
	TypeModelPull       = "model_pull"
	TypeModelReady      = "model_ready"
	TypeJobRequest      = "job_request"
	TypeJobChunk        = "job_chunk"
	TypeJobDone         = "job_done"
	TypeJobError        = "job_error"
	TypeCancel          = "cancel"
	TypeBenchmarkReport = "benchmark_report"
)

// SealAlg is the only accepted sealing algorithm in v0.
const SealAlg = "crypto_box-x25519-xsalsa20poly1305"

// Envelope is the common header on every WebSocket frame. Type-specific fields are
// carried alongside these in the same JSON object and unmarshalled separately.
type Envelope struct {
	V    string `json:"v"`
	Type string `json:"type"`
	ID   string `json:"id"`
	Re   string `json:"re,omitempty"`
	TS   int64  `json:"ts"`
}

// Frame carries the envelope plus the raw remaining JSON so a receiver can decode the
// type-specific struct after switching on Type.
type Frame struct {
	Envelope
	Raw json.RawMessage `json:"-"`
}

// SealedPayload is a NaCl crypto_box output plus the material needed to open it.
type SealedPayload struct {
	Alg        string `json:"alg"`
	EPK        string `json:"epk"`        // base64, coordinator ephemeral public key (32 bytes)
	Nonce      string `json:"nonce"`      // base64, 24 bytes
	Ciphertext string `json:"ciphertext"` // base64
}

// SecurityCapabilities is a provider's self-reported hardware-security posture.
type SecurityCapabilities struct {
	HardwareKey        bool `json:"hardware_key"`
	SecureBoot         bool `json:"secure_boot"`
	MeasuredBoot       bool `json:"measured_boot"`
	RuntimeAttested    bool `json:"runtime_attested"`
	ConfidentialMemory bool `json:"confidential_memory"`
	ConfidentialGPU    bool `json:"confidential_gpu"`
}

// Capabilities is what a provider advertises at registration.
type Capabilities struct {
	Platform      string               `json:"platform"`
	Arch          string               `json:"arch"`
	CPU           string               `json:"cpu,omitempty"`
	GPU           string               `json:"gpu,omitempty"`
	RAMMB         int                  `json:"ram_mb"`
	VRAMMB        int                  `json:"vram_mb,omitempty"`
	Backend       string               `json:"backend"`
	HardwareClass string               `json:"hardware_class"`
	MaxContext    int                  `json:"max_context"`
	Models        []string             `json:"models"`
	Security      SecurityCapabilities `json:"security_capabilities"`
}

// Telemetry is the dynamic state reported on each heartbeat.
type Telemetry struct {
	ActiveJobs     int     `json:"active_jobs"`
	QueueDepth     int     `json:"queue_depth"`
	CPULoad        float64 `json:"cpu_load"`
	MemAvailableMB int     `json:"mem_available_mb"`
	ThermalState   string  `json:"thermal_state"`
	BatteryPct     *int    `json:"battery_pct,omitempty"`
	Charging       *bool   `json:"charging,omitempty"`
	Network        string  `json:"network,omitempty"`
}

// Usage is token accounting for a completed job (metadata, not content).
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Attestation is opaque, tier-specific evidence. v0 accepts kind "none".
type Attestation struct {
	Kind     string          `json:"kind"`
	Evidence json.RawMessage `json:"evidence,omitempty"`
}

// --- provider -> coordinator ---

type Register struct {
	Type              string       `json:"type"`
	Agent             string       `json:"agent"`
	RegistrationToken string       `json:"registration_token"`
	StaticPK          string       `json:"static_pk"`
	Capabilities      Capabilities `json:"capabilities"`
	Attestation       *Attestation `json:"attestation,omitempty"`
	ResumeProviderID  string       `json:"resume_provider_id,omitempty"`
}

type Heartbeat struct {
	Type      string    `json:"type"`
	Telemetry Telemetry `json:"telemetry"`
}

type ModelReady struct {
	Type   string `json:"type"`
	Model  string `json:"model"`
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Bytes  int64  `json:"bytes,omitempty"`
}

type JobChunk struct {
	Type   string        `json:"type"`
	JobID  string        `json:"job_id"`
	Sealed SealedPayload `json:"sealed"`
}

type JobDone struct {
	Type         string  `json:"type"`
	JobID        string  `json:"job_id"`
	Usage        Usage   `json:"usage"`
	FinishReason string  `json:"finish_reason"`
	DurationMS   int64   `json:"duration_ms"`
	PrefillTPS   float64 `json:"prefill_tps,omitempty"`
	DecodeTPS    float64 `json:"decode_tps,omitempty"`
}

type JobError struct {
	Type    string `json:"type"`
	JobID   string `json:"job_id"`
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

type BenchmarkReport struct {
	Type          string  `json:"type"`
	Model         string  `json:"model"`
	Backend       string  `json:"backend"`
	PrefillTPS    float64 `json:"prefill_tps"`
	DecodeTPS     float64 `json:"decode_tps"`
	ContextTested int     `json:"context_tested,omitempty"`
	SampleMS      int     `json:"sample_ms,omitempty"`
}

// --- coordinator -> provider ---

type RegisterAck struct {
	Type             string   `json:"type"`
	OK               bool     `json:"ok"`
	Error            string   `json:"error,omitempty"`
	ProviderID       string   `json:"provider_id,omitempty"`
	TrustTier        string   `json:"trust_tier,omitempty"`
	AcceptedModels   []string `json:"accepted_models,omitempty"`
	HeartbeatSeconds int      `json:"heartbeat_seconds,omitempty"`
	CoordinatorPK    string   `json:"coordinator_pk,omitempty"`
}

type HeartbeatAck struct {
	Type      string `json:"type"`
	ServerTS  int64  `json:"server_ts"`
	Directive string `json:"directive,omitempty"`
}

type ModelPull struct {
	Type        string `json:"type"`
	Model       string `json:"model"`
	ManifestURL string `json:"manifest_url"`
	Priority    string `json:"priority,omitempty"`
}

type JobRequest struct {
	Type       string        `json:"type"`
	JobID      string        `json:"job_id"`
	Model      string        `json:"model"`
	DeadlineMS int64         `json:"deadline_ms,omitempty"`
	Sealed     SealedPayload `json:"sealed"`
}

type Cancel struct {
	Type   string `json:"type"`
	JobID  string `json:"job_id"`
	Reason string `json:"reason"`
}

// --- sealed plaintexts ---

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type SamplingParams struct {
	MaxTokens     int      `json:"max_tokens"`
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	RepeatPenalty *float64 `json:"repeat_penalty,omitempty"`
	Stop          []string `json:"stop,omitempty"`
	Seed          *int64   `json:"seed,omitempty"`
}

// JobRequestPlaintext is sealed inside JobRequest.Sealed.
type JobRequestPlaintext struct {
	JobID    string         `json:"job_id"`
	Model    string         `json:"model"`
	Messages []ChatMessage  `json:"messages"`
	Params   SamplingParams `json:"params"`
	Stream   bool           `json:"stream"`
}

// JobChunkPlaintext is sealed inside JobChunk.Sealed.
type JobChunkPlaintext struct {
	JobID string `json:"job_id"`
	Seq   int    `json:"seq"`
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
}
