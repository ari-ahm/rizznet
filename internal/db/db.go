package db

import (
	"fmt"
	"rizznet/internal/model"
	"rizznet/internal/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func Connect(path string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlog.Default.LogMode(gormlog.Error),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// --- PERFORMANCE TUNING ---
	// Enable Write-Ahead Logging for concurrency
	db.Exec("PRAGMA journal_mode=WAL;")
	// Synchronous NORMAL is faster and safe enough for WAL
	db.Exec("PRAGMA synchronous=NORMAL;")
	// Store temp tables in memory
	db.Exec("PRAGMA temp_store=MEMORY;")
    // Increase cache size (approx 64MB)
    db.Exec("PRAGMA cache_size=-64000;")

	return db, nil
}

func Migrate(db *gorm.DB) error {
	return db.AutoMigrate(&model.Proxy{}, &model.Category{}, &model.ProxyPerformance{})
}

func Close(db *gorm.DB) {
	sqlDB, err := db.DB()
	if err != nil {
		logger.Log.Errorf("Failed to get underlying SQL DB for closing: %v", err)
		return
	}
	if err := sqlDB.Close(); err != nil {
		logger.Log.Errorf("Error closing database: %v", err)
	}
}