package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
)

func TestInstallScriptRenders(t *testing.T) {
	s := &Server{cfg: config.Config{
		AuthCallbackBase: "https://api.example.com",
		ManifestURL:      "https://models.example.com",
	}}
	rec := httptest.NewRecorder()
	s.handleInstallScript(rec, httptest.NewRequest("GET", "/install/provider.sh", nil))

	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()

	// the Go template must not leak doubled braces
	if strings.Contains(body, "${{") || strings.Contains(body, "}}") {
		t.Fatalf("unrendered braces in script:\n%s", body)
	}
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		`COORD_WS="${SC_COORDINATOR_URL:-wss://api.example.com/ws/provider}"`,
		`MODEL_URL="https://models.example.com/${MODEL}/${MODEL}.gguf"`,
		"releases/download/provider-latest/${asset}",
		`SC_REGISTRATION_TOKEN="$SC_REGISTRATION_TOKEN"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/x-shellscript") {
		t.Errorf("content-type = %q", ct)
	}
}

func TestInstallScriptDefaults(t *testing.T) {
	s := &Server{cfg: config.Config{}} // no config at all
	rec := httptest.NewRecorder()
	s.handleInstallScript(rec, httptest.NewRequest("GET", "/install/provider.sh", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "wss://api.ayni-ai.com/ws/provider") {
		t.Errorf("missing default coordinator ws URL:\n%s", body)
	}
	if !strings.Contains(body, "https://models.ayni-ai.com/") {
		t.Errorf("missing default model URL")
	}
	for _, want := range []string{`https://api.ayni-ai.com/v1/manifest-key`, `SC_MANIFEST_VERIFY_KEY="$VERIFY_KEY"`, `SC_REQUIRE_MANIFEST="${SC_REQUIRE_MANIFEST:-1}"`} {
		if !strings.Contains(body, want) {
			t.Errorf("installer must pin the manifest key; missing %q", want)
		}
	}
}
