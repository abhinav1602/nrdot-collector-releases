package adaptivetelemetryprocessor

import (
	"time"
)

// TrackedEntityUtil represents an entity being tracked for telemetry filtering
type TrackedEntityUtil struct {
	Identity      string             `json:"identity"`
	FirstSeen     time.Time          `json:"first_seen"`
	LastExceeded  time.Time          `json:"last_exceeded"`
	CurrentValues map[string]float64 `json:"current_values"`
	MaxValues     map[string]float64 `json:"max_values"`
	Attributes    map[string]string  `json:"attributes,omitempty"`

	// Anomaly detection fields - historical metric values
	MetricHistory       map[string][]float64 `json:"metric_history,omitempty"`
	LastAnomalyDetected time.Time            `json:"last_anomaly_detected,omitempty"`
}
