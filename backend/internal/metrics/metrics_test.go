package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestTrackExternalCall_Success(t *testing.T) {
	const svc = "test-svc-success"
	before := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svc, "ok"))
	start := time.Now()
	TrackExternalCall(svc, start, nil)
	after := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svc, "ok"))
	if after != before+1 {
		t.Fatalf("expected counter to increment from %v to %v, got %v", before, before+1, after)
	}
}

func TestTrackExternalCall_Error(t *testing.T) {
	const svc = "test-svc-error"
	before := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svc, "error"))
	TrackExternalCall(svc, time.Now(), fmt.Errorf("timeout"))
	after := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svc, "error"))
	if after != before+1 {
		t.Fatalf("expected error counter to increment from %v to %v, got %v", before, before+1, after)
	}
}

func TestTrackExternalCall_DifferentServices(t *testing.T) {
	const svcA = "svc-a"
	const svcB = "svc-b"
	beforeA := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svcA, "ok"))
	beforeB := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svcB, "error"))
	TrackExternalCall(svcA, time.Now(), nil)
	TrackExternalCall(svcB, time.Now(), fmt.Errorf("fail"))
	afterA := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svcA, "ok"))
	afterB := testutil.ToFloat64(ExternalAPICallsTotal.WithLabelValues(svcB, "error"))
	if afterA != beforeA+1 {
		t.Fatalf("svc-a ok counter: expected %v, got %v", beforeA+1, afterA)
	}
	if afterB != beforeB+1 {
		t.Fatalf("svc-b error counter: expected %v, got %v", beforeB+1, afterB)
	}
}

func TestMetricsRegistered(t *testing.T) {
	// Trigger lazy registration for all metrics by using them
	JobsQueued.Inc()
	JobsQueued.Dec()
	JobsRunning.Inc()
	JobsRunning.Dec()
	JobsProcessedTotal.WithLabelValues("test", "ok").Inc()
	JobDurationSeconds.WithLabelValues("test").Observe(0.1)
	ItemsProcessedTotal.WithLabelValues("ok").Inc()
	ZombieJobsRecovered.Inc()
	ExternalAPICallsTotal.WithLabelValues("test", "ok").Inc()
	ExternalAPIDurationSeconds.WithLabelValues("test").Observe(0.1)
	AcquisitionDedupTotal.WithLabelValues("hash").Inc()
	CoverArtFetchTotal.WithLabelValues("test", "ok").Inc()

	gathered, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("DefaultGatherer.Gather() error: %v", err)
	}

	expectedPrefixes := []string{
		"netrunner_worker_jobs_queued",
		"netrunner_worker_jobs_running",
		"netrunner_worker_jobs_processed_total",
		"netrunner_worker_job_duration_seconds",
		"netrunner_worker_items_processed_total",
		"netrunner_worker_zombie_jobs_recovered_total",
		"netrunner_external_api_calls_total",
		"netrunner_external_api_duration_seconds",
		"netrunner_acquisition_dedup_total",
		"netrunner_acquisition_cover_art_fetch_total",
	}

	found := make(map[string]bool)
	for _, mf := range gathered {
		found[mf.GetName()] = true
	}

	for _, prefix := range expectedPrefixes {
		if !found[prefix] {
			t.Errorf("expected metric %q not found in default registry", prefix)
		}
	}

	// Print all gathered metric names for debugging
	var names []string
	for _, mf := range gathered {
		names = append(names, mf.GetName())
	}
	t.Logf("registered metrics: %s", strings.Join(names, ", "))
}
