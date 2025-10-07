package adaptivetelemetryprocessor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.uber.org/zap/zaptest"
)

func TestNewProcessor(t *testing.T) {
	tmpDir := t.TempDir()
	logger := zaptest.NewLogger(t)
	nextConsumer := consumertest.NewNop()
	
	testCases := []struct {
		name          string
		config        *Config
		expectedErr   string
	}{
		{
			name: "Valid configuration",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": 5.0,
				},
				StoragePath:      filepath.Join(tmpDir, "valid.db"),
				RetentionMinutes: 20,
			},
		},
		{
			name: "Valid configuration with all features enabled",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": 5.0,
					"process.memory.utilization": 10.0,
				},
				StoragePath:             filepath.Join(tmpDir, "features.db"),
				RetentionMinutes:        20,
				EnableDynamicThresholds: true,
				DynamicSmoothingFactor:  0.3,
				MinThresholds: map[string]float64{
					"process.cpu.utilization": 1.0,
				},
				MaxThresholds: map[string]float64{
					"process.cpu.utilization": 20.0,
				},
				EnableMultiMetric:      true,
				CompositeThreshold:     1.2,
				Weights: map[string]float64{
					"process.cpu.utilization": 1.0,
					"process.memory.utilization": 0.8,
				},
				EnableAnomalyDetection: true,
				AnomalyHistorySize:     15,
				AnomalyChangeThreshold: 150.0,
			},
		},
		{
			name: "Invalid configuration - negative threshold",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": -5.0,
				},
			},
			expectedErr: "threshold for metric",
		},
		{
			name: "Storage disabled",
			config: &Config{
				StoragePath:      filepath.Join(tmpDir, "disabled.db"),
				EnableStorage:    func() *bool { b := false; return &b }(),
				RetentionMinutes: 20,
			},
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			proc, err := newProcessor(logger, tc.config, nextConsumer)
			
			if tc.expectedErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.expectedErr)
				assert.Nil(t, proc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, proc)
				
				// Verify processor fields are set correctly
				assert.Equal(t, logger, proc.logger)
				assert.Equal(t, tc.config, proc.config)
				assert.Equal(t, nextConsumer, proc.nextConsumer)
				assert.NotNil(t, proc.trackedEntities)
				assert.NotNil(t, proc.dynamicCustomThresholds)
				
				// Test storage setting
				if tc.config.EnableStorage != nil && !*tc.config.EnableStorage {
					assert.False(t, proc.persistenceEnabled)
				} else if tc.config.StoragePath != "" {
					assert.True(t, proc.persistenceEnabled)
				}
				
				// Verify feature flags
				assert.Equal(t, tc.config.EnableDynamicThresholds, proc.dynamicThresholdsEnabled)
				assert.Equal(t, tc.config.EnableMultiMetric, proc.multiMetricEnabled)

				// Cleanup
				if proc != nil {
					err := proc.Shutdown(context.Background())
					assert.NoError(t, err)
				}
			}
		})
	}
}

func TestProcessorStartShutdownWithStorage(t *testing.T) {
	tmpDir := t.TempDir()
	storagePath := filepath.Join(tmpDir, "processor.db")
	logger := zaptest.NewLogger(t)
	nextConsumer := consumertest.NewNop()
	
	config := &Config{
		StoragePath:      storagePath,
		RetentionMinutes: 10,
	}
	
	proc, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	assert.NotNil(t, proc)
	assert.True(t, proc.persistenceEnabled)
	
	// Start should succeed
	err = proc.Start(context.Background(), nil)
	require.NoError(t, err)
	
	// Add some test entities
	proc.mu.Lock()
	proc.trackedEntities["test-entity-1"] = &TrackedEntity{
		Identity:      "test-entity-1",
		FirstSeen:     time.Now(),
		LastExceeded:  time.Now(),
		CurrentValues: map[string]float64{"metric1": 10.5},
	}
	proc.mu.Unlock()
	
	// Force persistence
	err = proc.persistTrackedEntities()
	require.NoError(t, err)
	
	// Shutdown should persist entities and close storage
	err = proc.Shutdown(context.Background())
	require.NoError(t, err)
	
	// Verify data was persisted by creating a new processor and checking
	proc2, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	
	// Verify entity was loaded
	proc2.mu.RLock()
	assert.Len(t, proc2.trackedEntities, 1)
	assert.Contains(t, proc2.trackedEntities, "test-entity-1")
	proc2.mu.RUnlock()
	
	err = proc2.Shutdown(context.Background())
	require.NoError(t, err)
}

func TestProcessorCleanupExpiredEntities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	nextConsumer := consumertest.NewNop()
	
	config := &Config{
		RetentionMinutes: 10,
		EnableStorage:    func() *bool { b := false; return &b }(), // Disable storage for this test
	}
	
	proc, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	
	now := time.Now()
	
	// Add some test entities
	proc.mu.Lock()
	proc.trackedEntities["active"] = &TrackedEntity{
		Identity:      "active",
		FirstSeen:     now.Add(-20 * time.Minute),
		LastExceeded:  now.Add(-5 * time.Minute), // Within retention window
		CurrentValues: map[string]float64{"metric1": 10.5},
	}
	proc.trackedEntities["expired"] = &TrackedEntity{
		Identity:      "expired",
		FirstSeen:     now.Add(-30 * time.Minute),
		LastExceeded:  now.Add(-15 * time.Minute), // Outside retention window
		CurrentValues: map[string]float64{"metric1": 5.0},
	}
	proc.trackedEntities["old-expired"] = &TrackedEntity{
		Identity:      "old-expired",
		FirstSeen:     now.Add(-60 * time.Minute),
		LastExceeded:  now.Add(-40 * time.Minute), // Way outside retention window
		CurrentValues: map[string]float64{"metric1": 5.0},
	}
	proc.mu.Unlock()
	
	// Run cleanup
	proc.cleanupExpiredEntities()
	
	// Verify only active entity remains
	proc.mu.RLock()
	assert.Len(t, proc.trackedEntities, 1)
	assert.Contains(t, proc.trackedEntities, "active")
	assert.NotContains(t, proc.trackedEntities, "expired")
	assert.NotContains(t, proc.trackedEntities, "old-expired")
	proc.mu.RUnlock()
}

// Helper function to create test metrics
func createTestMetrics(resourceAttrs map[string]string, metrics map[string]float64) pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	
	// Set resource attributes
	if resourceAttrs != nil {
		for k, v := range resourceAttrs {
			rm.Resource().Attributes().PutStr(k, v)
		}
	}
	
	// Add metrics
	sm := rm.ScopeMetrics().AppendEmpty()
	for name, value := range metrics {
		m := sm.Metrics().AppendEmpty()
		m.SetName(name)
		m.SetEmptyGauge()
		dp := m.Gauge().DataPoints().AppendEmpty()
		dp.SetDoubleValue(value)
		dp.SetTimestamp(pcommon.NewTimestampFromTime(time.Now()))
	}
	
	return md
}

func TestConsumeMetricsBasic(t *testing.T) {
	logger := zaptest.NewLogger(t)
	nextConsumer := consumertest.NewNop()
	
	config := &Config{
		MetricThresholds: map[string]float64{
			"process.cpu.utilization": 5.0,
		},
		EnableStorage: func() *bool { b := false; return &b }(), // Disable storage
	}
	
	proc, err := newProcessor(logger, config, nextConsumer)
	require.NoError(t, err)
	
	// Create test metrics that exceed threshold
	md := createTestMetrics(
		map[string]string{"service.name": "test-service", "host.name": "test-host"},
		map[string]float64{"process.cpu.utilization": 10.0}, // Exceeds threshold
	)
	
	// Process metrics
	err = proc.ConsumeMetrics(context.Background(), md)
	require.NoError(t, err)
	
	// Create test metrics that don't exceed threshold
	md = createTestMetrics(
		map[string]string{"service.name": "test-service", "host.name": "test-host"},
		map[string]float64{"process.cpu.utilization": 2.0}, // Below threshold
	)
	
	// Process metrics again (should filter out)
	err = proc.ConsumeMetrics(context.Background(), md)
	require.NoError(t, err)
}

func TestCalculateCompositeGeneric(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	tests := []struct {
		name      string
		config    *Config
		values    map[string]float64
		expected  float64
	}{
		{
			name: "Single metric with weight",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": 10.0,
				},
				Weights: map[string]float64{
					"process.cpu.utilization": 1.0,
				},
			},
			values: map[string]float64{
				"process.cpu.utilization": 15.0,
			},
			expected: 1.5, // 15/10 = 1.5
		},
		{
			name: "Multiple metrics with weights",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization":    10.0,
					"process.memory.utilization": 20.0,
				},
				Weights: map[string]float64{
					"process.cpu.utilization":    0.7,
					"process.memory.utilization": 0.3,
				},
			},
			values: map[string]float64{
				"process.cpu.utilization":    15.0, // 15/10 * 0.7 = 1.05
				"process.memory.utilization": 30.0, // 30/20 * 0.3 = 0.45
			},
			expected: 1.5, // 1.05 + 0.45 = 1.5
		},
		{
			name: "Some metrics below threshold",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization":    10.0,
					"process.memory.utilization": 20.0,
				},
				Weights: map[string]float64{
					"process.cpu.utilization":    0.6,
					"process.memory.utilization": 0.4,
				},
			},
			values: map[string]float64{
				"process.cpu.utilization":    5.0,  // 5/10 * 0.6 = 0.3
				"process.memory.utilization": 40.0, // 40/20 * 0.4 = 0.8
			},
			expected: 1.1, // 0.3 + 0.8 = 1.1
		},
		{
			name: "Missing metric values",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization":    10.0,
					"process.memory.utilization": 20.0,
				},
				Weights: map[string]float64{
					"process.cpu.utilization":    0.6,
					"process.memory.utilization": 0.4,
				},
			},
			values: map[string]float64{
				"process.cpu.utilization": 15.0, // 15/10 * 0.6 = 0.9
				// Missing process.memory.utilization
			},
			expected: 0.9, // Only CPU contributes
		},
		{
			name: "No matching metrics",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": 10.0,
				},
				Weights: map[string]float64{
					"process.cpu.utilization": 1.0,
				},
			},
			values: map[string]float64{
				"system.cpu.utilization": 20.0, // Not in thresholds
			},
			expected: 0, // No matching metrics
		},
	}
	
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := &processorImp{
				logger: logger,
				config: tc.config,
			}
			
			score, _ := p.calculateCompositeGeneric(tc.values)
			assert.InDelta(t, tc.expected, score, 0.001)
		})
	}
}