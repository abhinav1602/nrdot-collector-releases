package adaptivetelemetryprocessor

import (
	"fmt"
	"time"

	"go.uber.org/zap"
)

// detectAnomalyUtil checks for anomalous changes in any metric for a tracked entity.
// This is the first check in the filter flow, matching adaptiveprocessfilter.
func detectAnomalyUtil(p *processorImp, trackedEntity *TrackedEntity, currentValues map[string]float64) (bool, string) {
	if !p.config.EnableAnomalyDetection {
		return false, ""
	}

	// Get configuration values with defaults
	historySize, changeThreshold := p.getAnomalyConfig()

	// Initialize metric history if needed
	p.initializeMetricHistory(trackedEntity)

	// Check each metric for anomalies
	for metricName, currentValue := range currentValues {
		if anomalyDetected, reason := p.checkMetricAnomaly(trackedEntity, metricName, currentValue, historySize, changeThreshold); anomalyDetected {
			return true, reason
		}
	}

	return false, ""
}

// getAnomalyConfig retrieves anomaly detection configuration values with defaults
func (p *processorImp) getAnomalyConfig() (int, float64) {
	historySize := p.config.AnomalyHistorySize
	if historySize <= 0 {
		historySize = defaultAnomalyHistorySize
	}

	changeThreshold := p.config.AnomalyChangeThreshold
	if changeThreshold <= 0 {
		changeThreshold = defaultAnomalyChangeThreshold
	}

	return historySize, changeThreshold
}

// initializeMetricHistory ensures the metric history map is initialized
func (p *processorImp) initializeMetricHistory(trackedEntity *TrackedEntity) {
	if trackedEntity.MetricHistory == nil {
		trackedEntity.MetricHistory = make(map[string][]float64)
	}
}

// checkMetricAnomaly checks a single metric for anomalous behavior
func (p *processorImp) checkMetricAnomaly(trackedEntity *TrackedEntity, metricName string, currentValue float64, historySize int, changeThreshold float64) (bool, string) {
	// Only check metrics that have a defined threshold
	if _, has := p.config.MetricThresholds[metricName]; !has {
		return false, ""
	}

	history := trackedEntity.MetricHistory[metricName]

	// Update metric history
	p.updateMetricHistory(trackedEntity, metricName, currentValue, historySize)

	// Need enough history for anomaly detection
	if len(history) < 2 {
		return false, ""
	}

	// Calculate anomaly metrics
	avg := p.calculateAverage(history)
	pctChange := p.calculatePercentageChange(currentValue, avg)

	// Check for anomaly and handle if detected
	if pctChange >= changeThreshold {
		return p.handleAnomalyDetection(trackedEntity, metricName, currentValue, pctChange, avg)
	}

	return false, ""
}

// updateMetricHistory adds the current value to history and maintains size limit
func (p *processorImp) updateMetricHistory(trackedEntity *TrackedEntity, metricName string, currentValue float64, historySize int) {
	history := trackedEntity.MetricHistory[metricName]
	trackedEntity.MetricHistory[metricName] = append(history, currentValue)

	if len(trackedEntity.MetricHistory[metricName]) > historySize {
		trackedEntity.MetricHistory[metricName] = trackedEntity.MetricHistory[metricName][1:]
	}
}

// calculateAverage computes the average of historical values
func (p *processorImp) calculateAverage(history []float64) float64 {
	var sum float64
	for _, v := range history {
		sum += v
	}
	return sum / float64(len(history))
}

// calculatePercentageChange computes the percentage change from average
func (p *processorImp) calculatePercentageChange(currentValue, avg float64) float64 {
	if avg > 0 {
		return ((currentValue - avg) / avg) * 100
	}
	return 0.0
}

// handleAnomalyDetection processes when an anomaly is detected
func (p *processorImp) handleAnomalyDetection(trackedEntity *TrackedEntity, metricName string, currentValue, pctChange, avg float64) (bool, string) {
	// Update last anomaly time
	trackedEntity.LastAnomalyDetected = time.Now()

	// Format descriptive reason
	reason := fmt.Sprintf("%s anomaly: %.2f (%.1f%% change from avg %.2f)",
		metricName, currentValue, pctChange, avg)

	p.logger.Info("Anomaly detected",
		zap.String("entity_id", trackedEntity.Identity),
		zap.String("metric", metricName),
		zap.Float64("value", currentValue),
		zap.Float64("percent_change", pctChange),
		zap.Float64("average", avg))

	return true, reason
}
