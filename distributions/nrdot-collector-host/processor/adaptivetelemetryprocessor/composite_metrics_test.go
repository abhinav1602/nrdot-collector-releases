package adaptivetelemetryprocessor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap/zaptest"
)

func TestExtractMetricValuesComprehensive(t *testing.T) {
	logger := zaptest.NewLogger(t)

	testCases := []struct {
		name              string
		metrics           map[string][]float64 // metric name -> list of datapoint values
		metricTypes       map[string]pmetric.MetricType
		thresholds        map[string]float64
		weights           map[string]float64
		enableMultiMetric bool
		expectedExtracted map[string]float64
	}{
		{
			name: "Multiple datapoints in gauge metrics",
			metrics: map[string][]float64{
				"system.cpu.utilization": {10.0, 20.0, 30.0}, // Multiple datapoints
				"system.memory.usage":    {100.0},            // Single datapoint
			},
			metricTypes: map[string]pmetric.MetricType{
				"system.cpu.utilization": pmetric.MetricTypeGauge,
				"system.memory.usage":    pmetric.MetricTypeGauge,
			},
			thresholds: map[string]float64{
				"system.cpu.utilization": 50.0,
				"system.memory.usage":    200.0,
			},
			expectedExtracted: map[string]float64{
				"system.cpu.utilization": 60.0,  // Sum of 10.0 + 20.0 + 30.0
				"system.memory.usage":    100.0, // Single datapoint
			},
		},
		{
			name: "Mix of gauge and sum metrics",
			metrics: map[string][]float64{
				"system.cpu.utilization": {40.0},
				"system.memory.usage":    {150.0},
				"system.disk.operations": {1000.0}, // Sum metric
			},
			metricTypes: map[string]pmetric.MetricType{
				"system.cpu.utilization": pmetric.MetricTypeGauge,
				"system.memory.usage":    pmetric.MetricTypeGauge,
				"system.disk.operations": pmetric.MetricTypeSum, // Sum type now supported
			},
			thresholds: map[string]float64{
				"system.cpu.utilization": 50.0,
				"system.memory.usage":    200.0,
				"system.disk.operations": 2000.0,
			},
			expectedExtracted: map[string]float64{
				"system.cpu.utilization": 40.0,
				"system.memory.usage":    150.0,
				"system.disk.operations": 1000.0, // Sum metrics should now be included
			},
		},
		{
			name: "Only metrics with thresholds",
			metrics: map[string][]float64{
				"system.cpu.utilization": {40.0},
				"system.memory.usage":    {150.0},
				"system.disk.usage":      {75.0},  // No threshold
				"system.network.io":      {500.0}, // No threshold
			},
			metricTypes: map[string]pmetric.MetricType{
				"system.cpu.utilization": pmetric.MetricTypeGauge,
				"system.memory.usage":    pmetric.MetricTypeGauge,
				"system.disk.usage":      pmetric.MetricTypeGauge,
				"system.network.io":      pmetric.MetricTypeGauge,
			},
			thresholds: map[string]float64{
				"system.cpu.utilization": 50.0,
				"system.memory.usage":    200.0,
			},
			expectedExtracted: map[string]float64{
				"system.cpu.utilization": 40.0,
				"system.memory.usage":    150.0,
			},
		},
		{
			name: "Metrics with weights but no thresholds in multi-metric mode",
			metrics: map[string][]float64{
				"system.cpu.utilization": {40.0},
				"system.memory.usage":    {150.0},
				"system.disk.usage":      {75.0}, // No threshold, but has weight
			},
			metricTypes: map[string]pmetric.MetricType{
				"system.cpu.utilization": pmetric.MetricTypeGauge,
				"system.memory.usage":    pmetric.MetricTypeGauge,
				"system.disk.usage":      pmetric.MetricTypeGauge,
			},
			thresholds: map[string]float64{
				"system.cpu.utilization": 50.0,
				"system.memory.usage":    200.0,
			},
			weights: map[string]float64{
				"system.cpu.utilization": 0.5,
				"system.memory.usage":    0.3,
				"system.disk.usage":      0.2, // Has weight but no threshold
			},
			enableMultiMetric: true,
			expectedExtracted: map[string]float64{
				"system.cpu.utilization": 40.0,
				"system.memory.usage":    150.0,
				"system.disk.usage":      75.0, // Should be included because it has weight in multi-metric mode
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a processor for testing
			proc := &processorImp{
				logger: logger,
				config: &Config{
					MetricThresholds:  tc.thresholds,
					Weights:           tc.weights,
					EnableMultiMetric: tc.enableMultiMetric,
				},
				multiMetricEnabled: tc.enableMultiMetric,
			}

			// Create test metrics
			metrics := pmetric.NewMetrics()
			resourceMetrics := metrics.ResourceMetrics().AppendEmpty()

			// Set resource attributes
			resourceMetrics.Resource().Attributes().PutStr("service.name", "test-service")

			// Add scope metrics
			scopeMetrics := resourceMetrics.ScopeMetrics().AppendEmpty()

			// Add metrics
			for name, datapoints := range tc.metrics {
				metric := scopeMetrics.Metrics().AppendEmpty()
				metric.SetName(name)

				metricType := tc.metricTypes[name]

				// Create metric of the appropriate type
				switch metricType {
				case pmetric.MetricTypeGauge:
					gauge := metric.SetEmptyGauge()
					for _, value := range datapoints {
						dp := gauge.DataPoints().AppendEmpty()
						dp.SetDoubleValue(value)
						dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
					}
				case pmetric.MetricTypeSum:
					sum := metric.SetEmptySum()
					for _, value := range datapoints {
						dp := sum.DataPoints().AppendEmpty()
						dp.SetDoubleValue(value)
						dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
					}
				}
			}

			// Extract metric values
			values := proc.extractMetricValues(resourceMetrics)

			// Verify values match expected results
			assert.Equal(t, len(tc.expectedExtracted), len(values), "Number of extracted metrics should match")
			for metric, expected := range tc.expectedExtracted {
				actual, exists := values[metric]
				assert.True(t, exists, "Metric %s should be extracted", metric)
				assert.Equal(t, expected, actual, "Value for metric %s should match", metric)
			}

			// Also verify that metrics not in expected output are not present
			for metric := range values {
				_, shouldExist := tc.expectedExtracted[metric]
				assert.True(t, shouldExist, "Unexpected metric extracted: %s", metric)
			}
		})
	}
}
