package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/capability"
)

// Public fleet statistics for the marketing site, the testers page and the status page.
// Aggregates only: no user, key, device or job identifiers. Cached for thirty seconds so
// a popular page cannot turn into a load on the hub.

type publicStats struct {
	DevicesOnline int            `json:"devices_online"`
	DevicesKnown  int            `json:"devices_known"`
	ByKind        map[string]int `json:"by_kind"`
	ACUOnline     float64        `json:"acu_online"`
	DecodeTPS     float64        `json:"decode_tps_online"`
	Jobs24h       int64          `json:"jobs_24h"`
	Jobs7d        int            `json:"jobs_7d"`
	Models        []string       `json:"models"`
	GeneratedAt   string         `json:"generated_at"`
}

type statsCache struct {
	mu   sync.Mutex
	at   time.Time
	body publicStats
}

var publicStatsCache statsCache

const publicStatsTTL = 30 * time.Second

func (s *Server) handlePublicStats(w http.ResponseWriter, r *http.Request) {
	publicStatsCache.mu.Lock()
	defer publicStatsCache.mu.Unlock()
	if time.Since(publicStatsCache.at) < publicStatsTTL {
		w.Header().Set("Cache-Control", "public, max-age=30")
		writeJSON(w, http.StatusOK, publicStatsCache.body)
		return
	}

	devs := s.deviceViews(r)
	out := publicStats{ByKind: map[string]int{"phone": 0, "pc": 0, "other": 0}}
	models := map[string]bool{}
	for _, d := range devs {
		out.DevicesOnline++
		out.ByKind[d.Kind]++
		out.ACUOnline += d.ACU
		if d.SustainedEndTPS > 0 {
			out.DecodeTPS += d.SustainedEndTPS
		} else {
			out.DecodeTPS += d.DecodeTPS
		}
		for _, m := range d.Models {
			models[m] = true
		}
	}
	out.ACUOnline = capability.Round2(out.ACUOnline)
	out.DecodeTPS = float64(int(out.DecodeTPS*10)) / 10
	out.DevicesKnown = len(s.nodeFactsByPK(r))
	if rows, err := s.store.UsageSince(r.Context(), time.Now().Add(-24*time.Hour)); err == nil {
		for _, u := range rows {
			out.Jobs24h += int64(u.Requests)
		}
	}
	if rows, err := s.store.EarningsSince(r.Context(), time.Now().Add(-7*24*time.Hour)); err == nil {
		for _, e := range rows {
			out.Jobs7d += e.Jobs
		}
	}
	for m := range models {
		out.Models = append(out.Models, m)
	}
	if out.Models == nil {
		out.Models = []string{}
	}
	out.GeneratedAt = time.Now().UTC().Format(time.RFC3339)

	publicStatsCache.at = time.Now()
	publicStatsCache.body = out
	w.Header().Set("Cache-Control", "public, max-age=30")
	writeJSON(w, http.StatusOK, out)
}
