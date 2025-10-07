package adaptivetelemetryprocessor

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// loadTrackedEntities loads tracked entities from storage
func (p *processorImp) loadTrackedEntities() error {
	start := time.Now()
	p.logger.Info("Starting to load tracked entities from storage")

	if p.storage == nil || !p.persistenceEnabled {
		p.logger.Warn("Cannot load tracked entities: storage is nil or persistence is disabled")
		return nil
	}

	entities, err := p.storage.Load()
	if err != nil {
		p.logger.Error("Failed to load tracked entities from storage",
			zap.Error(err),
			zap.String("storage_type", fmt.Sprintf("%T", p.storage)))
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Count entities by type for better observability
	resourceTypes := make(map[string]int)
	var oldestEntity time.Time
	if !time.Time.IsZero(time.Now()) { // Initialize to maximum time
		oldestEntity = time.Now()
	}

	for _, entity := range entities {
		if resourceType, ok := entity.Attributes["type"]; ok {
			resourceTypes[resourceType]++
		} else {
			resourceTypes["host_component"]++
		}

		if entity.FirstSeen.Before(oldestEntity) && !time.Time.IsZero(entity.FirstSeen) {
			oldestEntity = entity.FirstSeen
		}
	}

	p.trackedEntities = entities
	duration := time.Since(start)

	p.logger.Info("Successfully loaded tracked entities from storage",
		zap.Int("count", len(entities)),
		zap.Duration("duration", duration),
		zap.Any("resource_types", resourceTypes),
		zap.Time("oldest_entity", oldestEntity))
	return nil
}

// persistTrackedEntities saves tracked entities to storage
func (p *processorImp) persistTrackedEntities() error {
	start := time.Now()

	if p.storage == nil || !p.persistenceEnabled {
		p.logger.Warn("Cannot persist tracked entities: storage is nil or persistence is disabled")
		return nil
	}

	p.mu.RLock()
	entitiesCount := len(p.trackedEntities)
	p.logger.Info("Starting to persist tracked entities",
		zap.Int("count", entitiesCount),
		zap.String("storage_type", fmt.Sprintf("%T", p.storage)))

	if err := p.storage.Save(p.trackedEntities); err != nil {
		p.mu.RUnlock()
		p.logger.Error("Failed to persist tracked entities",
			zap.Error(err),
			zap.Int("entity_count", entitiesCount),
			zap.Duration("attempt_duration", time.Since(start)))
		return err
	}
	p.mu.RUnlock()

	duration := time.Since(start)
	// Use Info level instead of Debug to ensure we see persistence activity
	p.logger.Info("Successfully persisted tracked entities to storage",
		zap.Int("count", entitiesCount),
		zap.Duration("duration", duration),
		zap.String("storage_path", p.config.StoragePath))
	return nil
}

// cleanupExpiredEntities removes entities that have exceeded their retention period
func (p *processorImp) cleanupExpiredEntities() {
	if p.config.RetentionMinutes <= 0 {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	exp := time.Now().Add(-time.Duration(p.config.RetentionMinutes) * time.Minute)
	removed := 0

	for id, te := range p.trackedEntities {
		if te.LastExceeded.Before(exp) {
			delete(p.trackedEntities, id)
			removed++
		}
	}

	if removed > 0 {
		p.logger.Debug("Removed expired entities",
			zap.Int("removed_count", removed),
			zap.Int("remaining_count", len(p.trackedEntities)))
	}
}
