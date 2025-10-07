package adaptivetelemetryprocessor

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
)

func TestMin(t *testing.T) {
	testCases := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{
			name:     "a is smaller than b",
			a:        5,
			b:        10,
			expected: 5,
		},
		{
			name:     "b is smaller than a",
			a:        15,
			b:        7,
			expected: 7,
		},
		{
			name:     "a equals b",
			a:        42,
			b:        42,
			expected: 42,
		},
		{
			name:     "negative numbers, a smaller",
			a:        -10,
			b:        -5,
			expected: -10,
		},
		{
			name:     "negative numbers, b smaller",
			a:        -5,
			b:        -15,
			expected: -15,
		},
		{
			name:     "zero and positive",
			a:        0,
			b:        5,
			expected: 0,
		},
		{
			name:     "zero and negative",
			a:        0,
			b:        -5,
			expected: -5,
		},
	}
	
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := min(tc.a, tc.b)
			assert.Equal(t, tc.expected, result)
		})
	}
}