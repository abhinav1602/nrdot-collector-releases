// Copyright 2023 New Relic Corporation. All rights reserved.
// SPDX-License-Identifier: Apache-2.0

package adaptivetelemetryprocessor

import (
	"sync"
	"time"

	"go.opentelemetry.io/collector/consumer"
	"go.uber.org/zap"
)

// processorImp is the main implementation of the adaptive telemetry processor.
// This file now only contains the core structure definition. All implementation
// details have been moved to specialized files.
type processorImp struct {
	logger       *zap.Logger
	config       *Config
	nextConsumer consumer.Metrics

	trackedEntities    map[string]*TrackedEntity
	mu                 sync.RWMutex // protects trackedEntities & dynamicCustomThresholds
	storage            EntityStateStorage
	lastPersistenceOp  time.Time
	persistenceEnabled bool

	lastThresholdUpdate      time.Time
	lastInfoLogTime          time.Time // Tracks when we last logged at INFO level
	dynamicThresholdsEnabled bool
	multiMetricEnabled       bool

	// Dynamic thresholds for metrics (including cpu/memory if configured)
	dynamicCustomThresholds map[string]float64
}
