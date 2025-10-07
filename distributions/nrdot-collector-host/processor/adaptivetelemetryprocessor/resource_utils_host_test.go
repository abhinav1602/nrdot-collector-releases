package adaptivetelemetryprocessor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func TestIdentifyHostMetricType(t *testing.T) {
	testCases := []struct {
		name           string
		attributes     map[string]string
		expectedType   string
		expectIdentified bool
	}{
		{
			name: "CPU with state idle",
			attributes: map[string]string{
				"cpu":   "0",
				"state": "idle",
			},
			expectedType:     resourceTypeCPU,
			expectIdentified: true,
		},
		{
			name: "CPU with state user",
			attributes: map[string]string{
				"cpu":   "1",
				"state": "user",
			},
			expectedType:     resourceTypeCPU,
			expectIdentified: true,
		},
		{
			name: "CPU with no state",
			attributes: map[string]string{
				"cpu": "2",
			},
			expectedType:     resourceTypeCPU,
			expectIdentified: true,
		},
		{
			name: "Disk with read direction",
			attributes: map[string]string{
				"device":    "sda",
				"direction": "read",
			},
			expectedType:     resourceTypeDisk,
			expectIdentified: true,
		},
		{
			name: "Disk with write direction",
			attributes: map[string]string{
				"device":    "sda",
				"direction": "write",
			},
			expectedType:     resourceTypeDisk,
			expectIdentified: true,
		},
		{
			name: "Filesystem with mountpoint",
			attributes: map[string]string{
				"mountpoint": "/",
				"device":     "sda1",
			},
			expectedType:     resourceTypeFilesystem,
			expectIdentified: true,
		},
		{
			name: "Filesystem with state",
			attributes: map[string]string{
				"type":  "ext4",
				"state": "free",
			},
			expectedType:     resourceTypeFilesystem,
			expectIdentified: true,
		},
		{
			name: "Memory with state",
			attributes: map[string]string{
				"state": "used",
			},
			expectedType:     resourceTypeMemory,
			expectIdentified: true,
		},
		{
			name: "Memory with cached state",
			attributes: map[string]string{
				"state": "cached",
			},
			expectedType:     resourceTypeMemory,
			expectIdentified: true,
		},
		{
			name: "Network with device and direction",
			attributes: map[string]string{
				"device":    "eth0",
				"direction": "receive",
			},
			expectedType:     resourceTypeNetwork,
			expectIdentified: true,
		},
		{
			name: "Network with protocol",
			attributes: map[string]string{
				"device":   "eth0",
				"protocol": "tcp",
			},
			expectedType:     resourceTypeNetwork,
			expectIdentified: true,
		},
		{
			name: "Paging with direction",
			attributes: map[string]string{
				"direction": "page_in",
			},
			expectedType:     resourceTypePaging,
			expectIdentified: true,
		},
		{
			name: "Paging with type",
			attributes: map[string]string{
				"type": "major",
			},
			expectedType:     resourceTypePaging,
			expectIdentified: true,
		},
		{
			name: "Processes with status",
			attributes: map[string]string{
				"status": "running",
			},
			expectedType:     resourceTypeProcesses,
			expectIdentified: true,
		},
		{
			name: "Processes with stopped status",
			attributes: map[string]string{
				"status": "stopped",
			},
			expectedType:     resourceTypeProcesses,
			expectIdentified: true,
		},
		{
			name: "Unknown attributes",
			attributes: map[string]string{
				"unknown": "value",
			},
			expectedType:     "",
			expectIdentified: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create resource attributes
			attrs := pcommon.NewMap()
			for key, val := range tc.attributes {
				attrs.PutStr(key, val)
			}

			// Call the function
			identifiedType, identified := identifyHostMetricType(attrs)

			// Verify results
			assert.Equal(t, tc.expectIdentified, identified, "Identification status mismatch")
			if tc.expectIdentified {
				assert.Equal(t, tc.expectedType, identifiedType, "Identified type mismatch")
			}
		})
	}
}