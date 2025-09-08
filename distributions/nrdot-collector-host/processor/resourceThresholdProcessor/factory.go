package resourcethresholdprocessor

import (
	"context"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
)

const (
	// Type string for this processor as used in configuration
	typeStr = "resource_threshold"
)

func NewFactory() processor.Factory {
	return processor.NewFactory(
		component.MustNewType(typeStr),
		createDefaultConfig,
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelBeta),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		MetricThresholds: map[string]float64{
			"process.cpu.utilization":    defaultCPUThresholdPercent,
			"process.memory.utilization": defaultMemoryThresholdPercent,
		},
		RetentionMinutes:        defaultRetentionMinutes,
		StoragePath:             defaultStoragePath,
		EnableDynamicThresholds: false, // default off
	}
}

func createMetricsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (processor.Metrics, error) {
	pCfg := cfg.(*Config)
	proc, err := newProcessor(set.TelemetrySettings.Logger, pCfg, nextConsumer)
	if err != nil {
		return nil, err
	}

	return proc, nil
}
