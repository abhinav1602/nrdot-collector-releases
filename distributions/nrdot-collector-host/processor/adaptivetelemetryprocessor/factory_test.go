package adaptivetelemetryprocessor

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/consumer/consumertest"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap/zaptest"
)

// mockMetricsConsumer implements consumer.Metrics for testing
type mockMetricsConsumer struct {
	metrics []pmetric.Metrics
}

func newMockMetricsConsumer() *mockMetricsConsumer {
	return &mockMetricsConsumer{
		metrics: make([]pmetric.Metrics, 0),
	}
}

func (m *mockMetricsConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (m *mockMetricsConsumer) ConsumeMetrics(_ context.Context, md pmetric.Metrics) error {
	metricsCopy := pmetric.NewMetrics()
	md.CopyTo(metricsCopy)
	m.metrics = append(m.metrics, metricsCopy)
	return nil
}

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	assert.Equal(t, component.MustNewType(typeStr), factory.Type())
}

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()
	assert.NotNil(t, cfg)
	assert.IsType(t, &Config{}, cfg)

	config := cfg.(*Config)
	assert.Equal(t, factoryDefaultStoragePath, config.StoragePath)
	assert.Equal(t, factoryDefaultCompositeThreshold, config.CompositeThreshold)
	assert.Equal(t, map[string]float64{}, config.MetricThresholds)
	assert.Equal(t, map[string]float64{}, config.Weights)
	assert.Equal(t, int64(30), config.RetentionMinutes)
	assert.Equal(t, false, config.EnableDynamicThresholds)
	assert.Equal(t, false, config.EnableMultiMetric)
	assert.Equal(t, float64(0.2), config.DynamicSmoothingFactor)
	assert.Equal(t, map[string]float64{}, config.MinThresholds)
	assert.Equal(t, map[string]float64{}, config.MaxThresholds)
	assert.Equal(t, false, config.EnableAnomalyDetection)
	assert.Equal(t, 10, config.AnomalyHistorySize)
	assert.Equal(t, float64(200.0), config.AnomalyChangeThreshold)
}

func TestCreateProcessor(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockConsumer := newMockMetricsConsumer()

	tests := []struct {
		name          string
		config        *Config
		errorExpected bool
	}{
		{
			name: "Valid config",
			config: &Config{
				MetricThresholds: map[string]float64{},
				StoragePath:      "./test_data/test.db",
				RetentionMinutes: 30,
			},
			errorExpected: false,
		},
		{
			name: "Valid config with options",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": 5.0,
				},
				StoragePath:      "./test_data/test.db",
				RetentionMinutes: 20,
			},
			errorExpected: false,
		},
		{
			name: "Invalid config with negative threshold",
			config: &Config{
				MetricThresholds: map[string]float64{
					"process.cpu.utilization": -5.0,
				},
			},
			errorExpected: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.config.Normalize()
			proc, err := newProcessor(logger, test.config, mockConsumer)

			if test.errorExpected {
				require.Error(t, err)
				assert.Nil(t, proc)
			} else {
				require.NoError(t, err)
				assert.NotNil(t, proc)
			}
		})
	}
}

func TestCapabilities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockConsumer := newMockMetricsConsumer()
	
	config := &Config{
		MetricThresholds: map[string]float64{},
		StoragePath:      "./test_data/test.db",
		RetentionMinutes: 30,
	}
	
	proc, err := newProcessor(logger, config, mockConsumer)
	require.NoError(t, err)
	
	caps := proc.Capabilities()
	assert.True(t, caps.MutatesData)
}

func TestStartShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mockConsumer := newMockMetricsConsumer()
	
	config := &Config{
		MetricThresholds: map[string]float64{},
		StoragePath:      "./test_data/test.db",
		RetentionMinutes: 30,
	}
	
	proc, err := newProcessor(logger, config, mockConsumer)
	require.NoError(t, err)
	
	// Start should succeed
	err = proc.Start(context.Background(), nil)
	require.NoError(t, err)
	
	// Shutdown should succeed
	err = proc.Shutdown(context.Background())
	require.NoError(t, err)
}

// Extended tests for factory and metrics processor creation

func TestCreateMetricsProcessorWithFactoryExtended(t *testing.T) {
	// Create a factory and test consumer
	factory := NewFactory()
	nextConsumer := consumertest.NewNop()
	
	// Test scenarios
	testCases := []struct {
		name          string
		configModify  func(*Config)
		expectSuccess bool
	}{
		{
			name: "Default config",
			configModify: func(c *Config) {
				// Use default config
			},
			expectSuccess: true,
		},
		{
			name: "Custom metric thresholds",
			configModify: func(c *Config) {
				c.MetricThresholds = map[string]float64{
					"system.cpu.utilization": 80.0,
					"process.cpu.utilization": 10.0,
				}
			},
			expectSuccess: true,
		},
		{
			name: "Invalid config - negative threshold",
			configModify: func(c *Config) {
				c.MetricThresholds = map[string]float64{
					"system.cpu.utilization": -10.0,
				}
			},
			expectSuccess: false,
		},
		{
			name: "Dynamic thresholds enabled",
			configModify: func(c *Config) {
				c.EnableDynamicThresholds = true
				c.DynamicSmoothingFactor = 0.3
				c.MetricThresholds = map[string]float64{
					"system.cpu.utilization": 80.0,
				}
			},
			expectSuccess: true,
		},
		{
			name: "Multi-metric evaluation enabled",
			configModify: func(c *Config) {
				c.EnableMultiMetric = true
				c.CompositeThreshold = 1.2
				c.MetricThresholds = map[string]float64{
					"system.cpu.utilization": 80.0,
					"system.memory.utilization": 70.0,
				}
				c.Weights = map[string]float64{
					"system.cpu.utilization": 0.7,
					"system.memory.utilization": 0.3,
				}
			},
			expectSuccess: true,
		},
		{
			name: "Anomaly detection enabled",
			configModify: func(c *Config) {
				c.EnableAnomalyDetection = true
				c.AnomalyHistorySize = 15
				c.AnomalyChangeThreshold = 150.0
			},
			expectSuccess: true,
		},
	}
	
	// Context for creating processors
	ctx := context.Background()
	settings := processor.Settings{
		TelemetrySettings: component.TelemetrySettings{
			Logger: zaptest.NewLogger(t),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a config and modify it for the test case
			config := factory.CreateDefaultConfig().(*Config)
			tc.configModify(config)
			
			// Create processor
			proc, err := createMetricsProcessor(ctx, settings, config, nextConsumer)
			
			// Check expectations
			if tc.expectSuccess {
				require.NoError(t, err)
				assert.NotNil(t, proc)
				
				// Verify processor implements required interfaces
				_, ok := proc.(processor.Metrics)
				assert.True(t, ok, "Processor should implement processor.Metrics")
			} else {
				require.Error(t, err)
				assert.Nil(t, proc)
			}
		})
	}
}