package agent

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
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

func TestConfigTools(t *testing.T) {
	// Setup in-memory DB
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	db.AutoMigrate(&database.Setting{})

	cfg := &config.Config{
		Port: "8080",
	}


	// Test ReadConfig
	settings, err := ReadConfig(db, cfg)
	assert.NoError(t, err)
	assert.Equal(t, "8080", settings["port"])

	// Test UpdateConfig
	err = UpdateConfig(db, "custom_setting", "custom_value")
	assert.NoError(t, err)

	// Verify update
	var setting database.Setting
	err = db.First(&setting, "key = ?", "custom_setting").Error
	assert.NoError(t, err)
	assert.Equal(t, "custom_value", setting.Value)
}
