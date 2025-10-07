package adaptivetelemetryprocessor

import (
	"fmt"
	"sort"
	"strings"

	"go.opentelemetry.io/collector/pdata/pcommon"
)

// buildResourceIdentity returns a stable, human-readable identity string for a resource.
// Priority order:
// - service or entity identifiers
// - hostmetrics specific identifiers (CPU, disk, filesystem, etc)
// - service.instance.id or service.name(+service.namespace)
// - fallback: sorted concatenation of all resource attributes
func buildResourceIdentity(res pcommon.Resource) string {
	attrs := res.Attributes()

	// Get host information if available for any resource
	host := getHostName(attrs)

	// Try host metrics identification first
	if identity := buildHostMetricIdentity(attrs, host); identity != "" {
		return identity
	}

	// Try service identification
	if identity := buildServiceIdentity(attrs); identity != "" {
		return identity
	}

	// Fallback to attribute concatenation
	return buildFallbackIdentity(attrs)
}

// getHostName extracts the host name from resource attributes
func getHostName(attrs pcommon.Map) string {
	if hv, ok := attrs.Get("host.name"); ok {
		return hv.AsString()
	}
	return ""
}

// buildHostMetricIdentity builds identity for host metrics with specific formatting
func buildHostMetricIdentity(attrs pcommon.Map, host string) string {
	hostMetricType, isHostMetric := identifyHostMetricType(attrs)
	if !isHostMetric {
		return ""
	}

	switch hostMetricType {
	case resourceTypeCPU:
		return buildCPUIdentity(attrs, host)
	case resourceTypeDisk, resourceTypeFilesystem, resourceTypeNetwork:
		return buildDeviceBasedIdentity(attrs, host, hostMetricType)
	case resourceTypeLoad, resourceTypeMemory, resourceTypeProcesses, resourceTypePaging:
		return buildSimpleHostIdentity(host, hostMetricType)
	default:
		return ""
	}
}

// buildCPUIdentity builds identity for CPU metrics
func buildCPUIdentity(attrs pcommon.Map, host string) string {
	cpuNum := ""
	if cpu, ok := attrs.Get("cpu"); ok {
		cpuNum = "." + cpu.AsString()
	}

	if host != "" {
		return fmt.Sprintf("cpu%s@%s", cpuNum, host)
	}
	return fmt.Sprintf("cpu%s", cpuNum)
}

// buildDeviceBasedIdentity builds identity for device-based metrics (disk, filesystem, network)
func buildDeviceBasedIdentity(attrs pcommon.Map, host, hostMetricType string) string {
	device := ""
	if dev, ok := attrs.Get("device"); ok {
		device = "." + dev.AsString()
	}

	if host != "" {
		return fmt.Sprintf("%s%s@%s", hostMetricType, device, host)
	}
	return fmt.Sprintf("%s%s", hostMetricType, device)
}

// buildSimpleHostIdentity builds identity for simple host metrics (load, memory, processes, paging)
func buildSimpleHostIdentity(host, hostMetricType string) string {
	if host != "" {
		return fmt.Sprintf("%s@%s", hostMetricType, host)
	}
	return hostMetricType
}

// buildServiceIdentity builds identity for service-based resources
func buildServiceIdentity(attrs pcommon.Map) string {
	// Check for service.instance.id first
	if v, ok := attrs.Get("service.instance.id"); ok {
		return "service.instance.id:" + v.AsString()
	}

	// Check for service.name with optional namespace
	if v, ok := attrs.Get("service.name"); ok {
		serviceName := v.AsString()
		namespace := ""
		if nv, ok := attrs.Get("service.namespace"); ok {
			namespace = nv.AsString()
		}

		if namespace != "" {
			return "service:" + namespace + "/" + serviceName
		}
		return "service:" + serviceName
	}

	return ""
}

// buildFallbackIdentity builds identity from all resource attributes as fallback
func buildFallbackIdentity(attrs pcommon.Map) string {
	// Collect all attribute keys
	keys := make([]string, 0, attrs.Len())
	attrs.Range(func(k string, _ pcommon.Value) bool {
		keys = append(keys, k)
		return true
	})

	if len(keys) == 0 {
		return "resource:empty"
	}

	// Sort keys for deterministic output
	sort.Strings(keys)

	// Build key=value pairs
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v, ok := attrs.Get(k); ok {
			parts = append(parts, k+"="+v.AsString())
		}
	}

	// Join and truncate if necessary
	id := strings.Join(parts, ",")
	if len(id) > 512 {
		id = id[:512]
	}
	return id
}

// snapshotResourceAttributes copies resource attributes into a plain map for logging/debugging
func snapshotResourceAttributes(res pcommon.Resource) map[string]string {
	out := map[string]string{}
	res.Attributes().Range(func(k string, v pcommon.Value) bool {
		out[k] = v.AsString()
		return true
	})
	return out
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
