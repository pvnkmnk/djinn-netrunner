package agent

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestProbeSystem(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	cfg := &config.Config{
		GonicURL: "http://localhost:14747",
	}

	status, err := ProbeSystem(db, cfg)
	assert.NoError(t, err)
	assert.True(t, status.DatabaseConnected)
	// We expect Gonic to fail in this test environment
	assert.False(t, status.GonicConnected)
}
