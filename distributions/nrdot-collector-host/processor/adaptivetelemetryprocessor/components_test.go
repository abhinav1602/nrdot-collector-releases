package adaptivetelemetryprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComponents(t *testing.T) {
	// Call the Components function
	factories := Components()
	
	// Verify the result
	assert.Len(t, factories, 1, "Should return exactly one factory")
	
	// Verify the factory creates the expected config
	config := factories[0].CreateDefaultConfig()
	_, ok := config.(*Config)
	assert.True(t, ok, "Config should be of type *Config")
}