package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/protocol"
)

func TestLimitBodyRejectsOversizeByContentLength(t *testing.T) {
	h := limitBody(1024, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	// declared Content-Length over the cap -> 413 before the body is read
	r := httptest.NewRequest("POST", "/x", strings.NewReader(strings.Repeat("a", 5000)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("want 413, got %d", rec.Code)
	}
}

func TestLimitBodyCapsStreamedBody(t *testing.T) {
	var readErr error
	h := limitBody(1024, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, readErr = io.Copy(io.Discard, r.Body)
		w.WriteHeader(200)
	}))
	// no Content-Length (chunked); MaxBytesReader must trip while reading
	pr, pw := io.Pipe()
	go func() { _, _ = pw.Write([]byte(strings.Repeat("a", 4096))); pw.Close() }()
	r := httptest.NewRequest("POST", "/x", pr)
	r.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), r)
	if readErr == nil {
		t.Fatal("expected MaxBytesReader to error on an oversize streamed body")
	}
}

func TestLimitBodyLeavesGetAlone(t *testing.T) {
	called := false
	h := limitBody(8, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	r := httptest.NewRequest("GET", "/x", nil)
	r.ContentLength = 1 << 30 // absurd, but GET has no body to guard
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !called {
		t.Fatal("GET should pass through untouched")
	}
}

func TestMessagesTooBig(t *testing.T) {
	small := []protocol.ChatMessage{{Role: "user", Content: strings.Repeat("x", 1000)}}
	if messagesTooBig(small) {
		t.Fatal("1 KB message should be fine")
	}
	big := []protocol.ChatMessage{
		{Role: "user", Content: strings.Repeat("x", maxPromptChars/2)},
		{Role: "user", Content: strings.Repeat("y", maxPromptChars/2+1)},
	}
	if !messagesTooBig(big) {
		t.Fatal("combined content over the cap must be rejected")
	}
}
