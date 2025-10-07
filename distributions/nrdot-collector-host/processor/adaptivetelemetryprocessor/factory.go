package adaptivetelemetryprocessor

import (
	"context"
	"fmt"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

const (
	typeStr = "adaptivetelemetryprocessor"
	// Constants needed for default config
	factoryDefaultStoragePath        = "./adaptiveprocess.db"
	factoryDefaultCompositeThreshold = 1.5
)

// NewFactory creates the processor.Factory used by the Collector to construct this processor.
func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelBeta),
	)
}

// createDefaultConfig returns the default configuration for this processor.
func createDefaultConfig() component.Config {
	return &Config{
		MetricThresholds:        map[string]float64{},
		Weights:                 map[string]float64{},
		RetentionMinutes:        30,
		StoragePath:             factoryDefaultStoragePath,
		EnableDynamicThresholds: false,
		EnableMultiMetric:       false,
		DynamicSmoothingFactor:  0.2,
		MinThresholds:           map[string]float64{},
		MaxThresholds:           map[string]float64{},
		CompositeThreshold:      factoryDefaultCompositeThreshold,
		EnableAnomalyDetection:  false,
		AnomalyHistorySize:      10,
		AnomalyChangeThreshold:  200.0,
	}
}

// createMetricsProcessor constructs the processor for metrics pipelines.
func createMetricsProcessor(
	_ context.Context, // Fixed: marked as unused
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	pCfg, ok := cfg.(*Config) // Fixed: proper type assertion
	if !ok {
		return nil, fmt.Errorf("invalid config type: expected *Config, got %T", cfg)
	}

	proc, err := newProcessor(set.Logger, pCfg, nextConsumer)
	if err != nil {
		return nil, err
	}
	return proc, nil
}
