package adaptivetelemetryprocessor

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.uber.org/zap"
)

// newProcessor constructs the processor with configured features and storage.
func newProcessor(logger *zap.Logger, config *Config, nextConsumer consumer.Metrics) (*processorImp, error) {
	// Normalize & validate config first
	config.Normalize()
	if err := config.Validate(); err != nil {
		return nil, err
	}

	// Check if storage is enabled (defaults to true if not specified)
	storageEnabled := true
	if config.EnableStorage != nil {
		storageEnabled = *config.EnableStorage
	}

	logger.Info("Initializing adaptivetelemetryprocessor",
		zap.Int("metric_thresholds_count", len(config.MetricThresholds)),
		zap.Bool("dynamic_thresholds_enabled", config.EnableDynamicThresholds),
		zap.Bool("multi_metric_enabled", config.EnableMultiMetric),
		zap.Bool("anomaly_detection_enabled", config.EnableAnomalyDetection),
		zap.Bool("storage_enabled", storageEnabled),
		zap.Int64("retention_minutes", config.RetentionMinutes),
		zap.Float64("composite_threshold", config.CompositeThreshold),
		zap.Float64("anomaly_change_threshold", config.AnomalyChangeThreshold),
		zap.Bool("storage_enabled", storageEnabled),
		zap.Bool("persistence_enabled", storageEnabled && config.StoragePath != ""),
		zap.String("storage_path", config.StoragePath),
		zap.Int64("retention_minutes", config.RetentionMinutes))

	logger.Info("Resource agnostic processing enabled",
		zap.String("supported_resources", "cpu, disk, filesystem, load, memory, network, process, processes, paging"))

	p := &processorImp{
		logger:                   logger,
		config:                   config,
		nextConsumer:             nextConsumer,
		trackedEntities:          make(map[string]*TrackedEntity),
		persistenceEnabled:       storageEnabled && config.StoragePath != "",
		dynamicThresholdsEnabled: config.EnableDynamicThresholds,
		multiMetricEnabled:       config.EnableMultiMetric,
		lastThresholdUpdate:      time.Now(),
		dynamicCustomThresholds:  make(map[string]float64),
	}

	// Seed dynamic thresholds with configured static thresholds
	for m, base := range config.MetricThresholds {
		if base > 0 {
			p.dynamicCustomThresholds[m] = base
			logger.Debug("Seeded dynamic threshold", zap.String("metric", m), zap.Float64("initial_threshold", base))
		}
	}

	if p.dynamicThresholdsEnabled {
		logger.Info("Dynamic thresholds enabled", zap.Float64("smoothing_factor", config.DynamicSmoothingFactor), zap.Int("metrics_tracked", len(p.dynamicCustomThresholds)))
	}
	if p.multiMetricEnabled {
		logger.Info("Multi-metric evaluation enabled", zap.Float64("composite_threshold", config.CompositeThreshold), zap.Int("weights_count", len(config.Weights)))
	}
	if config.EnableAnomalyDetection {
		logger.Info("Anomaly detection enabled", zap.Int("history_size", config.AnomalyHistorySize), zap.Float64("change_threshold", config.AnomalyChangeThreshold))
	}

	if p.persistenceEnabled {
		logger.Debug("Setting up persistent storage", zap.String("path", config.StoragePath))
		storageDir := filepath.Dir(config.StoragePath)
		if err := createDirectoryIfNotExists(storageDir); err != nil {
			logger.Warn("Failed to create storage directory", zap.String("path", storageDir), zap.Error(err))
			p.persistenceEnabled = false
		} else if storage, err := NewFileStorage(config.StoragePath); err != nil {
			logger.Warn("Failed to initialize storage", zap.Error(err))
			p.persistenceEnabled = false
		} else {
			p.storage = storage
			if err := p.loadTrackedEntities(); err != nil {
				logger.Warn("Failed to load tracked entities", zap.Error(err))
			}
		}
	}

	// Log processor configuration
	configSummary := map[string]interface{}{
		"dynamic_thresholds_enabled": p.dynamicThresholdsEnabled,
		"multi_metric_enabled":       p.multiMetricEnabled,
		"anomaly_detection_enabled":  config.EnableAnomalyDetection,
		"persistence_enabled":        p.persistenceEnabled,
		"retention_minutes":          float64(config.RetentionMinutes), // Convert int64 to float64 for consistent display
		"metric_thresholds_count":    len(config.MetricThresholds),
		"min_thresholds_count":       len(config.MinThresholds),
		"max_thresholds_count":       len(config.MaxThresholds),
		"weights_count":              len(config.Weights),
		"dynamic_smoothing_factor":   config.DynamicSmoothingFactor,
		"composite_threshold":        config.CompositeThreshold,
		"anomaly_history_size":       config.AnomalyHistorySize,
		"anomaly_change_threshold":   config.AnomalyChangeThreshold,
	}

	logger.Info("adaptivetelemetryprocessor initialized successfully",
		zap.Any("config_summary", configSummary))

	// Log top threshold values for visibility
	if len(config.MetricThresholds) > 0 {
		thresholds := make([]string, 0, 10)
		count := 0
		for metric, threshold := range config.MetricThresholds {
			if count < 10 {
				thresholds = append(thresholds, fmt.Sprintf("%s:%.4f", metric, threshold))
				count++
			} else {
				break
			}
		}
		logger.Info("Sample metric thresholds configured", zap.Strings("thresholds", thresholds),
			zap.Int("showing", len(thresholds)), zap.Int("total", len(config.MetricThresholds)))
	}

	return p, nil
}

// Shutdown cleans up processor resources
func (p *processorImp) Shutdown(ctx context.Context) error {
	if p.persistenceEnabled && p.storage != nil {
		if err := p.persistTrackedEntities(); err != nil {
			p.logger.Warn("Failed to persist tracked entities during shutdown", zap.Error(err))
		}
		if err := p.storage.Close(); err != nil {
			p.logger.Warn("Failed to close storage during shutdown", zap.Error(err))
		}
	}
	return nil
}

// Start is a no-op for this processor
func (p *processorImp) Start(_ context.Context, _ component.Host) error {
	return nil
}

// Capabilities indicates that this processor mutates data
func (p *processorImp) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}
