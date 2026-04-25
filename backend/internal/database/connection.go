package database

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect establishes a connection to the database (PostgreSQL or SQLite)
func Connect(cfg *config.Config) (*gorm.DB, error) {
	var dialector gorm.Dialector

	if strings.HasPrefix(cfg.DatabaseURL, "postgres://") || strings.HasPrefix(cfg.DatabaseURL, "postgresql://") {
		slog.Info("Connecting to PostgreSQL...")
		dialector = postgres.Open(cfg.DatabaseURL)
	} else {
		// Default to SQLite
		dbPath := cfg.DatabaseURL
		if !strings.HasSuffix(dbPath, ".db") && !strings.Contains(dbPath, ":") {
			dbPath = "netrunner.db"
		}
		slog.Info("Connecting to SQLite", "path", dbPath)
		dialector = sqlite.Open(dbPath)
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get database instance: %w", err)
	}

	// Connection pooling configuration
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(100)
	sqlDB.SetConnMaxLifetime(time.Hour)

	// SQLite-specific optimizations
	if !strings.HasPrefix(cfg.DatabaseURL, "postgres") {
		db.Exec("PRAGMA journal_mode=WAL;")
		db.Exec("PRAGMA synchronous=NORMAL;")
		db.Exec("PRAGMA foreign_keys=ON;")
		db.Exec("PRAGMA busy_timeout=5000;")
	}

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}
