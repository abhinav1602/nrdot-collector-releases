# Scenario-Based Traffic Simulation Script Documentation

## Overview

This script (`scenario_bases_traffic_simulation.py`) is designed to simulate various CPU usage patterns for testing the behavior of an OpenTelemetry (OTEL) processor, particularly in scenarios involving adaptive thresholding and retention logic. It generates controlled CPU load on a single core, allowing developers to validate how the processor responds to different resource usage patterns.

## Test Scenarios

The script supports the following test scenarios:

### 1. Process Under Threshold (`under_threshold`)
- **Description:** Simulates a process that maintains a steady, low CPU usage (e.g., 10%) for a specified duration.
- **Purpose:** Tests the processor's behavior when resource usage remains below the configured threshold.

### 2. Single Threshold Spike (`single_spike`)
- **Description:** Simulates a brief spike to high CPU usage (e.g., 90%) for a short period, followed by low usage for the remainder of the test.
- **Purpose:** Validates detection and handling of short-lived threshold violations.

### 3. Multiple Spikes within Retention Period (`multiple_spikes`)
- **Description:** Simulates two spikes to high CPU usage separated by a period of low usage, with the second spike occurring within a retention window (e.g., at 30 minutes).
- **Purpose:** Tests the processor's response to repeated violations within the retention period.

### 4. Sustained High Usage (`sustained_high`)
- **Description:** Simulates a process that maintains high CPU usage for the entire test duration.
- **Purpose:** Evaluates the processor's handling of continuous threshold violations.

### 5. Process Termination during Retention (`terminate_during_retention`)
- **Description:** Simulates a process that spikes to high CPU usage and then terminates abruptly.
- **Purpose:** Tests how the processor manages process termination during retention.

## Testing Approach

- Each scenario is implemented as a function that uses a busy loop to generate CPU load, controlled by the `cpu_hog` function.
- Scenarios are run in separate processes using Python's `multiprocessing` module to ensure isolation and clean termination.
- The script uses command-line arguments to select and configure scenarios, making it flexible for automated and manual testing.
- The approach allows for reproducible and controlled simulation of resource usage patterns, aiding in the validation of thresholding, anomaly detection, and retention logic in telemetry processors.

## Usage

Run the script from the command line, specifying the desired scenario and parameters. Example:

```bash
python scenario_bases_traffic_simulation.py --scenario single_spike --duration_minutes 10 --cpu_high 90 --cpu_low 10 --spike_duration_sec 30
```

Refer to the script's help message (`--help`) for details on available arguments and their usage.

## Threshold-Oriented Example Commands

The following examples show how to run long-lived simulations under a notional static threshold (e.g., 5% CPU). These use `nohup` so they continue after you disconnect. Output (stdout/stderr) is redirected to a log file per scenario.

NOTE:
- The script file in this repository is named `scenario_bases_traffic_simulation.py`.
- In the original request, the narrative described 90% spikes while passing `--cpu-high 10`. Adjust `--cpu-high` to the actual percentage you want (e.g., 90) if testing high saturation. Below we preserve the supplied arguments and add a corrected variant.

```bash
# 1. Process stays below threshold (runs at ~5%)
nohup python3 scenario_bases_traffic_simulation.py --scenario under_threshold --cpu-low 5 > under_threshold.log 2>&1 &

# 2. Single spike: spike (configured high), then drop to low
nohup python3 scenario_bases_traffic_simulation.py --scenario single_spike --cpu-high 10 --cpu-low 5 > single_spike.log 2>&1 &

# (Use 90% spike instead of 10%)
nohup python3 scenario_bases_traffic_simulation.py --scenario single_spike --cpu-high 90 --cpu-low 5 --spike-duration-sec 30 > single_spike_90pct.log 2>&1 &

# 3. Multiple spikes: two spikes separated by low usage
nohup python3 scenario_bases_traffic_simulation.py --scenario multiple_spikes --cpu-high 10 --cpu-low 5 > multiple_spikes.log 2>&1 &

# (Higher spike version)
nohup python3 scenario_bases_traffic_simulation.py --scenario multiple_spikes --cpu-high 90 --cpu-low 5 --spike-duration-sec 300 > multiple_spikes_90pct.log 2>&1 &

# 4. Sustained high usage
nohup python3 scenario_bases_traffic_simulation.py --scenario sustained_high --cpu-high 10 > sustained_high.log 2>&1 &

# (Higher sustained load)
nohup python3 scenario_bases_traffic_simulation.py --scenario sustained_high --cpu-high 90 > sustained_high_90pct.log 2>&1 &

# 5. Spike then termination
nohup python3 scenario_bases_traffic_simulation.py --scenario terminate_during_retention --cpu-high 10 > terminate_during_retention.log 2>&1 &

# (Higher termination spike)
nohup python3 scenario_bases_traffic_simulation.py --scenario terminate_during_retention --cpu-high 90 --spike-duration-sec 120 > terminate_90pct.log 2>&1 &
```

### Stopping All Running Simulations

To terminate every running instance (either name) in one command:

```bash
pkill -f "python3 scenario_bases_traffic_simulation.py"
```

### Log Review Tips

```bash
# Tail a specific scenario log
tail -f sustained_high.log
# Quick grep for spike start markers
grep "SCENARIO" single_spike.log
```

### Parameter Guidance
- `--cpu-high`: Percent of a single core to target during spike/high intervals. Use realistic values (e.g., 60–95) for saturation tests.
- `--cpu-low`: Baseline percent below your threshold (ensure it's clearly below to test non-violation behavior).
- `--spike-duration-sec`: Duration of each spike window. Short (<=30s) tests transient detection; longer tests sustained detection logic.
- `--duration-minutes`: Total wall-clock duration (ignored by `terminate_during_retention` after spike completes).

## Notes
- The script is intended for use in test environments and should not be run on production systems.
- CPU usage is simulated on a single core; for multi-core tests, run multiple instances or modify the script accordingly.
- Ensure you have appropriate permissions to run CPU-intensive processes on your system.
- Consider exporting host metrics while running these scenarios to correlate processor decisions with raw resource usage.
