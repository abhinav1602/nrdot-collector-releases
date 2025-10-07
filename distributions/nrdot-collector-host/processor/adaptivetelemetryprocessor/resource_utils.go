package adaptivetelemetryprocessor

import (
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

// identifyHostMetricType tries to identify the type of host metric from attributes
func identifyHostMetricType(attrs pcommon.Map) (string, bool) {
	// Check for specific resource attributes that identify different hostmetric types

	// CPU - look for cpu and state attributes
	if _, hasCPU := attrs.Get("cpu"); hasCPU {
		if stateVal, hasState := attrs.Get("state"); hasState && stateVal.Type() == pcommon.ValueTypeStr {
			state := stateVal.AsString()
			if state == "idle" || state == "interrupt" || state == "nice" ||
				state == "softirq" || state == "steal" || state == "system" ||
				state == "user" || state == "wait" {
				return resourceTypeCPU, true
			}
		}
		// Even without state, if cpu attribute exists, it's likely CPU metric
		return resourceTypeCPU, true
	}

	// Disk - look for device and direction attributes
	if deviceVal, hasDevice := attrs.Get("device"); hasDevice && deviceVal.Type() == pcommon.ValueTypeStr {
		if dirVal, hasDir := attrs.Get("direction"); hasDir && dirVal.Type() == pcommon.ValueTypeStr {
			dir := dirVal.AsString()
			if dir == "read" || dir == "write" {
				return resourceTypeDisk, true
			}
		}
	}

	// Filesystem - look for mountpoint, device, type, state attributes
	if _, hasMountpoint := attrs.Get("mountpoint"); hasMountpoint {
		return resourceTypeFilesystem, true
	}
	if _, hasType := attrs.Get("type"); hasType {
		if stateVal, hasState := attrs.Get("state"); hasState && stateVal.Type() == pcommon.ValueTypeStr {
			state := stateVal.AsString()
			if state == "free" || state == "reserved" || state == "used" {
				return resourceTypeFilesystem, true
			}
		}
	}

	// Memory - look for state attribute with memory-specific values
	if stateVal, hasState := attrs.Get("state"); hasState && stateVal.Type() == pcommon.ValueTypeStr {
		state := stateVal.AsString()
		if state == "buffered" || state == "cached" || state == "inactive" ||
			state == "free" || state == "slab_reclaimable" || state == "slab_unreclaimable" ||
			state == "used" {
			return resourceTypeMemory, true
		}
	}

	// Network - look for device, direction, protocol attributes
	if _, hasDevice := attrs.Get("device"); hasDevice {
		if dirVal, hasDir := attrs.Get("direction"); hasDir && dirVal.Type() == pcommon.ValueTypeStr {
			dir := dirVal.AsString()
			if dir == "receive" || dir == "transmit" {
				return resourceTypeNetwork, true
			}
		}
		if _, hasProtocol := attrs.Get("protocol"); hasProtocol {
			return resourceTypeNetwork, true
		}
		if stateVal, hasState := attrs.Get("state"); hasState && stateVal.Type() == pcommon.ValueTypeStr {
			return resourceTypeNetwork, true
		}
	}

	// Paging - look for direction, state, type attributes
	if dirVal, hasDir := attrs.Get("direction"); hasDir && dirVal.Type() == pcommon.ValueTypeStr {
		dir := dirVal.AsString()
		if dir == "page_in" || dir == "page_out" {
			return resourceTypePaging, true
		}
	}
	if stateVal, hasState := attrs.Get("state"); hasState && stateVal.Type() == pcommon.ValueTypeStr {
		state := stateVal.AsString()
		if state == "cached" || state == "free" || state == "used" {
			// Look for device to distinguish from memory
			if _, hasDevice := attrs.Get("device"); hasDevice {
				return resourceTypePaging, true
			}
		}
	}
	if typeVal, hasType := attrs.Get("type"); hasType && typeVal.Type() == pcommon.ValueTypeStr {
		typ := typeVal.AsString()
		if typ == "major" || typ == "minor" {
			return resourceTypePaging, true
		}
	}

	// Processes - look for status attribute
	if statusVal, hasStatus := attrs.Get("status"); hasStatus && statusVal.Type() == pcommon.ValueTypeStr {
		status := statusVal.AsString()
		if status == "blocked" || status == "daemon" || status == "detached" ||
			status == "idle" || status == "locked" || status == "orphan" ||
			status == "paging" || status == "running" || status == "sleeping" ||
			status == "stopped" || status == "system" || status == "unknown" ||
			status == "zombies" {
			return resourceTypeProcesses, true
		}
	}

	return "", false
}

// getResourceType determines the type of resource based on its attributes
func getResourceType(attrs pcommon.Map) string {
	// Try to identify if it's a host metric resource
	if resourceType, isHostMetric := identifyHostMetricType(attrs); isHostMetric {
		return resourceType
	}

	// Look for service.name as a fallback
	if val, ok := attrs.Get("service.name"); ok {
		return "service:" + val.AsString()
	}

	// Generic fallback
	return "unknown"
}

// countMetricsInResource counts the total number of metrics in a resource
func countMetricsInResource(rm pmetric.ResourceMetrics) int {
	count := 0
	for i := 0; i < rm.ScopeMetrics().Len(); i++ {
		sm := rm.ScopeMetrics().At(i)
		count += sm.Metrics().Len()
	}
	return count
}

// addAttributeToMetricDataPoints adds the given attribute to all datapoints in a metric
func addAttributeToMetricDataPoints(metric pmetric.Metric, key string, value string) {
	switch metric.Type() {
	case pmetric.MetricTypeGauge:
		dps := metric.Gauge().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dps.At(i).Attributes().PutStr(key, value)
		}
	case pmetric.MetricTypeSum:
		dps := metric.Sum().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dps.At(i).Attributes().PutStr(key, value)
		}
	case pmetric.MetricTypeHistogram:
		dps := metric.Histogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dps.At(i).Attributes().PutStr(key, value)
		}
	case pmetric.MetricTypeSummary:
		dps := metric.Summary().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dps.At(i).Attributes().PutStr(key, value)
		}
	case pmetric.MetricTypeExponentialHistogram:
		dps := metric.ExponentialHistogram().DataPoints()
		for i := 0; i < dps.Len(); i++ {
			dps.At(i).Attributes().PutStr(key, value)
		}
	}
}
