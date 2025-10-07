package adaptivetelemetryprocessor

import (
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// extractMetricValues returns numeric values for all supported metrics in the resource metrics.
// Supports only Gauge and Sum metric types as they contain direct numeric values suitable for threshold comparison.
func (p *processorImp) extractMetricValues(rm pmetric.ResourceMetrics) map[string]float64 {
	// Pre-allocate map with estimated capacity based on average metrics per resource
	// This reduces map resizing and improves performance
	estimatedMetricsPerResource := 10
	values := make(map[string]float64, estimatedMetricsPerResource)
	unsupportedSeen := false

	// Only collect values for metrics that we're interested in evaluating
	// to reduce memory and CPU usage
	scopeMetrics := rm.ScopeMetrics()
	for i := 0; i < scopeMetrics.Len(); i++ {
		sm := scopeMetrics.At(i)
		metrics := sm.Metrics()
		for j := 0; j < metrics.Len(); j++ {
			m := metrics.At(j)
			name := m.Name()

			// Only process metrics that have thresholds configured or are used in multi-metric evaluation
			if !p.shouldProcessMetric(name) {
				continue
			}

			value, supported := p.extractSingleMetricValue(m)
			if supported {
				values[name] = value
			} else {
				unsupportedSeen = true
			}
		}
	}

	if unsupportedSeen {
		p.logger.Debug("Some metrics ignored due to unsupported type (only Gauge/Sum supported - these contain direct numeric values)")
	}
	return values
}

// shouldProcessMetric determines if a metric should be processed based on configuration
func (p *processorImp) shouldProcessMetric(name string) bool {
	// Process only metrics with configured thresholds for better performance
	_, hasThreshold := p.config.MetricThresholds[name]
	if hasThreshold {
		return true
	}

	// Check if metric is used in multi-metric evaluation
	if p.multiMetricEnabled {
		_, hasWeight := p.config.Weights[name]
		return hasWeight
	}

	return false
}

// extractSingleMetricValue extracts value from a single metric
func (p *processorImp) extractSingleMetricValue(m pmetric.Metric) (float64, bool) {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		return p.extractGaugeValue(m.Gauge()), true
	case pmetric.MetricTypeSum:
		return p.extractSumValue(m.Sum()), true
	default:
		return 0, false
	}
}

// extractGaugeValue sums all gauge data points
func (p *processorImp) extractGaugeValue(g pmetric.Gauge) float64 {
	dataPoints := g.DataPoints()
	dpLen := dataPoints.Len()

	if dpLen == 0 {
		return 0
	}

	// Fast path for common case of single data point
	if dpLen == 1 {
		return dataPoints.At(0).DoubleValue()
	}

	// Sum multiple datapoints
	var sum float64
	for k := 0; k < dpLen; k++ {
		sum += dataPoints.At(k).DoubleValue()
	}
	return sum
}

// extractSumValue extracts value from sum metric (aggregates all data points if multiple exist)
func (p *processorImp) extractSumValue(s pmetric.Sum) float64 {
	dataPoints := s.DataPoints()
	dataPointCount := dataPoints.Len()

	if dataPointCount == 0 {
		return 0
	}

	if dataPointCount == 1 {
		return dataPoints.At(0).DoubleValue()
	}

	// Aggregate all data points
	var total float64
	for k := 0; k < dataPointCount; k++ {
		total += dataPoints.At(k).DoubleValue()
	}
	return total
}

// calculateCompositeGeneric calculates a composite score based on weighted metrics
func (p *processorImp) calculateCompositeGeneric(values map[string]float64) (float64, string) {
	weights := p.config.Weights
	if len(weights) == 0 {
		p.logger.Debug("Multi-metric evaluation skipped: no weights configured")
		return 0, ""
	}

	// Debug all available metrics and configured weights/thresholds
	p.logger.Debug("Multi-metric evaluation starting",
		zap.Int("available_metrics", len(values)),
		zap.Int("configured_weights", len(weights)),
		zap.Float64("composite_threshold", p.config.CompositeThreshold))

	// Log which configured metrics are missing from values
	missingMetrics := []string{}
	presentMetrics := []string{}
	for metric := range weights {
		if val, exists := values[metric]; exists {
			presentMetrics = append(presentMetrics,
				fmt.Sprintf("%s:%.2f(w:%.2f)", metric, val, weights[metric]))
		} else {
			missingMetrics = append(missingMetrics, metric)
		}
	}

	if len(missingMetrics) > 0 {
		p.logger.Debug("Some weighted metrics are missing values",
			zap.Strings("missing_metrics", missingMetrics),
			zap.Strings("present_metrics", presentMetrics))
	}

	// Debug all the values available
	p.logger.Debug("Multi-metric evaluation values",
		zap.Any("available_values", values),
		zap.Any("configured_weights", weights),
		zap.Any("configured_thresholds", p.config.MetricThresholds))

	// Sort metrics for consistent evaluation
	keys := make([]string, 0, len(weights))
	for k := range weights {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var score float64
	parts := make([]string, 0, len(keys))
	metricCount := 0
	processedMetrics := make([]string, 0, len(keys))

	for _, metric := range keys {
		w := weights[metric]
		v, ok := values[metric]
		if !ok {
			p.logger.Debug("Multi-metric missing value for weighted metric",
				zap.String("metric", metric),
				zap.Float64("weight", w))
			continue
		}

		// Get threshold (prefer dynamic if enabled)
		t, hasThreshold := p.config.MetricThresholds[metric]
		if !hasThreshold {
			// If no threshold is configured, use a default threshold based on the metric value
			// This ensures metrics with weights but no thresholds can still contribute
			t = v * 1.5 // Use 1.5x current value as default threshold
			p.logger.Debug("Multi-metric using default threshold for metric with weight",
				zap.String("metric", metric),
				zap.Float64("default_threshold", t),
				zap.Float64("value", v))
		} else if p.dynamicThresholdsEnabled {
			// Use dynamic threshold if available
			if dt, ok := p.dynamicCustomThresholds[metric]; ok && dt > 0 {
				t = dt
			}
		}

		if t <= 0 {
			p.logger.Debug("Multi-metric skipping metric with zero/negative threshold",
				zap.String("metric", metric),
				zap.Float64("threshold", t),
				zap.Float64("value", v))
			continue
		}

		// Keep track of which metrics we're processing
		processedMetrics = append(processedMetrics, metric)

		metricCount++
		norm := v / t
		score += norm * w
		parts = append(parts, fmt.Sprintf("(%s:%.2f/%.2f×%.2f)", metric, v, t, w))
	}

	if len(parts) == 0 {
		return 0, ""
	}

	// Build reason string
	reasonStr := fmt.Sprintf("Score %.2f = %s", score, strings.Join(parts, " + "))

	// Log at appropriate level
	threshold := p.config.CompositeThreshold
	if threshold <= 0 {
		threshold = defaultCompositeThreshold
	}

	// Log the composite score calculation
	if score >= threshold {
		p.logger.Info("Multi-metric composite score exceeded threshold",
			zap.Float64("score", score),
			zap.Float64("threshold", threshold),
			zap.Int("metrics_processed", metricCount),
			zap.Strings("processed_metrics", processedMetrics))
	} else {
		p.logger.Debug("Multi-metric composite score below threshold",
			zap.Float64("score", score),
			zap.Float64("threshold", threshold),
			zap.Int("metrics_processed", metricCount),
			zap.Strings("processed_metrics", processedMetrics))
	}

	return score, reasonStr
}
