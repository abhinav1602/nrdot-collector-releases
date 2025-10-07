package adaptivetelemetryprocessor

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestCountMetricsInResource(t *testing.T) {
	testCases := []struct {
		name          string
		setup         func() pmetric.ResourceMetrics
		expectedCount int
	}{
		{
			name: "Empty resource metrics",
			setup: func() pmetric.ResourceMetrics {
				return pmetric.NewResourceMetrics()
			},
			expectedCount: 0,
		},
		{
			name: "One scope with one metric",
			setup: func() pmetric.ResourceMetrics {
				rm := pmetric.NewResourceMetrics()
				sm := rm.ScopeMetrics().AppendEmpty()
				metric := sm.Metrics().AppendEmpty()
				metric.SetName("test.metric")
				metric.SetEmptyGauge()
				dp := metric.Gauge().DataPoints().AppendEmpty()
				dp.SetDoubleValue(42.0)
				dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
				return rm
			},
			expectedCount: 1,
		},
		{
			name: "Multiple scopes with multiple metrics",
			setup: func() pmetric.ResourceMetrics {
				rm := pmetric.NewResourceMetrics()
				
				// First scope
				sm1 := rm.ScopeMetrics().AppendEmpty()
				m1 := sm1.Metrics().AppendEmpty()
				m1.SetName("metric1")
				m1.SetEmptyGauge()
				
				m2 := sm1.Metrics().AppendEmpty()
				m2.SetName("metric2")
				m2.SetEmptyGauge()
				
				// Second scope
				sm2 := rm.ScopeMetrics().AppendEmpty()
				m3 := sm2.Metrics().AppendEmpty()
				m3.SetName("metric3")
				m3.SetEmptyGauge()
				
				m4 := sm2.Metrics().AppendEmpty()
				m4.SetName("metric4")
				m4.SetEmptyGauge()
				
				m5 := sm2.Metrics().AppendEmpty()
				m5.SetName("metric5")
				m5.SetEmptyGauge()
				
				return rm
			},
			expectedCount: 5,
		},
		{
			name: "Empty scope metrics",
			setup: func() pmetric.ResourceMetrics {
				rm := pmetric.NewResourceMetrics()
				_ = rm.ScopeMetrics().AppendEmpty()
				return rm
			},
			expectedCount: 0,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rm := tc.setup()
			count := countMetricsInResource(rm)
			assert.Equal(t, tc.expectedCount, count)
		})
	}
}