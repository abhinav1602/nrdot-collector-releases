# Adaptive Telemetry Processor

The Adaptive Telemetry Processor is a specialized OpenTelemetry processor that intelligently filters metrics based on configurable thresholds, dynamic thresholds, multi-metric evaluation, and anomaly detection.

## Architecture

The processor is implemented with a fully modular architecture consisting of the following components:

1. **Core Processor** (`processor.go`): The main structure definition and interface with the OpenTelemetry pipeline.
2. **Processor Implementations**:
   - `processor_init.go`: Initialization logic
   - `processor_anomaly.go`: Anomaly detection pipeline integration
   - `process_metrics.go`: Core metrics processing logic
   - `consume_metrics.go`: Metrics consumption pipeline
3. **Entity Model** (`entity_model.go`, `types.go`): Entity tracking and data models.
4. **Anomaly Detection** (`anomaly_detection.go`): Logic for detecting anomalies in metric values.
5. **Dynamic Thresholds** (`dynamic_thresholds.go`): Management of adaptive thresholds.
6. **Composite Metrics** (`composite_metrics.go`): Calculation of composite scores from multiple metrics.
7. **Metric Evaluation** (`metric_evaluation.go`): Evaluation of metrics against thresholds.
8. **Resource Utilities** (`resource_utils.go`): Utility functions for working with resources.
9. **Storage** (`storage.go`): Persistence layer for entity state.
10. **Configuration** (`config.go`): Configuration types and validation.
11. **Constants** (`constants.go`): Shared constant definitions.
12. **Factory** (`factory.go`): Processor factory implementation.
13. **Persistence** (`persistence.go`): Entity state persistence mechanisms.

## Features

### Static Thresholds
Configurable thresholds for specific metrics. Resources with metrics exceeding these thresholds are included in the output.

### Dynamic Thresholds
Adaptive thresholds that adjust based on observed metric values across resources. This allows for automatic adjustment to normal variations in the environment.

### Multi-Metric Evaluation
Combines multiple metrics with configurable weights to calculate a composite score. Resources with scores exceeding the threshold are included.

### Anomaly Detection
Detects anomalies in metric values by comparing current values with historical patterns. Resources exhibiting anomalous behavior are included.

### Entity Tracking
Tracks resources across batches to maintain state and apply retention policies.

### Persistence
Saves entity state to disk to maintain tracking across restarts.

## Configuration

```yaml
processors:
  adaptivetelemetryprocessor:
    # Metric thresholds - resources with values >= threshold are included
    metric_thresholds:
      process.cpu.utilization: 15.0
      system.cpu.utilization: 8.0
      process.memory.utilization: 30.0
      system.memory.utilization: 30.0
      system.filesystem.utilization: 30.0
      system.disk.io: 30.0
      system.network.io: 30.0
      system.paging.operations: 80.0
    
    # Retention period for resources after they fall below thresholds
    retention_minutes: 30
    
    # Enable dynamic thresholds based on observed values
    enable_dynamic_thresholds: true
    
    # Smoothing factor for dynamic threshold updates (0-1)
    dynamic_smoothing_factor: 0.3
    
    # Minimum and maximum values for dynamic thresholds
    min_thresholds:
      process.memory.utilization: 5.0
      system.memory.utilization: 5.0
    max_thresholds:
      process.memory.utilization: 60.0
      system.memory.utilization: 60.0
    
    # Enable multi-metric evaluation
    enable_multi_metric: true
    
    # Weights for multi-metric evaluation
    weights:
      system.filesystem.utilization: 0.4
      system.disk.io: 0.4
      system.network.io: 0.4
      system.cpu.utilization: 0.6
      system.memory.utilization: 0.6
      process.cpu.utilization: 0.7
      process.memory.utilization: 0.6
    
    # Threshold for composite score in multi-metric evaluation
    composite_threshold: 0.2
    
    # Enable anomaly detection
    enable_anomaly_detection: true
    
    # Number of data points to keep for anomaly detection
    anomaly_history_size: 3
    
    # Percentage change required to detect anomaly
    anomaly_change_threshold: 20.0
    
    # Path for persistent storage
    storage_path: "/var/lib/nrdot-collector/adaptivetelemetry.db"
    
    # Enable persistent storage
    enable_storage: true
    
    # Enable debug mode to show all filter stages
    debug_show_all_filter_stages: true
```

## Filtering Flow

1. **Anomaly Detection**: Check if any metric shows anomalous behavior
2. **Dynamic Thresholds**: Check if any metric exceeds its dynamic threshold
3. **Static Thresholds**: Check if any metric exceeds its static threshold
4. **Multi-Metric**: Calculate composite score and check against threshold
5. **Retention**: Include resources that were recently above thresholds

## Persistence

Entity state is persisted to disk at configurable intervals. This allows the processor to maintain tracking across restarts. The persistence layer is implemented in `storage.go` and `persistence.go`.

### Persisted State

The following data is persisted for each tracked entity:

- Entity identity
- First seen timestamp
- Last exceeded timestamp
- Current metric values
- Maximum metric values
- Resource attributes
- Metric history (for anomaly detection)
- Last anomaly timestamp

### Storage Implementation

The processor uses a file-based storage mechanism that serializes entity state to JSON and stores it in the configured path. The storage implementation automatically creates the required directory structure if it doesn't exist.

For testing, the database file is stored in the `test_data` directory to keep test artifacts organized.

## Development Status

The processor has been fully modularized from its original monolithic implementation into smaller, more focused components to improve maintainability, testability, and extensibility.

### Completed Refactoring

1. Core functionality has been extracted into dedicated files with clear responsibilities
2. Constants have been moved to a dedicated `constants.go` file
3. Entity models and types are well-defined in separate files
4. Comprehensive test coverage has been implemented for all components
5. Clear separation of concerns with specialized files for each feature area

### Testing

The processor includes comprehensive test coverage with various test types:
- Unit tests for each component
- Integration tests for feature combinations
- Persistence tests to verify state is maintained across restarts
- Resource utility tests for different resource types

Test data files are stored in the `test_data` directory to keep test artifacts organized.
