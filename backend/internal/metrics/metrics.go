package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Job metrics
var (
	JobsQueued = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "jobs_queued",
		Help:      "Number of jobs currently in queued state.",
	})

	JobsRunning = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "jobs_running",
		Help:      "Number of jobs currently in running state.",
	})

	JobsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "jobs_processed_total",
		Help:      "Total number of jobs processed, labeled by type and outcome.",
	}, []string{"type", "outcome"})

	JobDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "job_duration_seconds",
		Help:      "Time spent processing a job from claim to completion.",
		Buckets:   prometheus.ExponentialBuckets(0.5, 2, 12), // 0.5s to ~34min
	}, []string{"type"})

	ItemsProcessedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "items_processed_total",
		Help:      "Total number of job items processed, labeled by outcome.",
	}, []string{"outcome"})

	ZombieJobsRecovered = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "worker",
		Name:      "zombie_jobs_recovered_total",
		Help:      "Total number of zombie jobs detected and reset to queued.",
	})
)

// External API call metrics
var (
	ExternalAPICallsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "external",
		Name:      "api_calls_total",
		Help:      "Total external API calls, labeled by service and status.",
	}, []string{"service", "status"})

	ExternalAPIDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "netrunner",
		Subsystem: "external",
		Name:      "api_duration_seconds",
		Help:      "Duration of external API calls in seconds.",
		Buckets:   prometheus.ExponentialBuckets(0.05, 2, 10), // 50ms to ~25s
	}, []string{"service"})
)

// Acquisition pipeline metrics
var (
	AcquisitionDedupTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "acquisition",
		Name:      "dedup_total",
		Help:      "Total deduplications detected, labeled by method (hash, recording_id).",
	}, []string{"method"})

	CoverArtFetchTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "netrunner",
		Subsystem: "acquisition",
		Name:      "cover_art_fetch_total",
		Help:      "Total cover art fetch attempts, labeled by source and outcome.",
	}, []string{"source", "outcome"})
)
