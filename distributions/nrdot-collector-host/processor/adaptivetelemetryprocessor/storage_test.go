package adaptivetelemetryprocessor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileStorage(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test_data", "test.db")

	// Test creating storage
	storage, err := NewFileStorage(filePath)
	require.NoError(t, err)
	assert.NotNil(t, storage)

	// Test loading from non-existent file (should return empty map)
	entities, err := storage.Load()
	require.NoError(t, err)
	assert.NotNil(t, entities)
	assert.Empty(t, entities)

	// Test saving entities
	testEntities := map[string]*TrackedEntity{
		"entity1": {
			Identity:      "entity1",
			FirstSeen:     time.Now(),
			LastExceeded:  time.Now(),
			CurrentValues: map[string]float64{"metric1": 10.5},
			MaxValues:     map[string]float64{"metric1": 10.5},
			Attributes:    map[string]string{"attr1": "value1"},
		},
		"entity2": {
			Identity:      "entity2",
			FirstSeen:     time.Now().Add(-1 * time.Hour),
			LastExceeded:  time.Now().Add(-30 * time.Minute),
			CurrentValues: map[string]float64{"metric1": 5.5, "metric2": 7.5},
			MaxValues:     map[string]float64{"metric1": 15.5, "metric2": 20.0},
			Attributes:    map[string]string{"attr1": "value2"},
		},
	}

	err = storage.Save(testEntities)
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(filePath)
	assert.NoError(t, err)

	// Test loading saved entities
	loadedEntities, err := storage.Load()
	require.NoError(t, err)
	assert.Len(t, loadedEntities, 2)

	// Check entity content was preserved
	for id, entity := range testEntities {
		loaded, exists := loadedEntities[id]
		assert.True(t, exists, "Entity %s not found in loaded entities", id)
		if exists {
			assert.Equal(t, entity.Identity, loaded.Identity)
			assert.Equal(t, entity.CurrentValues, loaded.CurrentValues)
			assert.Equal(t, entity.MaxValues, loaded.MaxValues)
			assert.Equal(t, entity.Attributes, loaded.Attributes)

			// Time fields should be close - check formatting and truncate to seconds
			assert.Equal(t, entity.FirstSeen.Truncate(time.Second), loaded.FirstSeen.Truncate(time.Second))
			assert.Equal(t, entity.LastExceeded.Truncate(time.Second), loaded.LastExceeded.Truncate(time.Second))
		}
	}

	// Test closing storage
	err = storage.Close()
	require.NoError(t, err)

	// Test creating directory when it doesn't exist
	nestedPath := filepath.Join(tmpDir, "nested", "deep", "test_data", "test.db")
	storage, err = NewFileStorage(nestedPath)
	require.NoError(t, err)

	// Save should create all directories
	err = storage.Save(testEntities)
	require.NoError(t, err)

	// Verify nested directory was created
	_, err = os.Stat(filepath.Dir(nestedPath))
	assert.NoError(t, err)
}

func TestCreateDirectoryIfNotExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Test creating a new directory
	newDir := filepath.Join(tmpDir, "new_dir")
	err := createDirectoryIfNotExists(newDir)
	require.NoError(t, err)

	// Verify directory was created
	info, err := os.Stat(newDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Test with existing directory (should not error)
	err = createDirectoryIfNotExists(newDir)
	require.NoError(t, err)

	// Test with nested directories
	nestedDir := filepath.Join(tmpDir, "nested", "deep", "dir")
	err = createDirectoryIfNotExists(nestedDir)
	require.NoError(t, err)

	// Verify nested directories were created
	info, err = os.Stat(nestedDir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}