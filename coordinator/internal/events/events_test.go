package events

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestRingWrapsAndOrders(t *testing.T) {
	r := NewRing(16)
	for i := 0; i < 40; i++ {
		r.Push("INFO", "m", map[string]string{"i": strings.Repeat("x", i)})
	}
	if r.Len() != 16 {
		t.Fatalf("len = %d, want 16", r.Len())
	}
	got := r.Recent(5, "", "", 0)
	if len(got) != 5 || got[0].Seq != 40 || got[4].Seq != 36 {
		t.Fatalf("newest-first expected, got seqs %d..%d", got[0].Seq, got[len(got)-1].Seq)
	}
	if inc := r.Recent(100, "", "", 38); len(inc) != 2 {
		t.Fatalf("since_seq filter: got %d, want 2", len(inc))
	}
}

func TestFiltersAndCounts(t *testing.T) {
	r := NewRing(64)
	r.Push("INFO", "provider registered", map[string]string{"platform": "android"})
	r.Push("WARN", "attestation did not clear", nil)
	r.Push("ERROR", "stripe transfer failed", map[string]string{"account": "acct_1"})
	r.Push("AUDIT", "admin drain", map[string]string{"providers": "2"})

	if got := r.Recent(10, "error", "", 0); len(got) != 1 || got[0].Msg != "stripe transfer failed" {
		t.Fatalf("level filter: %+v", got)
	}
	if got := r.Recent(10, "", "ANDROID", 0); len(got) != 1 {
		t.Fatalf("query should match attrs case-insensitively: %+v", got)
	}
	w, e, last := r.Counts(time.Minute)
	if w != 1 || e != 1 || last == nil || last.Attrs["account"] != "acct_1" {
		t.Fatalf("counts = %d warn, %d err, last=%v", w, e, last)
	}
}

func TestHandlerTeesAndDropsContent(t *testing.T) {
	var out bytes.Buffer
	ring := NewRing(32)
	base := slog.NewTextHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})
	log := slog.New(NewHandler(base, ring, slog.LevelInfo)).With("svc", "coordinator")

	log.Debug("noise")
	log.Info("provider registered", "platform", "android", "prompt", "SECRET TEXT")
	log.Error("boom", slog.Group("job", "id", "j1"))

	if ring.Len() != 2 {
		t.Fatalf("ring should hold info+error only, has %d", ring.Len())
	}
	ev := ring.Recent(10, "info", "", 0)[0]
	if ev.Attrs["svc"] != "coordinator" || ev.Attrs["platform"] != "android" {
		t.Fatalf("attrs not carried: %+v", ev.Attrs)
	}
	if _, leaked := ev.Attrs["prompt"]; leaked {
		t.Fatal("content key must not reach the ring")
	}
	if g := ring.Recent(10, "error", "", 0)[0].Attrs["job.id"]; g != "j1" {
		t.Fatalf("group attr flattening: %q", g)
	}
	if !strings.Contains(out.String(), "provider registered") || !strings.Contains(out.String(), "noise") {
		t.Fatal("wrapped handler must still receive every record")
	}
}
