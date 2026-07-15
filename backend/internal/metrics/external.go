package metrics

import "time"

// TrackExternalCall records the duration and outcome of an external API call.
func TrackExternalCall(service string, start time.Time, err error) {
	duration := time.Since(start).Seconds()
	ExternalAPIDurationSeconds.WithLabelValues(service).Observe(duration)
	status := "ok"
	if err != nil {
		status = "error"
	}
	ExternalAPICallsTotal.WithLabelValues(service, status).Inc()
}
