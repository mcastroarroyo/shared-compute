// Package api serves the consumer-facing OpenAI-compatible REST surface and mounts the
// provider WebSocket handler.
package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mcastroarroyo/shared-compute/coordinator/internal/config"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/jobs"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/metrics"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/registry"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/relay"
	"github.com/mcastroarroyo/shared-compute/coordinator/internal/wshub"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Server struct {
	cfg config.Config
	reg *registry.Registry
	job *jobs.Manager
	log *slog.Logger
	hub *wshub.Hub
}

func NewServer(cfg config.Config, reg *registry.Registry, job *jobs.Manager, log *slog.Logger) *Server {
	return &Server{
		cfg: cfg, reg: reg, job: job, log: log,
		hub: &wshub.Hub{Cfg: cfg, Reg: reg, Job: job, Log: log},
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", instrument("healthz", http.HandlerFunc(s.handleHealth)))
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.Handle("GET /v1/models", instrument("v1_models", s.withAuth(s.handleModels)))
	mux.Handle("POST /v1/chat/completions", instrument("v1_chat_completions", s.withAuth(s.handleChatCompletions)))
	mux.HandleFunc("/ws/provider", s.hub.HandleProvider)
	return logRequests(s.log, mux)
}

// statusRecorder captures the response status for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// instrument records request count + duration for a route. The route label is a fixed
// string, never derived from the URL, so no user data enters metrics.
func instrument(route string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
		metrics.HTTPRequests.WithLabelValues(route, strconv.Itoa(rec.status/100)+"xx").Inc()
	})
}

func (s *Server) deps() relay.Deps {
	return relay.Deps{Reg: s.reg, Job: s.job, Cfg: s.cfg, Log: s.log}
}

// withAuth checks the consumer Bearer API key.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := bearer(r)
		if key == "" {
			writeError(w, http.StatusUnauthorized, "missing_api_key", "provide an API key via Authorization: Bearer")
			return
		}
		if _, ok := s.cfg.ConsumerAPIKeys[key]; !ok {
			writeError(w, http.StatusUnauthorized, "invalid_api_key", "the API key is not recognized")
			return
		}
		next(w, r)
	}
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}

func logRequests(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		// Method + path + remote only. Never the body.
		log.Debug("http", "method", r.Method, "path", r.URL.Path)
	})
}
