package adaptivetelemetryprocessor

import (
"context"
"errors"
"testing"
"time"

"github.com/stretchr/testify/assert"
"go.uber.org/zap/zaptest"
)

// Mock storage for testing persistence operations
type mockStorage struct {
	loadCalled  bool
	saveCalled  bool
	closeCalled bool
	loadError   error
	saveError   error
	closeError  error
	entities    map[string]*TrackedEntity
}

func (m *mockStorage) Load() (map[string]*TrackedEntity, error) {
	m.loadCalled = true
	if m.loadError != nil {
		return nil, m.loadError
	}
	return m.entities, nil
}

func (m *mockStorage) Save(entities map[string]*TrackedEntity) error {
	m.saveCalled = true
	if m.saveError != nil {
		return m.saveError
	}
	m.entities = entities
	return nil
}

func (m *mockStorage) Close() error {
	m.closeCalled = true
	return m.closeError
}

func TestPersistTrackedEntities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	testCases := []struct {
		name          string
		setupStorage  func() *mockStorage
		setupEntities func() map[string]*TrackedEntity
		expectedError bool
	}{
		{
			name: "Successful persistence",
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities: make(map[string]*TrackedEntity),
				}
			},
			setupEntities: func() map[string]*TrackedEntity {
				return map[string]*TrackedEntity{
					"entity1": {
						Identity:      "entity1",
						FirstSeen:     time.Now().Add(-time.Hour),
						LastExceeded:  time.Now().Add(-30 * time.Minute),
						CurrentValues: map[string]float64{"cpu": 80.0},
					},
					"entity2": {
						Identity:      "entity2",
						FirstSeen:     time.Now().Add(-2 * time.Hour),
						LastExceeded:  time.Now().Add(-time.Hour),
						CurrentValues: map[string]float64{"memory": 90.0},
					},
				}
			},
			expectedError: false,
		},
		{
			name: "Storage is nil",
			setupStorage: func() *mockStorage {
				return nil
			},
			setupEntities: func() map[string]*TrackedEntity {
				return map[string]*TrackedEntity{
					"entity1": {
						Identity: "entity1",
					},
				}
			},
			expectedError: false, // Should not error, just log a warning
		},
		{
			name: "Save error",
			setupStorage: func() *mockStorage {
				return &mockStorage{
					saveError: errors.New("mock save error"),
					entities:  make(map[string]*TrackedEntity),
				}
			},
			setupEntities: func() map[string]*TrackedEntity {
				return map[string]*TrackedEntity{
					"entity1": {
						Identity: "entity1",
					},
				}
			},
			expectedError: true,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
// Create processor with mock storage
storage := tc.setupStorage()
			proc := &processorImp{
				logger:            logger,
				config:            &Config{StoragePath: "/tmp/test_data/test.db"},
				trackedEntities:   tc.setupEntities(),
				persistenceEnabled: storage != nil,
				storage:           storage,
			}
			
			// Call persistTrackedEntities
			err := proc.persistTrackedEntities()
			
			// Check error
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			
			// Check if storage methods were called appropriately - only if storage is not nil
			if storage != nil {
				assert.True(t, storage.saveCalled, "Save should be called")
				if !tc.expectedError {
					assert.Equal(t, proc.trackedEntities, storage.entities, "Entities should be saved to storage")
				}
			}
		})
	}
}

func TestLoadTrackedEntities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	testCases := []struct {
		name          string
		setupStorage  func() *mockStorage
		expectedError bool
		expectedLen   int
	}{
		{
			name: "Successful load",
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities: map[string]*TrackedEntity{
						"entity1": {
							Identity:      "entity1",
							FirstSeen:     time.Now().Add(-time.Hour),
							LastExceeded:  time.Now().Add(-30 * time.Minute),
							CurrentValues: map[string]float64{"cpu": 80.0},
							Attributes:    map[string]string{"type": "host"},
						},
						"entity2": {
							Identity:      "entity2",
							FirstSeen:     time.Now().Add(-2 * time.Hour),
							LastExceeded:  time.Now().Add(-time.Hour),
							CurrentValues: map[string]float64{"memory": 90.0},
							Attributes:    map[string]string{"type": "container"},
						},
					},
				}
			},
			expectedError: false,
			expectedLen:   2,
		},
		{
			name: "Storage is nil",
			setupStorage: func() *mockStorage {
				return nil
			},
			expectedError: false, // Should not error, just log a warning
			expectedLen:   0,
		},
		{
			name: "Load error",
			setupStorage: func() *mockStorage {
				return &mockStorage{
					loadError: errors.New("mock load error"),
				}
			},
			expectedError: true,
			expectedLen:   0,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
// Create processor with mock storage
storage := tc.setupStorage()
			proc := &processorImp{
				logger:            logger,
				config:            &Config{StoragePath: "/tmp/test_data/test.db"},
				trackedEntities:   make(map[string]*TrackedEntity),
				persistenceEnabled: storage != nil,
				storage:           storage,
			}
			
			// Call loadTrackedEntities
			err := proc.loadTrackedEntities()
			
			// Check error
			if tc.expectedError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
			
			// Check if storage methods were called appropriately
			if storage != nil {
				assert.True(t, storage.loadCalled, "Load should be called")
				if !tc.expectedError {
					assert.Equal(t, tc.expectedLen, len(proc.trackedEntities), "Correct number of entities should be loaded")
				}
			}
		})
	}
}

func TestCleanupExpiredEntities(t *testing.T) {
	logger := zaptest.NewLogger(t)
	
	// Create test entities with different expiration times
	now := time.Now()
	entities := map[string]*TrackedEntity{
		"recent": {
			Identity:     "recent",
			FirstSeen:    now.Add(-time.Hour),
			LastExceeded: now.Add(-5 * time.Minute), // Recent, should not be removed
		},
		"old": {
			Identity:     "old",
			FirstSeen:    now.Add(-3 * time.Hour),
			LastExceeded: now.Add(-70 * time.Minute), // Old, should be removed
		},
		"very_old": {
			Identity:     "very_old",
			FirstSeen:    now.Add(-24 * time.Hour),
			LastExceeded: now.Add(-120 * time.Minute), // Very old, should be removed
		},
	}
	
	// Create processor
	proc := &processorImp{
		logger:          logger,
		config:          &Config{RetentionMinutes: 60}, // 60 minute retention
		trackedEntities: entities,
	}
	
	// Call cleanup
	proc.cleanupExpiredEntities()
	
	// Check results
	assert.Equal(t, 1, len(proc.trackedEntities), "Should have 1 entity remaining")
	assert.Contains(t, proc.trackedEntities, "recent", "Recent entity should still be present")
	assert.NotContains(t, proc.trackedEntities, "old", "Old entity should be removed")
	assert.NotContains(t, proc.trackedEntities, "very_old", "Very old entity should be removed")
}

func TestShutdown(t *testing.T) {
	logger := zaptest.NewLogger(t)
	ctx := context.Background()
	
	testCases := []struct {
		name              string
		persistenceEnabled bool
		setupStorage      func() *mockStorage
		expectSave        bool
		expectClose       bool
		saveError         error
		closeError        error
	}{
		{
			name:              "Normal shutdown with persistence",
			persistenceEnabled: true,
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities: make(map[string]*TrackedEntity),
				}
			},
			expectSave:  true,
			expectClose: true,
		},
		{
			name:              "Shutdown with persistence disabled",
			persistenceEnabled: false,
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities: make(map[string]*TrackedEntity),
				}
			},
			expectSave:  false,
			expectClose: false,
		},
		{
			name:              "Shutdown with save error",
			persistenceEnabled: true,
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities:  make(map[string]*TrackedEntity),
					saveError: errors.New("mock save error"),
				}
			},
			expectSave:  true,
			expectClose: true,
			saveError:   errors.New("mock save error"),
		},
		{
			name:              "Shutdown with close error",
			persistenceEnabled: true,
			setupStorage: func() *mockStorage {
				return &mockStorage{
					entities:   make(map[string]*TrackedEntity),
					closeError: errors.New("mock close error"),
				}
			},
			expectSave:  true,
			expectClose: true,
			closeError:  errors.New("mock close error"),
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
// Create processor with mock storage
storage := tc.setupStorage()
			proc := &processorImp{
				logger:            logger,
				config:            &Config{StoragePath: "/tmp/test_data/test.db"},
				trackedEntities:   map[string]*TrackedEntity{"entity1": {Identity: "entity1"}},
				persistenceEnabled: tc.persistenceEnabled,
				storage:           storage,
			}
			
			// Call Shutdown
			err := proc.Shutdown(ctx)
			
			// Shutdown should never return an error
			assert.NoError(t, err)
			
			// Check if storage methods were called appropriately
			if tc.persistenceEnabled && storage != nil {
				assert.Equal(t, tc.expectSave, storage.saveCalled, "Save should be called")
				assert.Equal(t, tc.expectClose, storage.closeCalled, "Close should be called")
			} else if storage != nil {
				assert.False(t, storage.saveCalled, "Save should not be called when persistence is disabled")
				assert.False(t, storage.closeCalled, "Close should not be called when persistence is disabled")
			}
		})
	}
}
