package adaptivetelemetryprocessor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type EntityStateStorage interface {
	Load() (map[string]*TrackedEntity, error)

	Save(map[string]*TrackedEntity) error

	Close() error
}

type FileStorage struct {
	filePath string
	mu       sync.Mutex
}

func NewFileStorage(filePath string) (*FileStorage, error) {
	return &FileStorage{
		filePath: filePath,
	}, nil
}

func (s *FileStorage) Load() (map[string]*TrackedEntity, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := os.Stat(s.filePath); os.IsNotExist(err) {
		// Return empty map if file doesn't exist yet
		return make(map[string]*TrackedEntity), nil
	}

	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return nil, err
	}

	var entities map[string]*TrackedEntity
	if err := json.Unmarshal(data, &entities); err != nil {
		return nil, err
	}

	return entities, nil
}

func (s *FileStorage) Save(entities map[string]*TrackedEntity) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure directory exists
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(entities, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.filePath, data, 0644)
}

func (s *FileStorage) Close() error {
	// No cleanup needed for file storage
	return nil
}

// createDirectoryIfNotExists creates a directory if it doesn't exist
func createDirectoryIfNotExists(dirPath string) error {
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		return os.MkdirAll(dirPath, 0755)
	}
	return nil
}
