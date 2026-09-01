package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/ratelimit"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
)

func demoPOST(s *Server, body string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/demo/summarize", strings.NewReader(body))
	r.RemoteAddr = "203.0.113.7:5555"
	s.handleDemoSummarize(rec, r)
	return rec
}

func TestDemoDisabledIs404(t *testing.T) {
	s := &Server{cfg: config.Config{DemoEnabled: false}, demoRL: ratelimit.New(3), demo: &demoState{}}
	if rec := demoPOST(s, `{"text":"`+strings.Repeat("word ", 40)+`"}`); rec.Code != 404 {
		t.Fatalf("disabled demo should 404, got %d", rec.Code)
	}
}

func TestDemoRejectsShortText(t *testing.T) {
	s := &Server{cfg: config.Config{DemoEnabled: true}, demoRL: ratelimit.New(3), demo: &demoState{}}
	rec := demoPOST(s, `{"text":"too short"}`)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "too_short") {
		t.Fatalf("short text should 400 too_short, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestDemoRateLimitedPerIP(t *testing.T) {
	s := &Server{cfg: config.Config{DemoEnabled: true}, demoRL: ratelimit.New(2), demo: &demoState{}}
	got := map[int]int{}
	for i := 0; i < 5; i++ {
		got[demoPOST(s, `{"text":"x"}`).Code]++ // rate check runs before the length check
	}
	if got[429] == 0 {
		t.Fatalf("expected some 429s after the burst, got %v", got)
	}
}

func TestDemoLastJobEmpty(t *testing.T) {
	s := &Server{cfg: config.Config{DemoEnabled: true}, demo: &demoState{}}
	rec := httptest.NewRecorder()
	s.handleDemoLastJob(rec, httptest.NewRequest("GET", "/v1/demo/last-job", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"job":null`) {
		t.Fatalf("empty last-job = %d %s", rec.Code, rec.Body.String())
	}
}

func TestDeviceLabel(t *testing.T) {
	cases := []struct{ plat, arch, backend, want string }{
		{"android", "aarch64", "llama", "Android phone · llama"},
		{"darwin", "arm64", "metal", "Apple-silicon Mac · metal"},
		{"macos", "arm64", "llama-cpp", "Apple-silicon Mac · llama-cpp"},
		{"linux", "x86_64", "cuda", "Linux desktop (x86-64) · cuda"},
		{"windows", "amd64", "", "Windows PC"},
		{"plan9", "mips", "", "plan9 mips"},
	}
	for _, c := range cases {
		p := &registry.Provider{Capabilities: protocol.Capabilities{
			Platform: c.plat, Arch: c.arch, Backend: c.backend,
		}}
		if got := deviceLabel(p); got != c.want {
			t.Errorf("deviceLabel(%s/%s/%s) = %q, want %q", c.plat, c.arch, c.backend, got, c.want)
		}
	}
}

func TestClipRunes(t *testing.T) {
	if got := clipRunes("  hi there  ", 100); got != "hi there" {
		t.Fatalf("trim: %q", got)
	}
	if got := clipRunes("abcdef", 3); got != "abc" {
		t.Fatalf("cap: %q", got)
	}
	if got := clipRunes("héllo wörld", 4); got != "héll" { // multi-byte runes not split
		t.Fatalf("rune-safe cap: %q", got)
	}
}
