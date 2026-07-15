package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsPostgres(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected bool
	}{
		{"postgres prefix", "postgres://user:pass@localhost:5432/db", true},
		{"postgresql prefix", "postgresql://user:pass@localhost:5432/db", true},
		{"sqlite memory", ":memory:", false},
		{"sqlite file", "netrunner.db", false},
		{"sqlite path", "/path/to/database.db", false},
		{"empty string", "", false},
		{"random string", "not-a-database-url", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsPostgres(tt.url)
			assert.Equal(t, tt.expected, got)
		})
	}
}
