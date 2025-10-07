package adaptivetelemetryprocessor

// This file contains all constant definitions for the adaptivetelemetryprocessor.

const (
	defaultStoragePath        = "/var/lib/nrdot-collector/adaptivetelemetry.db"
	dynamicSmoothingFactor    = 0.2
	dynamicUpdateIntervalSecs = 60
	genericScalingFactor      = 0.2

	// Attribute key denoting which filtering stage allowed the resource through
	// Allowed values: static_threshold | dynamic_threshold | multi_metric | anomaly_detection | retention
	adaptiveFilterStageAttributeKey = "adaptive.filter.stage"

	stageStaticThreshold           = "static_threshold"
	stageDynamicThreshold          = "dynamic_threshold"
	stageMultiMetric               = "multi_metric"
	stageAnomalyDetection          = "anomaly_detection"
	stageRetention                 = "retention"
	stageResourceProcessingTimeout = "resource_processing_timeout" // Used for all resource types during timeout

	// Hostmetrics resource types
	resourceTypeCPU        = "cpu"
	resourceTypeDisk       = "disk"
	resourceTypeFilesystem = "filesystem"
	resourceTypeLoad       = "load"
	resourceTypeMemory     = "memory"
	resourceTypeNetwork    = "network"
	resourceTypeProcess    = "process"
	resourceTypeProcesses  = "processes"
	resourceTypePaging     = "paging"
)
