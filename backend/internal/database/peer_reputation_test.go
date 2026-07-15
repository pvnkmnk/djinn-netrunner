package database

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestPeerReputation_SuccessRate(t *testing.T) {
	tests := []struct {
		name            string
		totalDownloads  int
		successfulDls   int
		failedDls       int
		expectedRate    float64
	}{
		{
			name:           "Zero downloads returns 1.0",
			totalDownloads: 0,
			successfulDls:  0,
			failedDls:      0,
			expectedRate:   1.0,
		},
		{
			name:           "All successful",
			totalDownloads: 10,
			successfulDls:  10,
			failedDls:      0,
			expectedRate:   1.0,
		},
		{
			name:           "All failed",
			totalDownloads: 10,
			successfulDls:  0,
			failedDls:      10,
			expectedRate:   0.0,
		},
		{
			name:           "Mixed results - 80% success",
			totalDownloads: 10,
			successfulDls:  8,
			failedDls:      2,
			expectedRate:   0.8,
		},
		{
			name:           "Mixed results - 50% success",
			totalDownloads: 100,
			successfulDls:  50,
			failedDls:      50,
			expectedRate:   0.5,
		},
		{
			name:           "One successful from many",
			totalDownloads: 100,
			successfulDls:  1,
			failedDls:      99,
			expectedRate:   0.01,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PeerReputation{
				TotalDownloads: tt.totalDownloads,
				SuccessfulDls:  tt.successfulDls,
				FailedDls:      tt.failedDls,
			}
			got := p.SuccessRate()
			assert.InDelta(t, tt.expectedRate, got, 0.001)
		})
	}
}

func TestPeerReputation_IsIgnored(t *testing.T) {
	tests := []struct {
		name           string
		totalDownloads int
		successfulDls  int
		failedDls      int
		expected       bool
	}{
		{
			name:           "Few downloads - not ignored",
			totalDownloads: 4,
			successfulDls:  0,
			failedDls:      4,
			expected:       false, // totalDownloads < 5
		},
		{
			name:           "Exactly 5 downloads, 100% failed - ignored",
			totalDownloads: 5,
			successfulDls:  0,
			failedDls:      5,
			expected:       true, // totalDownloads >= 5 && SuccessRate < 0.2
		},
		{
			name:           "5 downloads, 20% success - not ignored",
			totalDownloads: 5,
			successfulDls:  1,
			failedDls:      4,
			expected:       false, // SuccessRate = 0.2, not < 0.2
		},
		{
			name:           "5 downloads, 0% success - ignored",
			totalDownloads: 5,
			successfulDls:  0,
			failedDls:      5,
			expected:       true,
		},
		{
			name:           "Many downloads with good success rate - not ignored",
			totalDownloads: 100,
			successfulDls:  90,
			failedDls:      10,
			expected:       false,
		},
		{
			name:           "Many downloads with poor success rate - ignored",
			totalDownloads: 100,
			successfulDls:  10,
			failedDls:      90,
			expected:       true,
		},
		{
			name:           "Zero downloads - not ignored",
			totalDownloads: 0,
			successfulDls:  0,
			failedDls:      0,
			expected:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &PeerReputation{
				TotalDownloads: tt.totalDownloads,
				SuccessfulDls:  tt.successfulDls,
				FailedDls:      tt.failedDls,
			}
			got := p.IsIgnored()
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestPeerReputation_UpdatedAt(t *testing.T) {
	p := &PeerReputation{
		Username:      "testuser",
		TotalDownloads: 10,
		SuccessfulDls: 8,
		FailedDls:     2,
		AvgSpeed:      1024,
		LastSeen:      time.Now(),
	}
	// Verify fields are accessible
	assert.NotEmpty(t, p.Username)
	assert.Equal(t, 10, p.TotalDownloads)
}
