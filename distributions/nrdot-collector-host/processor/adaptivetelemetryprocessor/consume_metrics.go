package adaptivetelemetryprocessor

import (
	"context"
	"math"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// ConsumeMetrics implements consumer.Metrics; called by the OTel Collector pipeline when metrics arrive.
func (p *processorImp) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	// Track batch processing time for performance monitoring
	batchStart := time.Now()
	defer p.logBatchProcessingTime(batchStart)

	// Calculate and log input metrics statistics
	inputStats := p.calculateInputStats(md)
	p.logInputStats(inputStats)

	// Create a context with timeout to prevent processing from hanging indefinitely
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Process metrics with safeguards
	filteredMetrics, processingDuration, err := p.processMetricsWithTiming(ctxWithTimeout, md)
	if err != nil {
		return p.handleProcessingError(ctx, md, err, processingDuration)
	}

	// Safety check for empty results
	if err := p.validateProcessingResults(ctx, md, filteredMetrics, processingDuration); err != nil {
		return err
	}

	// Calculate output statistics and perform maintenance tasks
	outputStats := p.calculateOutputStats(filteredMetrics)
	p.performMaintenanceTasks()

	// Send metrics to next consumer
	return p.forwardMetricsToNextConsumer(ctx, filteredMetrics, outputStats)
}

// logBatchProcessingTime logs slow batch processing warnings
func (p *processorImp) logBatchProcessingTime(batchStart time.Time) {
	batchDuration := time.Since(batchStart)
	if batchDuration > 1*time.Second {
		p.logger.Warn("Slow batch processing detected",
			zap.Duration("total_batch_duration", batchDuration))
	}
}

// InputStats holds statistics about input metrics
type InputStats struct {
	ResourceCount int
	ScopeCount    int
	MetricCount   int
}

// calculateInputStats computes statistics for input metrics
func (p *processorImp) calculateInputStats(md pmetric.Metrics) InputStats {
	stats := InputStats{
		ResourceCount: md.ResourceMetrics().Len(),
	}

	// Count all metrics and scopes in the batch
	for i := 0; i < stats.ResourceCount; i++ {
		rm := md.ResourceMetrics().At(i)
		stats.ScopeCount += rm.ScopeMetrics().Len()

		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			stats.MetricCount += sm.Metrics().Len()
		}
	}

	return stats
}

// logInputStats logs input statistics with appropriate log level
func (p *processorImp) logInputStats(stats InputStats) {
	// Only log INFO for the first message every minute, use DEBUG for the rest
	shouldLogInfo := time.Since(p.lastInfoLogTime) > time.Minute
	if shouldLogInfo {
		p.lastInfoLogTime = time.Now()
		p.logger.Info("ConsumeMetrics called",
			zap.Int("input_resource_metrics_count", stats.ResourceCount),
			zap.Int("input_scope_count", stats.ScopeCount),
			zap.Int("input_metric_count", stats.MetricCount),
			zap.Float64("avg_metrics_per_resource", float64(stats.MetricCount)/math.Max(1.0, float64(stats.ResourceCount))))
	} else {
		p.logger.Debug("ConsumeMetrics called",
			zap.Int("input_resource_metrics_count", stats.ResourceCount),
			zap.Int("input_scope_count", stats.ScopeCount),
			zap.Int("input_metric_count", stats.MetricCount))
	}
}

// processMetricsWithTiming processes metrics and tracks timing
func (p *processorImp) processMetricsWithTiming(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, time.Duration, error) {
	processingStart := time.Now()
	filteredMetrics, err := p.processMetrics(ctx, md)
	processingDuration := time.Since(processingStart)
	return filteredMetrics, processingDuration, err
}

// handleProcessingError handles errors during metric processing
func (p *processorImp) handleProcessingError(ctx context.Context, md pmetric.Metrics, err error, processingDuration time.Duration) error {
	// Log detailed error information
	if ctxErr := ctx.Err(); ctxErr != nil {
		p.logger.Error("Context error during processing",
			zap.Error(ctxErr),
			zap.Duration("processing_duration", processingDuration))
	}
	p.logger.Error("Error processing metrics, falling back to original metrics",
		zap.Error(err),
		zap.Duration("processing_duration", processingDuration))
	// Fall back to passing through all metrics if processing fails
	return p.nextConsumer.ConsumeMetrics(ctx, md)
}

// validateProcessingResults checks if processing results are valid
func (p *processorImp) validateProcessingResults(ctx context.Context, md, filteredMetrics pmetric.Metrics, processingDuration time.Duration) error {
	// Safety check - if processing returned zero resources but input had resources,
	// fall back to the original metrics to ensure data keeps flowing
	if filteredMetrics.ResourceMetrics().Len() == 0 && md.ResourceMetrics().Len() > 0 {
		p.logger.Warn("Processing resulted in zero resources, falling back to original metrics",
			zap.Int("input_resources", md.ResourceMetrics().Len()),
			zap.Duration("processing_duration", processingDuration))
		return p.nextConsumer.ConsumeMetrics(ctx, md)
	}

	// Log metrics count after processing with detailed timing
	p.logger.Debug("Metrics processed",
		zap.Int("input_resources", md.ResourceMetrics().Len()),
		zap.Int("output_resources", filteredMetrics.ResourceMetrics().Len()),
		zap.Duration("processing_duration", processingDuration))

	return nil
}

// OutputStats holds statistics about output metrics
type OutputStats struct {
	TotalMetricCount int
	MetricTypeCount  map[string]int
	ResourceCount    int
}

// calculateOutputStats computes statistics for output metrics
func (p *processorImp) calculateOutputStats(filteredMetrics pmetric.Metrics) OutputStats {
	stats := OutputStats{
		MetricTypeCount: make(map[string]int),
		ResourceCount:   filteredMetrics.ResourceMetrics().Len(),
	}

	// Count metrics in each resource and track metric types
	for i := 0; i < stats.ResourceCount; i++ {
		rm := filteredMetrics.ResourceMetrics().At(i)

		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			stats.TotalMetricCount += sm.Metrics().Len()

			// Count metrics by type
			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				p.countMetricByType(m, stats.MetricTypeCount)
			}
		}

		// Only log individual resources at debug level for first 5 resources
		if ce := p.logger.Check(zap.DebugLevel, "Resource metrics"); ce != nil && i < 5 {
			resourceID := buildResourceIdentity(rm.Resource())
			p.logger.Debug("Output resource", zap.String("resource_id", resourceID))
		}
	}

	// Log output summary
	p.logger.Debug("Metrics output summary",
		zap.Int("total_resource_count", stats.ResourceCount),
		zap.Int("total_metric_count", stats.TotalMetricCount),
		zap.Float64("metrics_per_resource", float64(stats.TotalMetricCount)/math.Max(1.0, float64(stats.ResourceCount))))

	return stats
}

// countMetricByType categorizes metrics by their type
func (p *processorImp) countMetricByType(m pmetric.Metric, typeCount map[string]int) {
	switch m.Type() {
	case pmetric.MetricTypeGauge:
		typeCount["gauge"]++
	case pmetric.MetricTypeSum:
		typeCount["sum"]++
	case pmetric.MetricTypeHistogram:
		typeCount["histogram"]++
	case pmetric.MetricTypeSummary:
		typeCount["summary"]++
	case pmetric.MetricTypeExponentialHistogram:
		typeCount["exp_histogram"]++
	default:
		typeCount["unknown"]++
	}
}

// performMaintenanceTasks runs periodic maintenance operations
func (p *processorImp) performMaintenanceTasks() {
	// Run persistence operations if needed but only once per minute to reduce overhead
	if p.persistenceEnabled && time.Since(p.lastPersistenceOp) > time.Minute {
		if err := p.persistTrackedEntities(); err != nil {
			p.logger.Warn("Failed to persist tracked entities", zap.Error(err))
		}
		p.lastPersistenceOp = time.Now()
	}
}

// forwardMetricsToNextConsumer sends processed metrics to the next consumer
func (p *processorImp) forwardMetricsToNextConsumer(ctx context.Context, filteredMetrics pmetric.Metrics, stats OutputStats) error {
	// Call the next consumer with appropriate timeout handling
	nextStart := time.Now()
	consumeCtx, cancelConsume := context.WithTimeout(ctx, 10*time.Second)
	defer cancelConsume()

	// Always log what we're about to send to the next consumer
	p.logger.Info("SENDING METRICS to next consumer",
		zap.Int("resource_count", stats.ResourceCount),
		zap.Int("metric_count", stats.TotalMetricCount),
		zap.Any("metric_types", stats.MetricTypeCount))

	err := p.nextConsumer.ConsumeMetrics(consumeCtx, filteredMetrics)
	consumeDuration := time.Since(nextStart)

	// Always log at INFO level regardless of the result
	if err != nil {
		p.logger.Error("ERROR from next consumer",
			zap.Error(err),
			zap.Duration("consumer_duration", consumeDuration),
			zap.Int("resource_count", stats.ResourceCount),
			zap.Int("metric_count", stats.TotalMetricCount))
	} else {
		p.logger.Info("SUCCESS: Metrics passed to next consumer",
			zap.Int("resource_count", stats.ResourceCount),
			zap.Int("metric_count", stats.TotalMetricCount),
			zap.Duration("consumer_duration", consumeDuration))
	}
	return err
}
