// Package metrics holds the Prometheus collectors for the coordinator. No label value
// ever contains prompt or completion content.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	HTTPRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sc_http_requests_total",
		Help: "HTTP requests by route and status class.",
	}, []string{"route", "status"})

	HTTPDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "sc_http_request_duration_seconds",
		Help:    "HTTP request duration by route.",
		Buckets: prometheus.DefBuckets,
	}, []string{"route"})

	JobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sc_jobs_total",
		Help: "Inference jobs by result (ok, no_provider, tier_unmet, provider_gone, decrypt, error).",
	}, []string{"result"})

	JobTTFT = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "sc_job_ttft_seconds",
		Help:    "Time from job dispatch to first token chunk.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})

	TokensTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "sc_tokens_total",
		Help: "Tokens metered, by direction (prompt, completion).",
	}, []string{"direction"})

	ProvidersConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "sc_providers_connected",
		Help: "Currently connected providers.",
	})
)
