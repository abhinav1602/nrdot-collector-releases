package adaptivetelemetryprocessor

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// processMetrics iterates resource metrics, applies threshold logic, and returns a filtered copy.
// Optimized for better performance with metrics batching and reduced memory allocations.
func (p *processorImp) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	start := time.Now()

	// Quick exit for empty metrics
	if md.ResourceMetrics().Len() == 0 {
		p.logger.Info("Received empty metrics batch, returning without processing")
		return md, nil
	}

	// Initialize processing context
	processCtx := p.initializeProcessingContext(ctx, md, start)

	// Check if context is already cancelled
	if processCtx.ctx.Err() != nil {
		return p.handleContextCancellation(md, processCtx)
	}

	// Update dynamic thresholds if needed
	p.updateDynamicThresholdsIfNeeded(processCtx, md)

	// Process all resources
	filtered, includedCount := p.processAllResources(processCtx, md)

	// Perform post-processing tasks
	p.performPostProcessingTasks(processCtx, md, filtered, includedCount, start)

	return filtered, nil
}

// ProcessingContext holds context information for metrics processing
type ProcessingContext struct {
	ctx              context.Context
	resourceCount    int
	totalMetricCount int
	metricTypeCount  map[string]int
}

// initializeProcessingContext sets up the processing context and logs batch information
func (p *processorImp) initializeProcessingContext(ctx context.Context, md pmetric.Metrics, start time.Time) *ProcessingContext {
	processCtx := &ProcessingContext{
		ctx:             ctx,
		resourceCount:   md.ResourceMetrics().Len(),
		metricTypeCount: make(map[string]int),
	}

	// Count metrics by type for better visibility
	processCtx.totalMetricCount = p.countMetricsByType(md, processCtx.metricTypeCount)

	// Log batch information
	p.logger.Info("Processing metrics batch",
		zap.Int("resources", processCtx.resourceCount),
		zap.Int("metrics", processCtx.totalMetricCount),
		zap.Int("tracked_entities", len(p.trackedEntities)),
		zap.Bool("dynamic_thresholds", p.dynamicThresholdsEnabled),
		zap.Bool("multi_metric", p.multiMetricEnabled),
		zap.Bool("anomaly_detection", p.config.EnableAnomalyDetection))

	return processCtx
}

// countMetricsByType counts metrics by their OpenTelemetry type
func (p *processorImp) countMetricsByType(md pmetric.Metrics, metricTypeCount map[string]int) int {
	totalMetricCount := 0

	for i := 0; i < md.ResourceMetrics().Len(); i++ {
		rm := md.ResourceMetrics().At(i)
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			totalMetricCount += sm.Metrics().Len()

			for k := 0; k < sm.Metrics().Len(); k++ {
				m := sm.Metrics().At(k)
				switch m.Type() {
				case pmetric.MetricTypeGauge:
					metricTypeCount["gauge"]++
				case pmetric.MetricTypeSum:
					metricTypeCount["sum"]++
				case pmetric.MetricTypeHistogram:
					metricTypeCount["histogram"]++
				case pmetric.MetricTypeSummary:
					metricTypeCount["summary"]++
				case pmetric.MetricTypeExponentialHistogram:
					metricTypeCount["exp_histogram"]++
				default:
					metricTypeCount["unknown"]++
				}
			}
		}
	}

	return totalMetricCount
}

// handleContextCancellation handles when context is cancelled before processing
func (p *processorImp) handleContextCancellation(md pmetric.Metrics, processCtx *ProcessingContext) (pmetric.Metrics, error) {
	p.logger.Warn("Context cancelled before processing started",
		zap.Error(processCtx.ctx.Err()),
		zap.Int("resource_count", processCtx.resourceCount),
		zap.Int("metric_count", processCtx.totalMetricCount))
	return md, processCtx.ctx.Err()
}

// updateDynamicThresholdsIfNeeded updates dynamic thresholds with timeout protection
func (p *processorImp) updateDynamicThresholdsIfNeeded(processCtx *ProcessingContext, md pmetric.Metrics) {
	if !p.dynamicThresholdsEnabled || time.Since(p.lastThresholdUpdate).Seconds() < dynamicUpdateIntervalSecs {
		return
	}

	thresholdUpdateCtx, cancel := context.WithTimeout(processCtx.ctx, 500*time.Millisecond)
	defer cancel()

	// Run dynamic threshold update in a goroutine with timeout
	thresholdUpdateDone := make(chan struct{})
	go func() {
		p.updateDynamicThresholds(md)
		p.lastThresholdUpdate = time.Now()
		close(thresholdUpdateDone)
	}()

	// Wait for update to complete or timeout
	select {
	case <-thresholdUpdateDone:
		p.logger.Info("Dynamic thresholds updated successfully")
	case <-thresholdUpdateCtx.Done():
		p.logger.Warn("Dynamic threshold update timed out, continuing with existing thresholds")
	}
}

// processAllResources processes all resource metrics and returns filtered results
func (p *processorImp) processAllResources(processCtx *ProcessingContext, md pmetric.Metrics) (pmetric.Metrics, int) {
	filtered := pmetric.NewMetrics()
	rms := md.ResourceMetrics()
	includedCount := 0

	// Process all resources with a time limit per resource
	for i := 0; i < rms.Len(); i++ {
		// Check context occasionally to allow cancellation during long processing
		if i > 0 && i%25 == 0 {
			if processCtx.ctx.Err() != nil {
				p.logger.Warn("Context cancelled during resource processing", zap.Error(processCtx.ctx.Err()))
				return md, 0 // Return original metrics on timeout
			}
		}

		rm := rms.At(i)
		if p.processingSingleResource(processCtx.ctx, rm, &filtered) {
			includedCount++
		}
	}

	p.logger.Debug("Resource filtering completed",
		zap.Int("included_count", includedCount),
		zap.Int("total_resources", processCtx.resourceCount))

	return filtered, includedCount
}

// processingSingleResource processes a single resource and returns whether it was included
func (p *processorImp) processingSingleResource(ctx context.Context, rm pmetric.ResourceMetrics, filtered *pmetric.Metrics) bool {
	resourceID := buildResourceIdentity(rm.Resource())

	// Try processing with timeout per resource
	resourceProcessingCtx, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()

	var includeResource bool
	includeReason := stageStaticThreshold

	// Run the evaluation with proper timeout handling
	select {
	case <-resourceProcessingCtx.Done():
		includeResource, includeReason = p.handleResourceTimeout(rm, resourceID)
	default:
		includeResource = p.shouldIncludeResource(rm.Resource(), rm)
		// Get the filter stage from the resource attributes that was set by shouldIncludeResource
		if stageAttr, hasStage := rm.Resource().Attributes().Get(adaptiveFilterStageAttributeKey); hasStage {
			includeReason = stageAttr.AsString()
		}
	}

	if includeResource {
		p.handleIncludedResource(rm, resourceID, includeReason, filtered)
		return true
	} else {
		p.handleExcludedResource(rm, resourceID)
		return false
	}
}

// handleResourceTimeout handles resource processing timeout with fallback evaluation
func (p *processorImp) handleResourceTimeout(rm pmetric.ResourceMetrics, resourceID string) (bool, string) {
	p.logger.Warn("Resource processing timed out, using fallback evaluation",
		zap.String("resource_id", resourceID))

	values := p.extractMetricValues(rm)

	// Try multi-metric evaluation first if enabled
	if includeResource, reason := p.tryMultiMetricEvaluation(rm, values, resourceID); includeResource {
		return true, reason
	}

	// Check for zero thresholds
	if p.hasZeroThresholdMetrics(values) {
		return p.setResourceStage(rm, stageResourceProcessingTimeout), stageResourceProcessingTimeout
	}

	// Check regular thresholds
	if p.hasRegularThresholdExceeded(values) {
		return p.setResourceStage(rm, stageResourceProcessingTimeout), stageResourceProcessingTimeout
	}

	return false, ""
}

// tryMultiMetricEvaluation attempts multi-metric evaluation during timeout
func (p *processorImp) tryMultiMetricEvaluation(rm pmetric.ResourceMetrics, values map[string]float64, resourceID string) (bool, string) {
	if !p.multiMetricEnabled || len(p.config.Weights) == 0 {
		return false, ""
	}

	score, _ := p.calculateCompositeGeneric(values)
	threshold := p.config.CompositeThreshold
	if threshold <= 0 {
		threshold = defaultCompositeThreshold
	}

	if score >= threshold {
		p.setResourceStage(rm, stageMultiMetric)
		p.logger.Info("Resource included on timeout: multi-metric threshold",
			zap.String("resource_id", resourceID),
			zap.Float64("score", score))
		return true, stageMultiMetric
	}

	return false, ""
}

// hasZeroThresholdMetrics checks if any metrics have zero thresholds
func (p *processorImp) hasZeroThresholdMetrics(values map[string]float64) bool {
	for metricName, threshold := range p.config.MetricThresholds {
		if threshold == 0.0 {
			if _, exists := values[metricName]; exists {
				return true
			}
		}
	}
	return false
}

// hasRegularThresholdExceeded checks if any regular thresholds are exceeded
func (p *processorImp) hasRegularThresholdExceeded(values map[string]float64) bool {
	for metricName, value := range values {
		threshold := p.config.MetricThresholds[metricName]
		if threshold > 0 && value >= threshold {
			return true
		}
	}
	return false
}

// setResourceStage sets the filter stage attribute on a resource
func (p *processorImp) setResourceStage(rm pmetric.ResourceMetrics, stage string) bool {
	rm.Resource().Attributes().PutStr(adaptiveFilterStageAttributeKey, stage)
	rm.Resource().Attributes().PutStr("adaptive.filter.stage", stage)
	return true
}

// handleIncludedResource processes a resource that should be included in output
func (p *processorImp) handleIncludedResource(rm pmetric.ResourceMetrics, resourceID, includeReason string, filtered *pmetric.Metrics) {
	resourceType := getResourceType(rm.Resource().Attributes())
	serviceName := "unknown"
	if val, ok := rm.Resource().Attributes().Get("service.name"); ok {
		serviceName = val.AsString()
	}

	p.logger.Info("Including resource in output",
		zap.String("resource_id", resourceID),
		zap.String("filter_stage", includeReason),
		zap.String("resource_type", resourceType),
		zap.String("service_name", serviceName),
		zap.Int("scope_count", rm.ScopeMetrics().Len()),
		zap.Int("metric_count_in_resource", countMetricsInResource(rm)))

	// Ensure filter stage attribute is set
	if _, hasStage := rm.Resource().Attributes().Get(adaptiveFilterStageAttributeKey); !hasStage {
		rm.Resource().Attributes().PutStr(adaptiveFilterStageAttributeKey, includeReason)
		rm.Resource().Attributes().PutStr("adaptive.filter.stage", includeReason)
	}

	// Add the filter stage attribute to all datapoints in the resource metrics
	p.addStageAttributeToMetrics(rm, includeReason)

	rm.CopyTo(filtered.ResourceMetrics().AppendEmpty())
}

// handleExcludedResource processes a resource that should be excluded from output
func (p *processorImp) handleExcludedResource(rm pmetric.ResourceMetrics, resourceID string) {
	resourceType := getResourceType(rm.Resource().Attributes())
	p.logger.Info("Excluding resource from output",
		zap.String("resource_id", resourceID),
		zap.String("resource_type", resourceType),
		zap.Int("metric_count", countMetricsInResource(rm)))
}

// addStageAttributeToMetrics adds filter stage attribute to all metric data points
func (p *processorImp) addStageAttributeToMetrics(rm pmetric.ResourceMetrics, includeReason string) {
	for i := 0; i < rm.ScopeMetrics().Len(); i++ {
		sm := rm.ScopeMetrics().At(i)
		for j := 0; j < sm.Metrics().Len(); j++ {
			m := sm.Metrics().At(j)
			addAttributeToMetricDataPoints(m, adaptiveFilterStageAttributeKey, includeReason)
		}
	}
}

// performPostProcessingTasks handles cleanup and final logging
func (p *processorImp) performPostProcessingTasks(processCtx *ProcessingContext, md, filtered pmetric.Metrics, includedCount int, start time.Time) {
	// Perform cleanup of expired entities with controlled frequency
	if processCtx.resourceCount > 0 && p.config.RetentionMinutes > 0 && rand.Float64() < 0.01 {
		go p.cleanupExpiredEntities()
	}

	processingTime := time.Since(start)
	outputResourceCount := filtered.ResourceMetrics().Len()
	outputMetricCount := p.countOutputMetrics(filtered)

	p.logger.Info("Metrics processing completed",
		zap.Int("input_resources", processCtx.resourceCount),
		zap.Int("output_resources", outputResourceCount),
		zap.Int("input_metrics", processCtx.totalMetricCount),
		zap.Int("output_metrics", outputMetricCount),
		zap.Duration("processing_time", processingTime),
		zap.Int("tracked_entities", len(p.trackedEntities)))

	// Log warning if all resources were filtered out
	if processCtx.resourceCount > 0 && outputResourceCount == 0 {
		p.logger.Warn("All resources were filtered out - check configuration",
			zap.Int("input_resources", processCtx.resourceCount),
			zap.Int("input_metrics", processCtx.totalMetricCount))
	}
}

// countOutputMetrics counts metrics in the filtered output
func (p *processorImp) countOutputMetrics(filtered pmetric.Metrics) int {
	outputMetricCount := 0
	for i := 0; i < filtered.ResourceMetrics().Len(); i++ {
		rm := filtered.ResourceMetrics().At(i)
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			outputMetricCount += rm.ScopeMetrics().At(j).Metrics().Len()
		}
	}
	return outputMetricCount
}

// shouldIncludeResource determines if a resource should be included in the filtered output
func (p *processorImp) shouldIncludeResource(resource pcommon.Resource, rm pmetric.ResourceMetrics) bool {
	// Get resource identity and basic info
	id := buildResourceIdentity(resource)
	resourceType := getResourceType(resource.Attributes())
	values := p.extractMetricValues(rm)

	// Log basic resource info
	p.logger.Debug("Evaluating resource",
		zap.String("resource_id", id),
		zap.String("resource_type", resourceType),
		zap.Int("metric_count", len(values)))

	// Take write lock for entity tracking
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if this is a known entity
	trackedEntity, exists := p.trackedEntities[id]

	if exists {
		return p.evaluateExistingEntity(resource, id, trackedEntity, values)
	} else {
		return p.evaluateNewEntity(resource, id, values)
	}
}

// evaluateExistingEntity evaluates filter stages for an existing tracked entity
func (p *processorImp) evaluateExistingEntity(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	// Update current and max values
	p.updateEntityValues(trackedEntity, values)

	// Check filter stages in order
	return p.checkAnomalyDetectionStage(resource, id, trackedEntity, values) ||
		p.checkThresholdStages(resource, id, trackedEntity, values) ||
		p.checkMultiMetricStage(resource, id, trackedEntity, values) ||
		p.checkRetentionStages(resource, id, trackedEntity)
}

// updateEntityValues updates current and max values for a tracked entity
func (p *processorImp) updateEntityValues(trackedEntity *TrackedEntity, values map[string]float64) {
	if trackedEntity.CurrentValues == nil {
		trackedEntity.CurrentValues = make(map[string]float64)
	}
	if trackedEntity.MaxValues == nil {
		trackedEntity.MaxValues = make(map[string]float64)
	}

	for m, v := range values {
		trackedEntity.CurrentValues[m] = v
		if v > trackedEntity.MaxValues[m] {
			trackedEntity.MaxValues[m] = v
		}
	}
}

// checkAnomalyDetectionStage checks for anomaly detection in existing entities
func (p *processorImp) checkAnomalyDetectionStage(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	if !p.config.EnableAnomalyDetection {
		return false
	}

	if isAnomaly, anomalyReason := p.detectAnomaly(trackedEntity, values); isAnomaly {
		trackedEntity.LastExceeded = time.Now()
		p.setResourceFilterStage(resource, stageAnomalyDetection)
		p.logger.Info("Resource included: anomaly detected",
			zap.String("resource_id", id),
			zap.String("details", anomalyReason))
		return true
	}
	return false
}

// checkThresholdStages checks dynamic and static threshold stages
func (p *processorImp) checkThresholdStages(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	if p.dynamicThresholdsEnabled {
		return p.checkDynamicThresholds(resource, id, trackedEntity, values)
	} else {
		return p.checkStaticThresholds(resource, id, trackedEntity, values)
	}
}

// checkDynamicThresholds checks dynamic threshold stage for existing entities
func (p *processorImp) checkDynamicThresholds(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	for m, v := range values {
		if threshold, ok := p.dynamicCustomThresholds[m]; ok && v >= threshold {
			trackedEntity.LastExceeded = time.Now()
			p.setResourceFilterStage(resource, stageDynamicThreshold)
			p.logger.Info("Resource included: dynamic threshold",
				zap.String("resource_id", id),
				zap.String("metric", m),
				zap.Float64("value", v),
				zap.Float64("threshold", threshold))
			return true
		}
	}
	return false
}

// checkStaticThresholds checks static threshold stage for existing entities
func (p *processorImp) checkStaticThresholds(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	for m, v := range values {
		if threshold := p.config.MetricThresholds[m]; threshold == 0.0 || (threshold > 0 && v >= threshold) {
			trackedEntity.LastExceeded = time.Now()
			p.setResourceFilterStage(resource, stageStaticThreshold)
			p.logger.Info("Resource included: static threshold",
				zap.String("resource_id", id),
				zap.String("metric", m),
				zap.Float64("value", v),
				zap.Float64("threshold", threshold))
			return true
		}
	}
	return false
}

// checkMultiMetricStage checks multi-metric stage for existing entities
func (p *processorImp) checkMultiMetricStage(resource pcommon.Resource, id string, trackedEntity *TrackedEntity, values map[string]float64) bool {
	if !p.multiMetricEnabled {
		return false
	}

	compScore, reason := p.calculateCompositeGeneric(values)
	threshold := p.config.CompositeThreshold
	if threshold <= 0 {
		threshold = defaultCompositeThreshold
	}

	if compScore >= threshold {
		trackedEntity.LastExceeded = time.Now()
		p.setResourceFilterStage(resource, stageMultiMetric)
		p.logger.Info("Resource included: multi-metric",
			zap.String("resource_id", id),
			zap.Float64("score", compScore),
			zap.Float64("threshold", threshold),
			zap.String("calculation", reason))
		return true
	}
	return false
}

// checkRetentionStages checks anomaly and standard retention stages
func (p *processorImp) checkRetentionStages(resource pcommon.Resource, id string, trackedEntity *TrackedEntity) bool {
	return p.checkAnomalyRetention(resource, id, trackedEntity) ||
		p.checkStandardRetention(resource, id, trackedEntity)
}

// checkAnomalyRetention checks anomaly retention stage
func (p *processorImp) checkAnomalyRetention(resource pcommon.Resource, id string, trackedEntity *TrackedEntity) bool {
	if !p.config.EnableAnomalyDetection || trackedEntity.LastAnomalyDetected.IsZero() {
		return false
	}

	anomalyRetentionMins := 30 // Default
	if p.config.RetentionMinutes > 0 {
		anomalyRetentionMins = int(p.config.RetentionMinutes)
	}

	if time.Since(trackedEntity.LastAnomalyDetected).Minutes() < float64(anomalyRetentionMins) {
		p.setResourceFilterStage(resource, stageAnomalyDetection)
		p.logger.Info("Resource included: anomaly retention",
			zap.String("resource_id", id),
			zap.Float64("minutes_since_anomaly", time.Since(trackedEntity.LastAnomalyDetected).Minutes()),
			zap.Int("retention_minutes", anomalyRetentionMins))
		return true
	}
	return false
}

// checkStandardRetention checks standard retention stage
func (p *processorImp) checkStandardRetention(resource pcommon.Resource, id string, trackedEntity *TrackedEntity) bool {
	if p.config.RetentionMinutes <= 0 {
		return false
	}

	retentionWindow := time.Duration(p.config.RetentionMinutes) * time.Minute
	if time.Since(trackedEntity.LastExceeded) < retentionWindow {
		p.setResourceFilterStage(resource, stageRetention)
		p.logger.Info("Resource included: retention period",
			zap.String("resource_id", id),
			zap.Duration("time_since_exceeded", time.Since(trackedEntity.LastExceeded)),
			zap.Int64("retention_minutes", p.config.RetentionMinutes))
		return true
	}
	return false
}

// evaluateNewEntity evaluates filter stages for a new entity
func (p *processorImp) evaluateNewEntity(resource pcommon.Resource, id string, values map[string]float64) bool {
	// Create new tracked entity
	newEntity := p.createNewTrackedEntity(id, values, resource)

	// Check filter stages for new entity
	include, stage := p.checkNewEntityFilterStages(id, newEntity, values)

	// Store entity if it should be included or if debug mode is enabled
	if include || p.config.DebugShowAllFilterStages {
		p.trackedEntities[id] = newEntity

		if include {
			p.setResourceFilterStage(resource, stage)
			p.logger.Info("Resource included: new resource",
				zap.String("resource_id", id),
				zap.String("filter_stage", stage))
			return true
		} else if p.config.DebugShowAllFilterStages {
			return p.handleDebugMode(resource, id, values)
		}
	}

	p.logger.Debug("Excluding new resource", zap.String("resource_id", id))
	return false
}

// createNewTrackedEntity creates a new tracked entity
func (p *processorImp) createNewTrackedEntity(id string, values map[string]float64, resource pcommon.Resource) *TrackedEntity {
	now := time.Now()
	newEntity := &TrackedEntity{
		Identity:      id,
		FirstSeen:     now,
		LastExceeded:  now,
		CurrentValues: values,
		MaxValues:     values,
		Attributes:    snapshotResourceAttributes(resource),
	}

	// Initialize history if needed for anomaly detection
	if p.config.EnableAnomalyDetection {
		newEntity.MetricHistory = make(map[string][]float64)
		for m, v := range values {
			newEntity.MetricHistory[m] = []float64{v}
		}
	}

	return newEntity
}

// checkNewEntityFilterStages checks all filter stages for a new entity
func (p *processorImp) checkNewEntityFilterStages(id string, newEntity *TrackedEntity, values map[string]float64) (bool, string) {
	// Check threshold stages first (dynamic or static)
	if include, stage := p.checkNewEntityThresholds(id, values); include {
		return true, stage
	}

	// Check multi-metric stage
	if include, stage := p.checkNewEntityMultiMetric(id, values); include {
		return true, stage
	}

	// Check anomaly detection stage
	if include, stage := p.checkNewEntityAnomaly(id, newEntity, values); include {
		return true, stage
	}

	return false, ""
}

// checkNewEntityThresholds checks threshold stages for new entities
func (p *processorImp) checkNewEntityThresholds(id string, values map[string]float64) (bool, string) {
	if p.dynamicThresholdsEnabled {
		for m, v := range values {
			if threshold, ok := p.dynamicCustomThresholds[m]; ok && v >= threshold {
				p.logger.Info("New resource exceeds dynamic threshold",
					zap.String("resource_id", id),
					zap.String("metric", m),
					zap.Float64("value", v),
					zap.Float64("threshold", threshold))
				return true, stageDynamicThreshold
			}
		}
	} else {
		for m, v := range values {
			if threshold := p.config.MetricThresholds[m]; threshold == 0.0 || (threshold > 0 && v >= threshold) {
				p.logger.Info("New resource exceeds static threshold",
					zap.String("resource_id", id),
					zap.String("metric", m),
					zap.Float64("value", v),
					zap.Float64("threshold", threshold))
				return true, stageStaticThreshold
			}
		}
	}
	return false, ""
}

// checkNewEntityMultiMetric checks multi-metric stage for new entities
func (p *processorImp) checkNewEntityMultiMetric(id string, values map[string]float64) (bool, string) {
	if !p.multiMetricEnabled {
		return false, ""
	}

	compScore, reason := p.calculateCompositeGeneric(values)
	threshold := p.config.CompositeThreshold
	if threshold <= 0 {
		threshold = defaultCompositeThreshold
	}

	if compScore >= threshold {
		p.logger.Info("New resource exceeds multi-metric threshold",
			zap.String("resource_id", id),
			zap.Float64("score", compScore),
			zap.Float64("threshold", threshold),
			zap.String("reason", reason))
		return true, stageMultiMetric
	}
	return false, ""
}

// checkNewEntityAnomaly checks anomaly detection stage for new entities
func (p *processorImp) checkNewEntityAnomaly(id string, newEntity *TrackedEntity, values map[string]float64) (bool, string) {
	if !p.config.EnableAnomalyDetection {
		return false, ""
	}

	if isAnomaly, anomalyReason := p.detectAnomaly(newEntity, values); isAnomaly {
		p.logger.Info("New resource shows anomaly",
			zap.String("resource_id", id),
			zap.String("reason", anomalyReason))
		return true, stageAnomalyDetection
	}
	return false, ""
}

// handleDebugMode handles debug mode for resources that don't match any filter
func (p *processorImp) handleDebugMode(resource pcommon.Resource, id string, values map[string]float64) bool {
	debugReason := "debug_no_match"
	if p.multiMetricEnabled {
		score, _ := p.calculateCompositeGeneric(values)
		threshold := p.config.CompositeThreshold
		if threshold <= 0 {
			threshold = defaultCompositeThreshold
		}
		debugReason = fmt.Sprintf("debug_no_match:multi_metric_score=%.2f/%.2f", score, threshold)
	}

	p.setResourceFilterStage(resource, debugReason)
	p.logger.Debug("Including resource for debugging",
		zap.String("resource_id", id),
		zap.String("debug_reason", debugReason))
	return true
}

// setResourceFilterStage sets the filter stage attribute on a resource
func (p *processorImp) setResourceFilterStage(resource pcommon.Resource, stage string) {
	resource.Attributes().PutStr(adaptiveFilterStageAttributeKey, stage)
}
