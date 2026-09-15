package services

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A peer that accepts the transfer and then never starts sending is the failure
// mode that used to consume the entire download budget — and, because the worker
// runs a single job at a time, stalled every other queued job behind it.
// WaitForDownload must give up on such a peer quickly and say so distinctly, so
// the pipeline can try another candidate instead of failing the item.
func TestWaitForDownload_RemoteQueueStallAbandonsEarly(t *testing.T) {
	originalGrace, originalInterval := remoteQueueGrace, downloadPollInterval
	remoteQueueGrace, downloadPollInterval = 30*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		remoteQueueGrace, downloadPollInterval = originalGrace, originalInterval
	})

	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// The peer queued us remotely: a real slskd state, no bytes, no progress.
		_, _ = w.Write([]byte(`{"id":"11111111-2222-3333-4444-555555555555","username":"peer","filename":"music\\Artist\\Album\\Track.flac","size":30000000,"state":"Queued, Remotely","bytesTransferred":0,"percentComplete":0}`))
	})))
	defer server.Close()

	svc := NewSlskdServiceWithClient(&config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}, nil, testSlskdClient())
	start := time.Now()

	download, err := svc.WaitForDownload(context.Background(), "peer", "11111111-2222-3333-4444-555555555555", DownloadWaitOptions{Timeout: 10 * time.Minute, HasAlternatives: true})

	require.Error(t, err, "a peer that never sends must not be waited on indefinitely")
	assert.Nil(t, download)
	assert.ErrorIs(t, err, ErrRemoteQueueStalled, "the pipeline keys off this sentinel to retry another candidate")
	assert.Less(t, time.Since(start), 5*time.Second,
		"must abandon on the grace period, not the full download timeout")
	assert.Contains(t, err.Error(), "Queued, Remotely", "the error should name the state that caused it")
}

// When no other candidate is available there is nothing to gain by abandoning a
// queued peer: the item fails either way, while a long peer queue may still
// deliver. The wait must therefore run to its timeout rather than tripping the
// early-abandon path.
func TestWaitForDownload_LastCandidateWaitsOutRemoteQueue(t *testing.T) {
	originalGrace, originalInterval := remoteQueueGrace, downloadPollInterval
	remoteQueueGrace, downloadPollInterval = 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		remoteQueueGrace, downloadPollInterval = originalGrace, originalInterval
	})

	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"id-1","username":"peer","filename":"music\\Album\\Track.flac","size":30000000,"state":"Queued, Remotely","bytesTransferred":0,"percentComplete":0}`))
	})))
	defer server.Close()

	svc := NewSlskdServiceWithClient(&config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}, nil, testSlskdClient())

	_, err := svc.WaitForDownload(context.Background(), "peer", "id-1", DownloadWaitOptions{Timeout: 200 * time.Millisecond})

	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRemoteQueueStalled,
		"with no alternative candidate the wait must run its course instead of abandoning early")
	assert.Contains(t, err.Error(), "download timeout")
}

// A transfer that does start sending must not be mistaken for a stalled one:
// slow-but-progressing downloads are legitimate.
func TestWaitForDownload_InProgressIsNotTreatedAsRemoteQueueStall(t *testing.T) {
	originalGrace, originalInterval := remoteQueueGrace, downloadPollInterval
	remoteQueueGrace, downloadPollInterval = 10*time.Millisecond, 10*time.Millisecond
	t.Cleanup(func() {
		remoteQueueGrace, downloadPollInterval = originalGrace, originalInterval
	})

	var polls int
	server := httptest.NewServer(withAPIKeyCheck(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		w.Header().Set("Content-Type", "application/json")
		if polls == 1 {
			_, _ = w.Write([]byte(`{"id":"id-1","state":"InProgress","bytesTransferred":1000,"percentComplete":1}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"id-1","state":"Completed, Succeeded","bytesTransferred":30000000,"percentComplete":100}`))
	})))
	defer server.Close()

	svc := NewSlskdServiceWithClient(&config.Config{SlskdURL: server.URL, SlskdAPIKey: testAPIKey}, nil, testSlskdClient())

	download, err := svc.WaitForDownload(context.Background(), "peer", "id-1", DownloadWaitOptions{Timeout: time.Minute, HasAlternatives: true})

	require.NoError(t, err, "a progressing transfer must be allowed to finish")
	require.NotNil(t, download)
	assert.True(t, download.State.IsSucceeded())
	assert.False(t, errors.Is(err, ErrRemoteQueueStalled))
}
