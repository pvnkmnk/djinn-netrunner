package main

import (
	"log"
	"os"

	"github.com/pvnkmnk/netrunner/backend/internal/config"
	"github.com/pvnkmnk/netrunner/backend/internal/database"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func main() {
	pgURL := os.Getenv("DATABASE_URL")
	if pgURL == "" {
		log.Fatal("DATABASE_URL (Postgres) must be set")
	}

	sqlitePath := os.Getenv("SQLITE_PATH")
	if sqlitePath == "" {
		sqlitePath = "netrunner.db"
	}

	log.Printf("Migrating from Postgres to SQLite (%s)...", sqlitePath)

	// 1. Connect to Postgres
	pgDB, err := gorm.Open(postgres.Open(pgURL), &gorm.Config{})
	if err != nil {
		log.Fatalf("Failed to connect to Postgres: %v", err)
	}

	// 2. Connect to SQLite
	cfg := &config.Config{DatabaseURL: sqlitePath}
	sqliteDB, err := database.Connect(cfg)
	if err != nil {
		log.Fatalf("Failed to connect to SQLite: %v", err)
	}

	// 3. Migrate Schema
	err = database.Migrate(sqliteDB)
	if err != nil {
		log.Fatalf("Failed to migrate SQLite schema: %v", err)
	}

	// 4. Transfer Data
	transferData(pgDB, sqliteDB)

	log.Println("Migration complete!")
}

func transferData(src, dst *gorm.DB) {
	// List of tables to transfer
	tables := []string{
		"users", "sessions", "quality_profiles", "monitored_artists",
		"tracked_releases", "sources", "jobs", "job_logs", "jobitems",
		"acquisitions", "libraries", "tracks", "schedules",
	}

	for _, table := range tables {
		log.Printf("Transferring table: %s", table)
		
		var data []map[string]interface{}
		if err := src.Table(table).Find(&data).Error; err != nil {
			log.Printf("Error reading from %s: %v", table, err)
			continue
		}

		if len(data) == 0 {
			continue
		}

		// Insert into destination
		if err := dst.Table(table).Create(&data).Error; err != nil {
			log.Printf("Error writing to %s: %v", table, err)
		}
	}
}
