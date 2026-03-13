package database

import (
	"fmt"
	"testing"
	"time"
)

func TestCalculateBackoff(t *testing.T) {
	tests := []struct {
		retryCount int
		expected   time.Duration
	}{
		{retryCount: 0, expected: 1 * time.Minute},
		{retryCount: 1, expected: 5 * time.Minute},
		{retryCount: 2, expected: 1 * time.Hour},
		{retryCount: 3, expected: 24 * time.Hour},
		{retryCount: 4, expected: 24 * time.Hour}, // Max backoff
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("RetryCount_%d", tt.retryCount), func(t *testing.T) {
			if got := CalculateBackoff(tt.retryCount); got != tt.expected {
				t.Errorf("CalculateBackoff() = %v, want %v", got, tt.expected)
			}
		})
	}
}
