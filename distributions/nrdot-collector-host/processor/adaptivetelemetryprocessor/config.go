package adaptivetelemetryprocessor

import (
	"fmt"
)

// Config is populated from the Collector YAML under:
// processors:
//   adaptivetelemetryprocessor:
//     metric_thresholds:
//       process.cpu.utilization: 5.0
//       process.memory.utilization: 10.0
//       custom.requests.per_second: 100
//       app.latency.ms: 50
//
//     # Optional per-metric weights for composite scoring (multi-metric)
//     enable_multi_metric: true
//     weights:
//       process.cpu.utilization: 1.0
//       process.memory.utilization: 0.8
//       custom.requests.per_second: 0.3
//     composite_threshold: 1.2
//
//     # Dynamic thresholds (optional)
//     enable_dynamic_thresholds: true
//     dynamic_smoothing_factor: 0.2
//     min_thresholds:
//       process.cpu.utilization: 1.0
//     max_thresholds:
//       process.cpu.utilization: 20.0
//
//     # Anomaly detection (optional)
//     enable_anomaly_detection: true
//     anomaly_history_size: 10            # will be capped if > max allowed
//     anomaly_change_threshold: 200.0     # percentage spike over rolling avg
//
//     # Retention & persistence
//     retention_minutes: 30               # how long since last exceed to keep entity (capped)
//     storage_path: /var/lib/nrdot-collector/adaptiveprocess.db
//
// Example pipeline wiring:
// service:
//   pipelines:
//     metrics:
//       processors: [adaptivetelemetryprocessor]
//       receivers: [...]
//       exporters: [...]

// Config defines processor configuration.
// All numeric fields are validated & capped during processor construction so startup does not fail
// for mildly invalid values; only clearly invalid (e.g. negative thresholds) return an error.
type Config struct {
	// Generic thresholds per metric (required for those metrics to participate in evaluation)
	MetricThresholds map[string]float64 `mapstructure:"metric_thresholds"`

	// Common settings
	RetentionMinutes int64  `mapstructure:"retention_minutes"`
	StoragePath      string `mapstructure:"storage_path"`
	EnableStorage    *bool  `mapstructure:"enable_storage"`

	// Dynamic thresholds
	EnableDynamicThresholds bool               `mapstructure:"enable_dynamic_thresholds"`
	DynamicSmoothingFactor  float64            `mapstructure:"dynamic_smoothing_factor"`
	MinThresholds           map[string]float64 `mapstructure:"min_thresholds"`
	MaxThresholds           map[string]float64 `mapstructure:"max_thresholds"`

	// Multi metric (composite) scoring
	EnableMultiMetric  bool               `mapstructure:"enable_multi_metric"`
	CompositeThreshold float64            `mapstructure:"composite_threshold"`
	Weights            map[string]float64 `mapstructure:"weights"`

	// Anomaly detection
	EnableAnomalyDetection bool    `mapstructure:"enable_anomaly_detection"`
	AnomalyHistorySize     int     `mapstructure:"anomaly_history_size"`
	AnomalyChangeThreshold float64 `mapstructure:"anomaly_change_threshold"`

	// Debug options
	DebugShowAllFilterStages bool `mapstructure:"debug_show_all_filter_stages"`
}

// Default / cap constants
const (
	defaultRetentionMinutes       int64   = 30
	maxRetentionMinutes           int64   = 30
	defaultDynamicSmoothingFactor float64 = 0.2
	maxAnomalyHistorySize         int     = 100
	defaultCompositeThreshold     float64 = 1.5
	defaultAnomalyHistorySize     int     = 10
	defaultAnomalyChangeThreshold float64 = 200.0
)

// Normalize applies defaults & caps. Must be called before processor usage. It does not log; caller should.
func (cfg *Config) Normalize() {
	if cfg.MetricThresholds == nil {
		cfg.MetricThresholds = map[string]float64{}
	}
	if cfg.MinThresholds == nil {
		cfg.MinThresholds = map[string]float64{}
	}
	if cfg.MaxThresholds == nil {
		cfg.MaxThresholds = map[string]float64{}
	}
	if cfg.Weights == nil {
		cfg.Weights = map[string]float64{}
	}

	if cfg.RetentionMinutes <= 0 {
		cfg.RetentionMinutes = defaultRetentionMinutes
	}
	if cfg.RetentionMinutes > maxRetentionMinutes {
		cfg.RetentionMinutes = maxRetentionMinutes
	}

	if cfg.EnableDynamicThresholds {
		if cfg.DynamicSmoothingFactor <= 0 || cfg.DynamicSmoothingFactor > 1 {
			cfg.DynamicSmoothingFactor = defaultDynamicSmoothingFactor
		}
	}

	if cfg.EnableMultiMetric {
		if cfg.CompositeThreshold <= 0 {
			cfg.CompositeThreshold = defaultCompositeThreshold
		}
	}

	if cfg.EnableAnomalyDetection {
		if cfg.AnomalyHistorySize <= 0 {
			cfg.AnomalyHistorySize = defaultAnomalyHistorySize
		}
		if cfg.AnomalyHistorySize > maxAnomalyHistorySize {
			cfg.AnomalyHistorySize = maxAnomalyHistorySize
		}
		if cfg.AnomalyChangeThreshold <= 0 {
			cfg.AnomalyChangeThreshold = defaultAnomalyChangeThreshold
		}
	} else {
		// Provide sane baseline values if disabled
		if cfg.AnomalyHistorySize == 0 {
			cfg.AnomalyHistorySize = defaultAnomalyHistorySize
		}
		if cfg.AnomalyChangeThreshold == 0 {
			cfg.AnomalyChangeThreshold = defaultAnomalyChangeThreshold
		}
	}
}

// Validate performs strict validation after normalization for values that must not be negative.
func (cfg *Config) Validate() error {
	for metric, threshold := range cfg.MetricThresholds {
		if threshold < 0 {
			return fmt.Errorf("threshold for metric %q must be >= 0, got %v", metric, threshold)
		}
	}
	for metric, v := range cfg.MinThresholds {
		if v < 0 {
			return fmt.Errorf("min_thresholds[%s] must be >= 0, got %v", metric, v)
		}
	}
	for metric, v := range cfg.MaxThresholds {
		if v < 0 {
			return fmt.Errorf("max_thresholds[%s] must be >= 0, got %v", metric, v)
		}
	}
	if cfg.EnableAnomalyDetection {
		if cfg.AnomalyHistorySize <= 0 {
			return fmt.Errorf("anomaly_history_size must be > 0, got %d", cfg.AnomalyHistorySize)
		}
		if cfg.AnomalyChangeThreshold <= 0 {
			return fmt.Errorf("anomaly_change_threshold must be > 0, got %f", cfg.AnomalyChangeThreshold)
		}
	}
	if cfg.EnableMultiMetric && cfg.CompositeThreshold <= 0 {
		return fmt.Errorf("composite_threshold must be > 0, got %f", cfg.CompositeThreshold)
	}
	return nil
}
