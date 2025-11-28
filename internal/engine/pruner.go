package engine

import (
	"sort"

	"rizznet/internal/config"
	"rizznet/internal/environment"
	"rizznet/internal/logger"
	"rizznet/internal/model"

	"gorm.io/gorm"
)

// PruneDatabase checks if the DB size exceeds the target limit and removes the lowest-scoring proxies.
// If customLimit is 0, it uses cfg.Database.MaxProxies.
func PruneDatabase(db *gorm.DB, cfg *config.Config, customLimit int) error {
	limit := customLimit
	if limit <= 0 {
		limit = cfg.Database.MaxProxies
	}
	if limit <= 0 {
		limit = 10000 // Default safe fallback
	}

	var count int64
	db.Model(&model.Proxy{}).Count(&count)

	if count <= int64(limit) {
		return nil // No pruning needed
	}

	excess := int(count) - limit
	logger.Log.Infof("✂️  Pruning Database: Count %d > Limit %d. Removing %d proxies...", count, limit, excess)

	// 1. Detect Environment (Needed for scoring)
	env, err := environment.Detect(cfg.Tester)
	if err != nil {
		logger.Log.Warn("Env detection failed during prune, assuming default.")
		env = &environment.Env{ISP: "Unknown"}
	}

	hist := NewHistoryEngine(db)

	// 2. Fetch ALL proxies
	var allProxies []model.Proxy
	db.Find(&allProxies)

	// 3. Score Everyone
	type scoredProxy struct {
		ID    uint
		Score float64
	}

	scored := make([]scoredProxy, len(allProxies))
	for i, p := range allProxies {
		score := CalculateGlobalPriority(p, hist, env.ISP, cfg.Categories)
		scored[i] = scoredProxy{ID: p.ID, Score: score}
	}

	// 4. Sort Ascending (Lowest score first)
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score < scored[j].Score
	})

	// 5. Delete Bottom N
	toDelete := make([]uint, 0, excess)
	for i := 0; i < excess; i++ {
		toDelete = append(toDelete, scored[i].ID)
	}

	tx := db.Begin()
	if err := tx.Delete(&model.Proxy{}, toDelete).Error; err != nil {
		tx.Rollback()
		return err
	}
	
	// Clean up orphaned Performance records
	tx.Where("proxy_id IN ?", toDelete).Delete(&model.ProxyPerformance{})
	
	return tx.Commit().Error
}
