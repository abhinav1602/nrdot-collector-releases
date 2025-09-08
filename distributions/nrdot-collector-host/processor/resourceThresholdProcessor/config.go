package resourcethresholdprocessor

import (
	"fmt"
)

// Config is populated from the Collector YAML under:
// processors:
//   resource_threshold:
//     metric_thresholds:
//       process.cpu.utilization: 5.0
//       process.memory.utilization: 10.0
//     retention_minutes: ...
//     storage_path: ...
//     enable_dynamic_thresholds: ...

type Config struct {
	MetricThresholds        map[string]float64 `mapstructure:"metric_thresholds"`
	RetentionMinutes        int64              `mapstructure:"retention_minutes"`
	StoragePath             string             `mapstructure:"storage_path"`
	EnableDynamicThresholds bool               `mapstructure:"enable_dynamic_thresholds"`
}

func (cfg *Config) Validate() error {
	if len(cfg.MetricThresholds) == 0 {
		return fmt.Errorf("metric_thresholds must be configured")
	}

	for metric, threshold := range cfg.MetricThresholds {
		if threshold < 0 || threshold > 100 {
			return fmt.Errorf("threshold for metric %q must be between 0 and 100, got %v", metric, threshold)
		}
	}

	if cfg.RetentionMinutes <= 0 {
		return fmt.Errorf("retention_minutes must be greater than 0, got %v", cfg.RetentionMinutes)
	}
	return nil
}
