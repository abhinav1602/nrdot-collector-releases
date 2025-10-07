package resourcethresholdprocessor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/processor"
	"go.uber.org/zap"
)

const (
	defaultCPUThresholdPercent    = 5.0
	defaultMemoryThresholdPercent = 10.0
	defaultRetentionMinutes       = 60
	defaultStoragePath            = "/var/lib/nrdot-collector/topnprocess.db"
)

const DEBUG_FILTER = true

// TrackedProcess captures metadata and utilization stats of a process while it exceeds thresholds
type TrackedProcess struct {
	PID                 int                `json:"pid"`
	Name                string             `json:"name"`
	CommandLine         string             `json:"command_line"`
	Owner               string             `json:"owner"`
	FirstSeen           time.Time          `json:"first_seen"`
	LastExceeded        time.Time          `json:"last_exceeded"`
	CurrentMetricValues map[string]float64 `json:"current_metric_values"`
	MaxMetricValues     map[string]float64 `json:"max_metric_values"`
}

type processorImp struct {
	logger                   *zap.Logger
	config                   *Config
	nextConsumer             consumer.Metrics
	trackedProcesses         map[int]*TrackedProcess
	mu                       sync.RWMutex
	storage                  ProcessStateStorage
	lastPersistenceOp        time.Time
	persistenceEnabled       bool
	systemCPUUtilization     float64
	systemMemoryUtilization  float64
	lastThresholdUpdate      time.Time
	dynamicThresholdsEnabled bool
}

func newProcessor(logger *zap.Logger, config *Config, nextConsumer consumer.Metrics) (*processorImp, error) {
	p := &processorImp{
		logger:                   logger,
		config:                   config,
		nextConsumer:             nextConsumer,
		trackedProcesses:         make(map[int]*TrackedProcess),
		persistenceEnabled:       config.StoragePath != "",
		systemCPUUtilization:     0.0,
		systemMemoryUtilization:  0.0,
		dynamicThresholdsEnabled: false, // config.EnableDynamicThresholds
		lastThresholdUpdate:      time.Now(),
	}

	if p.persistenceEnabled {
		storageDir := filepath.Dir(config.StoragePath)
		if err := createDirectoryIfNotExists(storageDir); err != nil {
			logger.Warn("Failed to create storage directory", zap.String("path", storageDir), zap.Error(err))
			p.persistenceEnabled = false
		} else {
			storage, err := NewFileStorage(config.StoragePath)
			if err != nil {
				logger.Warn("Failed to initialize storage", zap.Error(err))
				p.persistenceEnabled = false
			} else {
				p.storage = storage
				if err := p.loadTrackedProcesses(); err != nil {
					logger.Warn("Failed to load tracked processes", zap.Error(err))
				}
			}
		}
	}

	return p, nil
}

// helper to deep copy a map[string]float64
func copyFloatMap(src map[string]float64) map[string]float64 {
	if src == nil {
		return make(map[string]float64)
	}
	dst := make(map[string]float64, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func (p *processorImp) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	filteredMetrics, err := p.processMetrics(ctx, md)
	if err != nil {
		return err
	}

	if p.persistenceEnabled && time.Since(p.lastPersistenceOp) > time.Minute {
		if err := p.persistTrackedProcesses(); err != nil {
			p.logger.Warn("Failed to persist tracked processes", zap.Error(err))
		}
		p.lastPersistenceOp = time.Now()
	}

	return p.nextConsumer.ConsumeMetrics(ctx, filteredMetrics)
}

func (p *processorImp) processMetrics(ctx context.Context, md pmetric.Metrics) (pmetric.Metrics, error) {
	filteredMetrics := pmetric.NewMetrics()

	resourceMetrics := md.ResourceMetrics()
	for i := 0; i < resourceMetrics.Len(); i++ {
		resourceMetric := resourceMetrics.At(i)

		resource := resourceMetric.Resource()
		pid, hasPID := getProcessPID(resource)

		if !hasPID {
			resourceMetric.CopyTo(filteredMetrics.ResourceMetrics().AppendEmpty())
			continue
		}

		if p.shouldIncludeProcess(resource, resourceMetric, pid) {
			resourceMetric.CopyTo(filteredMetrics.ResourceMetrics().AppendEmpty())
		}
	}

	p.cleanupExpiredProcesses()

	return filteredMetrics, nil
}

func (p *processorImp) shouldIncludeProcess(resource pcommon.Resource, rm pmetric.ResourceMetrics, pid int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	metricValues := p.extractMetricValues(rm)
	if len(metricValues) == 0 {
		// No metrics relevant to our thresholds were found.
		// If the process is already tracked, we keep it until it expires.
		_, exists := p.trackedProcesses[pid]
		return exists
	}

	trackedProcess, exists := p.trackedProcesses[pid]

	exceeds := false
	var reasons []string
	for metricName, threshold := range p.config.MetricThresholds {
		if currentValue, ok := metricValues[metricName]; ok {
			if currentValue >= threshold {
				exceeds = true
				reasons = append(reasons, fmt.Sprintf("%s %.1f >= %.1f", metricName, currentValue, threshold))
			}
		}
	}

	if exists {
		if trackedProcess.CurrentMetricValues == nil {
			trackedProcess.CurrentMetricValues = make(map[string]float64)
		}
		if trackedProcess.MaxMetricValues == nil {
			trackedProcess.MaxMetricValues = make(map[string]float64)
		}
		trackedProcess.CurrentMetricValues = metricValues
		for name, value := range metricValues {
			if existingMax, ok := trackedProcess.MaxMetricValues[name]; !ok || value > existingMax {
				trackedProcess.MaxMetricValues[name] = value
			}
		}

		if exceeds {
			trackedProcess.LastExceeded = time.Now()
			p.logger.Debug("Tracked process exceeded threshold", zap.Int("pid", pid), zap.Strings("reasons", reasons))
		}
		return true // Always include already tracked processes until they expire
	}

	if exceeds {
		processNameVal, _ := resource.Attributes().Get("process.executable.name")
		commandLineVal, _ := resource.Attributes().Get("process.command_line")
		ownerVal, _ := resource.Attributes().Get("process.owner")

		now := time.Now()
		p.trackedProcesses[pid] = &TrackedProcess{
			PID:                 pid,
			Name:                processNameVal.AsString(),
			CommandLine:         commandLineVal.AsString(),
			Owner:               ownerVal.AsString(),
			FirstSeen:           now,
			LastExceeded:        now,
			CurrentMetricValues: copyFloatMap(metricValues),
			MaxMetricValues:     copyFloatMap(metricValues),
		}
		p.logger.Info("Started tracking new process", zap.Int("pid", pid), zap.String("name", processNameVal.AsString()), zap.Strings("reasons", reasons))
		return true
	}

	return false
}

func (p *processorImp) extractMetricValues(rm pmetric.ResourceMetrics) map[string]float64 {
	values := make(map[string]float64)

	for i := 0; i < rm.ScopeMetrics().Len(); i++ {
		scopeMetrics := rm.ScopeMetrics().At(i)
		for j := 0; j < scopeMetrics.Metrics().Len(); j++ {
			metric := scopeMetrics.Metrics().At(j)

			// Only consider metrics that are configured with a threshold
			if _, configured := p.config.MetricThresholds[metric.Name()]; !configured {
				continue
			}

			if metric.Type() == pmetric.MetricTypeGauge && metric.Gauge().DataPoints().Len() > 0 {
				// This logic assumes a ratio [0,1] and converts to percent.
				// It sums data points for multi-datapoint gauges; consider averaging if more appropriate.
				var value float64
				for k := 0; k < metric.Gauge().DataPoints().Len(); k++ {
					value += metric.Gauge().DataPoints().At(k).DoubleValue()
				}
				values[metric.Name()] = value * 100
			}
		}
	}
	return values
}

func (p *processorImp) cleanupExpiredProcesses() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if DEBUG_FILTER {
		_, _ = fmt.Fprintf(os.Stderr, "\n[DEBUG] Checking for expired processes among %d tracked processes\n", len(p.trackedProcesses))
	}

	expirationTime := time.Now().Add(-time.Duration(p.config.RetentionMinutes) * time.Minute)

	for pid, process := range p.trackedProcesses {
		if process.LastExceeded.Before(expirationTime) {
			timeSinceExceeded := time.Since(process.LastExceeded).Seconds()

			debugMsg := fmt.Sprintf("Removing expired process from tracking: PID %d, Name: %s, Last exceeded %.1f seconds ago (retention period: %d minutes)",
				pid, process.Name, timeSinceExceeded, p.config.RetentionMinutes)
			p.logger.Info(debugMsg)

			if DEBUG_FILTER {
				_, _ = fmt.Fprintln(os.Stderr, "[DEBUG] "+debugMsg)
			}

			delete(p.trackedProcesses, pid)
		} else if DEBUG_FILTER {
			timeSinceExceeded := time.Since(process.LastExceeded).Seconds()
			_, _ = fmt.Fprintf(os.Stderr, "[DEBUG] PID %d (%s) still within retention period: last exceeded %.1f seconds ago (retention: %d minutes)\n",
				pid, process.Name, timeSinceExceeded, p.config.RetentionMinutes)
		}
	}
}

func getProcessPID(resource pcommon.Resource) (int, bool) {
	pidAttr, exists := resource.Attributes().Get("process.pid")
	if !exists {
		return 0, false
	}

	// Check that the attribute is of the integer type
	if pidAttr.Type() != pcommon.ValueTypeInt {
		return 0, false
	}

	return int(pidAttr.Int()), true
}

func (p *processorImp) loadTrackedProcesses() error {
	if p.storage == nil {
		return nil
	}

	processes, err := p.storage.Load()
	if err != nil {
		return err
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.trackedProcesses = processes
	// ensure maps initialized to avoid nil map assignment panics
	for _, tp := range p.trackedProcesses {
		if tp.CurrentMetricValues == nil {
			tp.CurrentMetricValues = make(map[string]float64)
		}
		if tp.MaxMetricValues == nil {
			tp.MaxMetricValues = make(map[string]float64)
		}
	}
	p.logger.Info("Loaded tracked processes from storage", zap.Int("count", len(processes)))

	return nil
}

func (p *processorImp) persistTrackedProcesses() error {
	if p.storage == nil {
		return nil
	}

	p.mu.RLock()
	defer p.mu.RUnlock()

	err := p.storage.Save(p.trackedProcesses)
	if err != nil {
		return err
	}

	p.logger.Debug("Persisted tracked processes to storage", zap.Int("count", len(p.trackedProcesses)))
	return nil
}

func (p *processorImp) Shutdown(ctx context.Context) error {
	if p.persistenceEnabled && p.storage != nil {
		if err := p.persistTrackedProcesses(); err != nil {
			p.logger.Warn("Failed to persist tracked processes during shutdown", zap.Error(err))
		}

		if err := p.storage.Close(); err != nil {
			p.logger.Warn("Failed to close storage during shutdown", zap.Error(err))
		}
	}
	return nil
}

func (p *processorImp) Start(_ context.Context, _ component.Host) error {
	return nil
}

func (p *processorImp) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: true}
}

var _ processor.Metrics = (*processorImp)(nil)
