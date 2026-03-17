package api

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStatsHandler_Init(t *testing.T) {
	// Basic test to ensure handler can be created
	// Full integration tests would require DB setup
	assert.NotNil(t, &StatsHandler{})
}

func TestJobStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := JobStats{
		Total:       100,
		Queued:      10,
		Running:     5,
		Succeeded:   75,
		Failed:      10,
		SuccessRate: 88.23,
	}

	assert.Equal(t, int64(100), stats.Total)
	assert.Equal(t, int64(75), stats.Succeeded)
	assert.Equal(t, 88.23, stats.SuccessRate)
}

func TestLibraryStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := LibraryStats{
		TotalTracks: 1000,
		TotalSize:   5000000000,
		TotalSizeMB: 4768.37,
		FormatBreakdown: []FormatCount{
			{Format: "FLAC", Count: 800, TotalSize: 4000000000},
			{Format: "MP3", Count: 200, TotalSize: 1000000000},
		},
	}

	assert.Equal(t, int64(1000), stats.TotalTracks)
	assert.Len(t, stats.FormatBreakdown, 2)
	assert.Equal(t, "FLAC", stats.FormatBreakdown[0].Format)
}

func TestActivityStats_Structure(t *testing.T) {
	// Test that the struct has the expected fields
	stats := ActivityStats{
		MonitoredArtists: 50,
		Watchlists:       10,
		QualityProfiles:  5,
		Libraries:        3,
		RecentJobs24h:    25,
		RecentJobs7d:     100,
	}

	assert.Equal(t, int64(50), stats.MonitoredArtists)
	assert.Equal(t, int64(3), stats.Libraries)
}
