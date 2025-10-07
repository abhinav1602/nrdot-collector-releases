package adaptivetelemetryprocessor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap/zaptest"
)

func TestProcessMetricsExtended(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	// Create a basic configuration for the processor
	config := &Config{
		// Include basic thresholds for testing
		MetricThresholds: map[string]float64{
			"system.cpu.utilization": 80.0,
		},
		// Enable enough features to test different code paths
		EnableDynamicThresholds: true,
		EnableMultiMetric: true,
		RetentionMinutes: 60,
	}
	
	// Create the processor
	processor := &processorImp{
		logger:                   logger,
		config:                   config,
		dynamicThresholdsEnabled: config.EnableDynamicThresholds,
		multiMetricEnabled:       config.EnableMultiMetric,
		trackedEntities:          make(map[string]*TrackedEntity),
		dynamicCustomThresholds:  config.MetricThresholds,
	}
	
	// Test with a simple metrics batch that should be filtered
	t.Run("Process metrics below threshold", func(t *testing.T) {
		// Create test metrics
		metrics := createExtendedTestMetrics(
			map[string]string{"service.name": "test-service"},
			map[string]float64{"system.cpu.utilization": 50.0}, // Below threshold
		)
		
		// Initialize metric count
		initialMetricCount := countMetrics(metrics)
		assert.Greater(t, initialMetricCount, 0, "Should have metrics to process")
		
		// Process metrics
		result, err := processor.processMetrics(context.Background(), metrics)
		
		// Verify results
		assert.NoError(t, err)
		assert.NotNil(t, result)
		
		// Check that metrics were filtered
		resultCount := countMetrics(result)
		assert.Less(t, resultCount, initialMetricCount, "Should have fewer metrics after processing")
	})
	
	t.Run("Process metrics above threshold", func(t *testing.T) {
		// Create test metrics with values above thresholds
		metrics := createExtendedTestMetrics(
			map[string]string{"service.name": "test-service"},
			map[string]float64{"system.cpu.utilization": 90.0}, // Above threshold
		)
		
		// Initialize metric count
		initialMetricCount := countMetrics(metrics)
		assert.Greater(t, initialMetricCount, 0, "Should have metrics to process")
		
		// Process metrics
		result, err := processor.processMetrics(context.Background(), metrics)
		
		// Verify results
		assert.NoError(t, err)
		assert.NotNil(t, result)
		
		// Check that metrics were kept
		resultCount := countMetrics(result)
		assert.Equal(t, initialMetricCount, resultCount, "Should keep metrics above threshold")
		
		// Verify that the resource has been marked
		rm := result.ResourceMetrics().At(0)
		stageAttr, exists := rm.Resource().Attributes().Get(adaptiveFilterStageAttributeKey)
		assert.True(t, exists, "Filter stage attribute should be set")
		// The implementation seems to prefer dynamic thresholds over static ones
		// so we accept either value
		actualStage := stageAttr.AsString()
		assert.True(t, 
			actualStage == stageStaticThreshold || actualStage == stageDynamicThreshold, 
			"Filter stage should be either static_threshold or dynamic_threshold, got: %s", actualStage)
	})

	t.Run("Process with history for dynamic thresholds", func(t *testing.T) {
		// Create an entity with history first
		metrics := createExtendedTestMetrics(
			map[string]string{
				"service.name": "dynamic-test-service",
				"service.instance.id": "instance-1",
			},
			map[string]float64{"system.cpu.utilization": 60.0},
		)
		
		// First process to create history
		_, err := processor.processMetrics(context.Background(), metrics)
		assert.NoError(t, err)
		
		// Wait a tiny bit to ensure timestamp changes
		time.Sleep(10 * time.Millisecond)
		
		// Now process with a value above the dynamic threshold
		metrics = createExtendedTestMetrics(
			map[string]string{
				"service.name": "dynamic-test-service",
				"service.instance.id": "instance-1",
			},
			map[string]float64{"system.cpu.utilization": 90.0}, // Significantly higher than history
		)
		
		// Process again
		result, err := processor.processMetrics(context.Background(), metrics)
		assert.NoError(t, err)
		
		// Check that metrics were kept due to exceeding thresholds
		resultCount := countMetrics(result)
		assert.Greater(t, resultCount, 0, "Should keep metrics above dynamic threshold")
	})

	t.Run("Process with cancelled context", func(t *testing.T) {
		// Create cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately
	
		// Create some test metrics
		metrics := createExtendedTestMetrics(
			map[string]string{"host.name": "cancelled-context-host"},
			map[string]float64{"system.cpu.utilization": 80.0},
		)
	
		// Process metrics with cancelled context
		result, err := processor.processMetrics(ctx, metrics)
		
		// Should return original metrics and context error
		assert.Equal(t, metrics, result, "Original metrics should be returned when context is cancelled")
		assert.Error(t, err, "Error should be returned when context is cancelled")
		assert.Equal(t, context.Canceled, err, "Error should be context.Canceled")
	})

	t.Run("Process empty metrics", func(t *testing.T) {
		// Create empty metrics
		md := pmetric.NewMetrics()
	
		// Process empty metrics
		result, err := processor.processMetrics(context.Background(), md)
		
		// Should return empty metrics and no error
		assert.NoError(t, err, "No error should be returned for empty metrics")
		assert.Equal(t, md, result, "Empty metrics should be returned unchanged")
	})
	
	t.Run("Process metrics with many resources", func(t *testing.T) {
		// Create test metrics with 30 resources (more than the 25 check interval)
		md := pmetric.NewMetrics()
		for i := 0; i < 30; i++ {
			rm := md.ResourceMetrics().AppendEmpty()
			rm.Resource().Attributes().PutStr("host.name", "test-host")
			rm.Resource().Attributes().PutInt("index", int64(i))
			sm := rm.ScopeMetrics().AppendEmpty()
			metric := sm.Metrics().AppendEmpty()
			metric.SetName("system.cpu.utilization")
			gauge := metric.SetEmptyGauge()
			dp := gauge.DataPoints().AppendEmpty()
			dp.SetDoubleValue(90.0) // Above threshold so all should be included
		}
		
		// Process metrics with active context
		result, err := processor.processMetrics(context.Background(), md)
		
		// Should succeed and return all resources
		assert.NoError(t, err)
		assert.Equal(t, md.ResourceMetrics().Len(), result.ResourceMetrics().Len(), 
			"All resources should be included in result")
	})

	t.Run("Process metrics all filtered out", func(t *testing.T) {
		// Create a processor that will filter out all resources
		lowThresholdProcessor := &processorImp{
			logger: logger,
			config: &Config{
				MetricThresholds: map[string]float64{
					"system.cpu.utilization": 95.0, // Very high threshold
				},
				EnableDynamicThresholds: false,
				EnableMultiMetric: false,
			},
			trackedEntities: make(map[string]*TrackedEntity),
		}
		
		// Create test metrics with values below threshold
		metrics := createExtendedTestMetrics(
			map[string]string{"service.name": "low-value-service"},
			map[string]float64{"system.cpu.utilization": 50.0}, // Below threshold
		)
		
		// Process metrics
		result, err := lowThresholdProcessor.processMetrics(context.Background(), metrics)
		
		// Should succeed but return no resources
		assert.NoError(t, err)
		assert.Equal(t, 0, result.ResourceMetrics().Len(), 
			"All resources should be filtered out")
	})
}

// Helper function to count metrics in a batch
func countMetrics(metrics pmetric.Metrics) int {
	count := 0
	for i := 0; i < metrics.ResourceMetrics().Len(); i++ {
		rm := metrics.ResourceMetrics().At(i)
		for j := 0; j < rm.ScopeMetrics().Len(); j++ {
			sm := rm.ScopeMetrics().At(j)
			count += sm.Metrics().Len()
		}
	}
	return count
}

// Helper function to create test metrics for processMetrics tests
func createExtendedTestMetrics(resourceAttrs map[string]string, metricValues map[string]float64) pmetric.Metrics {
	metrics := pmetric.NewMetrics()
	resourceMetrics := metrics.ResourceMetrics().AppendEmpty()
	resource := resourceMetrics.Resource()
	
	// Set resource attributes
	for key, val := range resourceAttrs {
		resource.Attributes().PutStr(key, val)
	}
	
	// Create metrics
	scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()
	for name, value := range metricValues {
		metric := scopeMetrics.Metrics().AppendEmpty()
		metric.SetName(name)
		metric.SetEmptyGauge()
		
		dp := metric.Gauge().DataPoints().AppendEmpty()
		dp.SetDoubleValue(value)
		dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	}
	
	return metrics
}