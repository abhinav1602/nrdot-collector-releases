package adaptivetelemetryprocessor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestAddAttributeToMetricDataPointsAllTypes(t *testing.T) {
	testCases := []struct {
		name       string
		metricType pmetric.MetricType
		setup      func(metric pmetric.Metric)
		key        string
		value      string
		validate   func(t *testing.T, metric pmetric.Metric, key, value string)
	}{
		{
			name:       "Gauge metric",
			metricType: pmetric.MetricTypeGauge,
			setup: func(metric pmetric.Metric) {
				gauge := metric.Gauge()
				dp1 := gauge.DataPoints().AppendEmpty()
				dp1.SetDoubleValue(42.0)
				dp1.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
				
				dp2 := gauge.DataPoints().AppendEmpty()
				dp2.SetDoubleValue(84.0)
				dp2.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
			},
			key:   "test.key",
			value: "test.value",
			validate: func(t *testing.T, metric pmetric.Metric, key, value string) {
				gauge := metric.Gauge()
				assert.Equal(t, 2, gauge.DataPoints().Len())
				
				for i := 0; i < gauge.DataPoints().Len(); i++ {
					dp := gauge.DataPoints().At(i)
					val, ok := dp.Attributes().Get(key)
					assert.True(t, ok, "Attribute should be present")
					assert.Equal(t, value, val.AsString(), "Attribute value should match")
				}
			},
		},
		{
			name:       "Sum metric",
			metricType: pmetric.MetricTypeSum,
			setup: func(metric pmetric.Metric) {
				sum := metric.Sum()
				dp := sum.DataPoints().AppendEmpty()
				dp.SetDoubleValue(100.0)
				dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
			},
			key:   "test.key",
			value: "test.value",
			validate: func(t *testing.T, metric pmetric.Metric, key, value string) {
				sum := metric.Sum()
				assert.Equal(t, 1, sum.DataPoints().Len())
				
				dp := sum.DataPoints().At(0)
				val, ok := dp.Attributes().Get(key)
				assert.True(t, ok, "Attribute should be present")
				assert.Equal(t, value, val.AsString(), "Attribute value should match")
			},
		},
		{
			name:       "Histogram metric",
			metricType: pmetric.MetricTypeHistogram,
			setup: func(metric pmetric.Metric) {
				hist := metric.Histogram()
				dp := hist.DataPoints().AppendEmpty()
				dp.SetCount(100)
				dp.SetSum(5000.0)
				dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
			},
			key:   "test.key",
			value: "test.value",
			validate: func(t *testing.T, metric pmetric.Metric, key, value string) {
				hist := metric.Histogram()
				assert.Equal(t, 1, hist.DataPoints().Len())
				
				dp := hist.DataPoints().At(0)
				val, ok := dp.Attributes().Get(key)
				assert.True(t, ok, "Attribute should be present")
				assert.Equal(t, value, val.AsString(), "Attribute value should match")
			},
		},
		{
			name:       "Summary metric",
			metricType: pmetric.MetricTypeSummary,
			setup: func(metric pmetric.Metric) {
				summary := metric.Summary()
				dp := summary.DataPoints().AppendEmpty()
				dp.SetCount(100)
				dp.SetSum(5000.0)
				dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
				
				quantile1 := dp.QuantileValues().AppendEmpty()
				quantile1.SetQuantile(0.5)
				quantile1.SetValue(50)
				
				quantile2 := dp.QuantileValues().AppendEmpty()
				quantile2.SetQuantile(0.9)
				quantile2.SetValue(90)
			},
			key:   "test.key",
			value: "test.value",
			validate: func(t *testing.T, metric pmetric.Metric, key, value string) {
				summary := metric.Summary()
				assert.Equal(t, 1, summary.DataPoints().Len())
				
				dp := summary.DataPoints().At(0)
				val, ok := dp.Attributes().Get(key)
				assert.True(t, ok, "Attribute should be present")
				assert.Equal(t, value, val.AsString(), "Attribute value should match")
			},
		},
		{
			name:       "Exponential histogram metric",
			metricType: pmetric.MetricTypeExponentialHistogram,
			setup: func(metric pmetric.Metric) {
				hist := metric.ExponentialHistogram()
				dp := hist.DataPoints().AppendEmpty()
				dp.SetCount(100)
				dp.SetSum(5000.0)
				dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
			},
			key:   "test.key",
			value: "test.value",
			validate: func(t *testing.T, metric pmetric.Metric, key, value string) {
				hist := metric.ExponentialHistogram()
				assert.Equal(t, 1, hist.DataPoints().Len())
				
				dp := hist.DataPoints().At(0)
				val, ok := dp.Attributes().Get(key)
				assert.True(t, ok, "Attribute should be present")
				assert.Equal(t, value, val.AsString(), "Attribute value should match")
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a metric with the specified type
			metric := pmetric.NewMetric()
			metric.SetName("test.metric")
			
			// Setup the metric based on its type
			switch tc.metricType {
			case pmetric.MetricTypeGauge:
				metric.SetEmptyGauge()
			case pmetric.MetricTypeSum:
				metric.SetEmptySum()
			case pmetric.MetricTypeHistogram:
				metric.SetEmptyHistogram()
			case pmetric.MetricTypeSummary:
				metric.SetEmptySummary()
			case pmetric.MetricTypeExponentialHistogram:
				metric.SetEmptyExponentialHistogram()
			default:
				t.Fatalf("Unsupported metric type: %v", tc.metricType)
			}
			
			// Setup the metric data points
			tc.setup(metric)
			
			// Call the function under test
			addAttributeToMetricDataPoints(metric, tc.key, tc.value)
			
			// Validate the results
			tc.validate(t, metric, tc.key, tc.value)
		})
	}
}