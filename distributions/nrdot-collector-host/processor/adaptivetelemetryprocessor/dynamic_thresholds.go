package adaptivetelemetryprocessor

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// updateDynamicThresholds recalculates thresholds for all metrics listed in MetricThresholds.
// Optimized for better performance by reducing lock contention and unnecessary computations.
func (p *processorImp) updateDynamicThresholds(md pmetric.Metrics) {
	if !p.shouldUpdateDynamicThresholds() {
		return
	}

	// Initialize update process
	updateContext := p.initializeDynamicUpdate()

	// Compute metric averages from the current batch
	metricAvgs := p.computeMetricAverages(md, updateContext.metricKeys)

	// Calculate new threshold values
	newThresholds := p.calculateNewThresholds(metricAvgs, updateContext)

	// Apply the new thresholds
	p.applyThresholdUpdates(newThresholds, updateContext)

	// Log the update results
	p.logThresholdUpdate(updateContext, len(metricAvgs), len(newThresholds), md)
}

// shouldUpdateDynamicThresholds checks if dynamic threshold update should proceed
func (p *processorImp) shouldUpdateDynamicThresholds() bool {
	if !p.dynamicThresholdsEnabled {
		p.logger.Debug("Dynamic thresholds update skipped: not enabled")
		return false
	}

	// Check if it's too soon to update again
	timeSinceLastUpdate := time.Since(p.lastThresholdUpdate)
	if timeSinceLastUpdate < time.Duration(dynamicUpdateIntervalSecs)*time.Second/2 {
		p.logger.Debug("Skipping dynamic threshold update - too soon since last update",
			zap.Duration("time_since_last_update", timeSinceLastUpdate))
		return false
	}

	return true
}

// DynamicUpdateContext holds context information for a dynamic threshold update
type DynamicUpdateContext struct {
	startTime  time.Time
	smoothing  float64
	metricKeys []string
}

// initializeDynamicUpdate sets up the context for dynamic threshold update
func (p *processorImp) initializeDynamicUpdate() *DynamicUpdateContext {
	timeSinceLastUpdate := time.Since(p.lastThresholdUpdate)

	p.logger.Info("Starting dynamic thresholds update",
		zap.Int("metric_thresholds_configured", len(p.config.MetricThresholds)),
		zap.Int("current_dynamic_thresholds", len(p.dynamicCustomThresholds)),
		zap.Duration("time_since_last_update", timeSinceLastUpdate))

	smoothing := p.config.DynamicSmoothingFactor
	if smoothing <= 0 {
		smoothing = dynamicSmoothingFactor
	}

	start := time.Now()
	// Record update time at beginning to prevent frequent updates
	p.lastThresholdUpdate = start

	// Collect metric keys for better cache locality
	metricKeys := make([]string, 0, len(p.config.MetricThresholds))
	for metric := range p.config.MetricThresholds {
		metricKeys = append(metricKeys, metric)
	}

	return &DynamicUpdateContext{
		startTime:  start,
		smoothing:  smoothing,
		metricKeys: metricKeys,
	}
}

// MetricAverageData holds average calculation data for a metric
type MetricAverageData struct {
	avg   float64
	count int
}

// computeMetricAverages calculates averages for all configured metrics in a single pass
func (p *processorImp) computeMetricAverages(md pmetric.Metrics, metricKeys []string) map[string]MetricAverageData {
	metricAvgs := make(map[string]MetricAverageData, len(metricKeys))

	// Process all metrics in the batch
	rms := md.ResourceMetrics()
	for i := 0; i < rms.Len(); i++ {
		rm := rms.At(i)
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				p.processMetricForAverages(m, metricAvgs)
			}
		}
	}

	// Calculate final averages
	p.finalizeAverages(metricAvgs)

	return metricAvgs
}

// processMetricForAverages processes a single metric for average calculation
func (p *processorImp) processMetricForAverages(m pmetric.Metric, metricAvgs map[string]MetricAverageData) {
	name := m.Name()

	// Skip metrics we don't have thresholds for
	if _, ok := p.config.MetricThresholds[name]; !ok {
		return
	}

	if m.Type() != pmetric.MetricTypeGauge {
		return
	}

	g := m.Gauge()
	if g.DataPoints().Len() == 0 {
		return
	}

	var sum float64
	for d := 0; d < g.DataPoints().Len(); d++ {
		sum += g.DataPoints().At(d).DoubleValue()
	}

	val := metricAvgs[name]
	val.avg += sum
	val.count++
	metricAvgs[name] = val
}

// finalizeAverages calculates the final average values
func (p *processorImp) finalizeAverages(metricAvgs map[string]MetricAverageData) {
	for metric, data := range metricAvgs {
		if data.count > 0 {
			metricAvgs[metric] = MetricAverageData{
				avg:   data.avg / float64(data.count),
				count: data.count,
			}
		}
	}
}

// calculateNewThresholds computes new threshold values based on averages
func (p *processorImp) calculateNewThresholds(metricAvgs map[string]MetricAverageData, updateContext *DynamicUpdateContext) map[string]float64 {
	// Read all current thresholds at once to minimize lock time
	currentThresholds := p.getCurrentThresholds()

	// Calculate all new thresholds without holding the lock
	newThresholds := make(map[string]float64)

	for _, metric := range updateContext.metricKeys {
		newVal, shouldUpdate := p.calculateSingleThreshold(metric, metricAvgs, currentThresholds, updateContext.smoothing)
		if shouldUpdate {
			newThresholds[metric] = newVal
		}
	}

	return newThresholds
}

// getCurrentThresholds safely reads current dynamic thresholds
func (p *processorImp) getCurrentThresholds() map[string]float64 {
	p.mu.RLock()
	currentThresholds := make(map[string]float64, len(p.dynamicCustomThresholds))
	for k, v := range p.dynamicCustomThresholds {
		currentThresholds[k] = v
	}
	p.mu.RUnlock()
	return currentThresholds
}

// calculateSingleThreshold calculates a new threshold value for a single metric
func (p *processorImp) calculateSingleThreshold(metric string, metricAvgs map[string]MetricAverageData, currentThresholds map[string]float64, smoothing float64) (float64, bool) {
	base := p.config.MetricThresholds[metric]
	data, hasData := metricAvgs[metric]

	if !hasData || data.count == 0 {
		return 0, false
	}

	prev := currentThresholds[metric]
	if prev == 0 {
		prev = base
	}

	target := base + (data.avg * genericScalingFactor)
	newVal := (smoothing * target) + ((1 - smoothing) * prev)

	// Apply min/max constraints
	newVal = p.applyThresholdConstraints(metric, newVal)

	p.logger.Debug("Dynamic threshold updated",
		zap.String("metric", metric),
		zap.Float64("base", base),
		zap.Float64("avg", data.avg),
		zap.Float64("previous", prev),
		zap.Float64("new", newVal),
		zap.Int("samples", data.count))

	return newVal, true
}

// applyThresholdConstraints applies min/max constraints to a threshold value
func (p *processorImp) applyThresholdConstraints(metric string, value float64) float64 {
	// Apply min constraint
	if minV, ok := p.config.MinThresholds[metric]; ok && minV > 0 && value < minV {
		value = minV
	}

	// Apply max constraint
	if maxV, ok := p.config.MaxThresholds[metric]; ok && maxV > 0 && value > maxV {
		value = maxV
	}

	return value
}

// applyThresholdUpdates safely updates the dynamic thresholds
func (p *processorImp) applyThresholdUpdates(newThresholds map[string]float64, updateContext *DynamicUpdateContext) {
	if len(newThresholds) > 0 {
		p.mu.Lock()
		for k, v := range newThresholds {
			p.dynamicCustomThresholds[k] = v
		}
		p.mu.Unlock()
	}
}

// logThresholdUpdate logs the results of the threshold update
func (p *processorImp) logThresholdUpdate(updateContext *DynamicUpdateContext, totalAnalyzed, updatedCount int, md pmetric.Metrics) {
	duration := time.Since(updateContext.startTime)

	// Always log the summary at INFO level
	p.logger.Info("Dynamic threshold batch update complete",
		zap.Int("updated_metrics", updatedCount),
		zap.Int("total_metrics_analyzed", totalAnalyzed),
		zap.Int("total_dynamic_thresholds", len(p.dynamicCustomThresholds)),
		zap.Duration("duration", duration),
		zap.Float64("smoothing_factor", updateContext.smoothing))

	// Only log detailed warning if the update took too long
	if duration > 500*time.Millisecond {
		p.logger.Warn("Dynamic threshold update was slow",
			zap.Duration("duration", duration),
			zap.Int("metrics_analyzed", totalAnalyzed),
			zap.Int("resources_in_batch", md.ResourceMetrics().Len()))
	}

	// Log detailed thresholds at debug level
	p.logDetailedThresholds()
}

// logDetailedThresholds logs the actual dynamic thresholds with better formatting
func (p *processorImp) logDetailedThresholds() {
	if len(p.dynamicCustomThresholds) == 0 {
		return
	}

	// Group thresholds by metrics pattern for better readability
	thresholdsByPrefix := p.groupThresholdsByPrefix()

	// Log each group separately
	for prefix, thresholds := range thresholdsByPrefix {
		sort.Strings(thresholds)
		if len(thresholds) > 20 {
			// Truncate very long lists
			p.logger.Debug(fmt.Sprintf("Dynamic thresholds for %s (showing 20/%d)",
				prefix, len(thresholds)),
				zap.Strings("thresholds", thresholds[:20]),
				zap.Int("total_count", len(thresholds)))
		} else {
			p.logger.Debug(fmt.Sprintf("Dynamic thresholds for %s", prefix),
				zap.Strings("thresholds", thresholds))
		}
	}
}

// groupThresholdsByPrefix groups thresholds by metric prefix for organized logging
func (p *processorImp) groupThresholdsByPrefix() map[string][]string {
	thresholdsByPrefix := make(map[string][]string)

	p.mu.RLock()
	defer p.mu.RUnlock()

	for k, v := range p.dynamicCustomThresholds {
		prefix := strings.Split(k, ".")[0] // Group by first component of metric name
		formattedEntry := p.formatThresholdEntry(k, v)
		thresholdsByPrefix[prefix] = append(thresholdsByPrefix[prefix], formattedEntry)
	}

	return thresholdsByPrefix
}

// formatThresholdEntry formats a single threshold entry for logging
func (p *processorImp) formatThresholdEntry(metricName string, value float64) string {
	entry := strings.Builder{}
	entry.WriteString(metricName)
	entry.WriteString(":")

	// Format the value with 4 decimal places
	formattedValue := fmt.Sprintf("%.4f", value)

	// Simple replacements to clean up the formatting
	formattedValue = strings.Replace(formattedValue, "-0.0000", "0", 1)
	formattedValue = strings.Replace(formattedValue, "0.0000", "0", 1)

	// Handle trailing zeros
	if strings.Contains(formattedValue, ".") {
		formattedValue = strings.TrimRight(formattedValue, "0")
		formattedValue = strings.TrimRight(formattedValue, ".")
	}

	entry.WriteString(formattedValue)
	return entry.String()
}
