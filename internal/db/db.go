package db

import (
	"fmt"
	"rizznet/internal/model"
	"rizznet/internal/logger"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	gormlog "gorm.io/gorm/logger"
)

func Connect(path string) (*gorm.DB, error) {
	// 1. Open Gorm connection
	db, err := gorm.Open(sqlite.Open(path), &gorm.Config{
		Logger: gormlog.Default.LogMode(gormlog.Error),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 2. Configure Connection Pool (CRITICAL FOR SQLITE)
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get sql.DB: %w", err)
	}

	// Restrict to 1 connection to prevent SQLITE_BUSY errors during concurrent writes.
	// Since SQLite only supports one writer at a time, having multiple connections
	// just causes lock contention at the file level.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	sqlDB.SetConnMaxLifetime(-1) // Never close idle connections

	// --- PERFORMANCE TUNING ---
	// Enable Write-Ahead Logging for concurrency
	db.Exec("PRAGMA journal_mode=WAL;")
	
	// Synchronous NORMAL is faster and safe enough for WAL
	db.Exec("PRAGMA synchronous=NORMAL;")
	
	// Store temp tables in memory
	db.Exec("PRAGMA temp_store=MEMORY;")
	
	// Increase cache size (approx 64MB)
	db.Exec("PRAGMA cache_size=-64000;")
	
	// Increase busy timeout to 10 seconds (gives queued writes more time)
	db.Exec("PRAGMA busy_timeout=10000;")

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
