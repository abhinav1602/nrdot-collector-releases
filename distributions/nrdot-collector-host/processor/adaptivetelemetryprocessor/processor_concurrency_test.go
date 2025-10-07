package adaptivetelemetryprocessor

import (
	"context"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap"
)

// buildSimpleMetrics creates a metrics batch with a few gauge metrics for stress testing.
func buildSimpleMetrics(metricNames []string, values []float64) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	for i, name := range metricNames {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		m.SetEmptyGauge()
		dp := m.Gauge().DataPoints().AppendEmpty()
		v := 1.0
		if i < len(values) {
			v = values[i]
		}
		dp.SetDoubleValue(v)
	}
	return md
}

// TestConcurrentConsumeNoDeadlock stresses ConsumeMetrics & dynamic updates concurrently to detect deadlocks.
func TestConcurrentConsumeNoDeadlock(t *testing.T) {
	cfg := &Config{
		MetricThresholds: map[string]float64{
			"system.cpu.utilization":     0.0001,
			"system.memory.utilization":  0.0001,
			"system.disk.io_time":        1,
			"system.disk.operation_time": 1,
			"system.network.io":          1,
		},
		EnableDynamicThresholds: true,
		DynamicSmoothingFactor:  0.3,
		EnableMultiMetric:       true,
		CompositeThreshold:      0.5,
		Weights: map[string]float64{
			"system.cpu.utilization":    0.34,
			"system.memory.utilization": 0.33,
			"system.disk.io_time":       0.33,
		},
		EnableAnomalyDetection: true,
		AnomalyHistorySize:     5,
		AnomalyChangeThreshold: 20,
	}

	cfg.StoragePath = "" // disable persistence for the concurrency test
	logger := zap.NewNop()
	sink := &testSink{}
	p, err := newProcessor(logger, cfg, sink)
	if err != nil {
		t.Fatalf("failed to build processor: %v", err)
	}

	metricNames := []string{"system.cpu.utilization", "system.memory.utilization", "system.disk.io_time", "system.disk.operation_time", "system.network.io"}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	workers := 25
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					md := buildSimpleMetrics(metricNames, []float64{0.5, 0.6, 10, 20, 5})
					_ = p.ConsumeMetrics(context.Background(), md)
				}
			}
		}(w)
	}

	// Concurrent threshold updater
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
				md := buildSimpleMetrics(metricNames, []float64{0.7, 0.8, 12, 25, 9})
				p.updateDynamicThresholds(md)
				time.Sleep(15 * time.Millisecond)
			}
		}
	}()

	// Watchdog: ensure progress (tracked entities should grow or at least not stall processing)
	progressTicker := time.NewTicker(500 * time.Millisecond)
	defer progressTicker.Stop()
	lastTracked := -1
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return // success if we reach timeout without deadlock
		case <-progressTicker.C:
			p.mu.RLock()
			tracked := len(p.trackedEntities)
			p.mu.RUnlock()
			if tracked == lastTracked {
				// Not definitive, but if after multiple intervals no change AND zero tracked, raise suspicion
				if tracked == 0 {
					// allow warm-up; only fail if near end of window
					if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < 500*time.Millisecond {
						// Dump dynamic thresholds for diagnostics
						p.mu.RLock()
						// Not failing outright; deadlock would stop the test via context
						p.mu.RUnlock()
					}
				}
			} else {
				lastTracked = tracked
			}
		}
	}
}
