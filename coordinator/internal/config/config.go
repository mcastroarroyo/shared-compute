// Package config loads coordinator configuration from the environment.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	// HTTPAddr is the listen address for the consumer API + provider WebSocket.
	HTTPAddr string
	// ConsumerAPIKeys is the set of accepted Bearer keys (M1: static from env; M2: Postgres).
	ConsumerAPIKeys map[string]struct{}
	// ProviderRegistrationTokens is the set of accepted provider registration tokens.
	ProviderRegistrationTokens map[string]struct{}
	// HeartbeatSeconds is advertised to providers in register_ack.
	HeartbeatSeconds int
	// ManifestURL is where /v1/models and model_pull point (M3+). Optional in M1.
	ManifestURL string
	// JobDeadlineMS bounds a single inference job.
	JobDeadlineMS int64
}

func Load() (Config, error) {
	c := Config{
		HTTPAddr:                   getenv("SC_HTTP_ADDR", ":8080"),
		ConsumerAPIKeys:            set(getenv("SC_CONSUMER_API_KEYS", "dev-consumer-key")),
		ProviderRegistrationTokens: set(getenv("SC_PROVIDER_TOKENS", "dev-provider-token")),
		HeartbeatSeconds:           getenvInt("SC_HEARTBEAT_SECONDS", 15),
		ManifestURL:                getenv("SC_MANIFEST_URL", ""),
		JobDeadlineMS:              int64(getenvInt("SC_JOB_DEADLINE_MS", 120000)),
	}
	if len(c.ConsumerAPIKeys) == 0 {
		return c, fmt.Errorf("SC_CONSUMER_API_KEYS must not be empty")
	}
	if len(c.ProviderRegistrationTokens) == 0 {
		return c, fmt.Errorf("SC_PROVIDER_TOKENS must not be empty")
	}
	if c.HeartbeatSeconds < 5 || c.HeartbeatSeconds > 120 {
		return c, fmt.Errorf("SC_HEARTBEAT_SECONDS out of range [5,120]")
	}
	return c, nil
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// set parses a comma-separated env value into a set, trimming whitespace and empties.
func set(csv string) map[string]struct{} {
	m := make(map[string]struct{})
	for _, p := range strings.Split(csv, ",") {
		p = strings.TrimSpace(p)
		if p != "" {
			m[p] = struct{}{}
		}
	}
	return m
}
