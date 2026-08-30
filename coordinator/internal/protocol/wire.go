package protocol

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Marshal serializes a typed payload struct into a full wire frame, injecting the
// envelope fields (v, id, ts) and ensuring "type" is set. re is optional (echo id).
func Marshal(payload any, re string) ([]byte, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("payload is not a JSON object: %w", err)
	}
	m["v"], _ = json.Marshal(Version)
	m["id"], _ = json.Marshal(uuid.NewString())
	m["ts"], _ = json.Marshal(time.Now().Unix())
	if re != "" {
		m["re"], _ = json.Marshal(re)
	}
	if _, ok := m["type"]; !ok {
		return nil, fmt.Errorf("payload has no \"type\" field")
	}
	return json.Marshal(m)
}

// Parse splits a raw inbound frame into its envelope and keeps the raw bytes so the
// caller can unmarshal the type-specific struct after switching on Type.
func Parse(raw []byte) (Frame, error) {
	var env Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Frame{}, fmt.Errorf("parse envelope: %w", err)
	}
	if env.V != Version {
		return Frame{}, fmt.Errorf("unsupported protocol version %q", env.V)
	}
	if env.Type == "" {
		return Frame{}, fmt.Errorf("missing type")
	}
	return Frame{Envelope: env, Raw: append([]byte(nil), raw...)}, nil
}

// As unmarshals the frame's raw bytes into v (a pointer to a type-specific struct).
func (f Frame) As(v any) error {
	return json.Unmarshal(f.Raw, v)
}
