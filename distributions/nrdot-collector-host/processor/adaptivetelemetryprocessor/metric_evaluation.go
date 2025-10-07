package adaptivetelemetryprocessor

import (
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// MetricEvaluator coordinates the evaluation of metrics against thresholds
// and acts as a facade for the specialized implementations
type MetricEvaluator struct {
	config            *Config
	logger            *zap.Logger
	processor         *processorImp      // Reference to the parent processor for accessing shared state
	dynamicThresholds map[string]float64 // Cache of dynamic thresholds
}

// NewMetricEvaluator creates a new metric evaluator
func NewMetricEvaluator(config *Config, logger *zap.Logger, processor *processorImp) *MetricEvaluator {
	return &MetricEvaluator{
		config:            config,
		logger:            logger,
		processor:         processor,
		dynamicThresholds: make(map[string]float64),
	}
}

// EvaluateResource delegates to the processor's implementation for resource evaluation
// This method simply serves as a facade that delegates to the processor
// Returns whether the resource should be included
func (me *MetricEvaluator) EvaluateResource(resourceMetrics pmetric.ResourceMetrics) bool {
	// Delegate to the processor's implementation for evaluating resources
	return me.processor.shouldIncludeResource(resourceMetrics.Resource(), resourceMetrics)
}

// extractMetricValues delegates to processor's implementation in composite_metrics.go
func (me *MetricEvaluator) extractMetricValues(rm pmetric.ResourceMetrics) map[string]float64 {
	// Delegate to the specialized implementation in processor
	return me.processor.extractMetricValues(rm)
}

// detectAnomaly delegates to the specialized implementation in anomaly_detection.go
func (me *MetricEvaluator) detectAnomaly(trackedEntity *TrackedEntity, currentValues map[string]float64) (bool, string) {
	return detectAnomalyUtil(me.processor, trackedEntity, currentValues)
}

// calculateCompositeScore delegates to the specialized implementation in composite_metrics.go
func (me *MetricEvaluator) calculateCompositeScore(values map[string]float64) (float64, string) {
	return me.processor.calculateCompositeGeneric(values)
}

// UpdateDynamicThresholds delegates to the specialized implementation in dynamic_thresholds.go
func (me *MetricEvaluator) UpdateDynamicThresholds(md pmetric.Metrics) {
	me.processor.updateDynamicThresholds(md)
}
