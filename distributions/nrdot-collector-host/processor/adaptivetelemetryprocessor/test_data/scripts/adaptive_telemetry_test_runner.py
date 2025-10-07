#!/usr/bin/env python3
"""
Adaptive Telemetry Filter Test Runner - ERROR FREE VERSION

This script generates test processes to demonstrate ALL stages of the
adaptive telemetry filter by manipulating CPU and Memory usage.
"""

import argparse
import time
import os
import signal
import sys
import threading
import multiprocessing
import psutil
import logging
from datetime import datetime
import random

# --- Configuration & Setup ---
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(levelname)s - [%(name)s] - %(message)s',
    handlers=[
        logging.StreamHandler(),
        logging.FileHandler("adaptive_filter_test.log")
    ]
)
logger = logging.getLogger("ATF-Test")
running = True

# Helper to get system total memory for realistic MB calculation
def get_total_memory_mb():
    # Using float division for accuracy
    return psutil.virtual_memory().total / (1024 * 1024) 

TOTAL_MEM_MB = get_total_memory_mb()

def convert_percent_to_mb(percent):
    """Converts a percentage of total system memory to MB."""
    return (percent / 100.0) * TOTAL_MEM_MB

# --- Scenario Definitions (Tuned to config.yaml) ---
# Format: (initial_cpu_pct, peak_cpu_pct, initial_mem_pct, peak_mem_pct, name)
SCENARIOS = {
    "static": [
        (20, 20, 10, 10, "trigger-static-cpu-20") 
    ],
    "dynamic": [
        (5, 5, 40, 40, "trigger-dynamic-mem-40") 
    ],
    "multi": [
        (12, 12, 10, 10, "trigger-multi-pass-0.76") 
    ],
    "anomaly": [
        (5, 60, 5, 5, "trigger-anomaly-cpu-spike") 
    ],
    "retention": [
        (30, 30, 10, 10, "trigger-retention-active"),
        (0.1, 0.1, 0.1, 0.1, "trigger-retention-idle")
    ]
}

SYSTEM_LOADS = {
    "low": (2, convert_percent_to_mb(10)),
    "medium": (4, convert_percent_to_mb(20)),
    "high": (6, convert_percent_to_mb(30))
}

# --- CORE SIMULATOR CLASSES (DEFINED FIRST TO AVOID NAME ERRORS) ---

class ProcessSimulator:
    """Creates and manages a separate process with controlled CPU and memory usage."""
    def __init__(self, name, cpu_target, memory_pct_target):
        self.name = name
        self.cpu_target = cpu_target
        # Converts target percentage to MB once upon initialization/update
        self.memory_mb_target = convert_percent_to_mb(memory_pct_target) 
        self.running = False
        self.process = None
        self.pid = None
        self.start_time = None
        self.stop_event = multiprocessing.Event()
        
    def _process_worker(self, cpu_target, memory_mb_target, event):
        """Worker function that runs in a separate process (the resource consumer)."""
        try:
            if hasattr(os, 'setproctitle'):
                import setproctitle
                setproctitle.setproctitle(self.name)
            
            self.pid = os.getpid()
            memory = []
            
            # Memory allocation
            if memory_mb_target > 0:
                # Allocates memory in 1MB chunks
                num_chunks = int(memory_mb_target)
                for _ in range(num_chunks):
                    if event.is_set(): break
                    try:
                        chunk = bytearray(1024 * 1024)
                        for i in range(0, len(chunk), 4096): chunk[i] = 1 # Touch memory
                        memory.append(chunk)
                    except MemoryError: break
            
            # CPU busy-loop
            while not event.is_set():
                if cpu_target > 0:
                    active_time = 0.01 * (cpu_target / 100.0)
                    active_end = time.time() + active_time
                    while time.time() < active_end and not event.is_set():
                        x = 0
                        for i in range(1000): x += i * i
                    
                    sleep_time = 0.01 - (time.time() - (active_end - active_time))
                    if sleep_time > 0: time.sleep(sleep_time)
                else:
                    time.sleep(0.01)
            
            memory.clear()
        except Exception as e:
            logger.error(f"Error in process {self.name}: {e}")
        sys.exit(0)
    
    def start(self):
        if self.running: return
        self.running = True
        self.start_time = datetime.now()
        self.stop_event.clear()
        self.process = multiprocessing.Process(
            target=self._process_worker,
            args=(self.cpu_target, self.memory_mb_target, self.stop_event),
            name=self.name
        )
        self.process.daemon = True
        self.process.start()
        self.pid = self.process.pid
        logger.info(f"START: {self.name} (PID: {self.pid}) - Target: CPU {self.cpu_target}%, Mem {self.memory_mb_target:.1f}MB")
        
    def update(self, new_cpu_target, new_memory_pct_target):
        # Update targets and restart the process to apply new memory/CPU allocation
        self.cpu_target = new_cpu_target
        self.memory_mb_target = convert_percent_to_mb(new_memory_pct_target)
        self.stop()
        self.start()
        logger.info(f"UPDATE: {self.name} -> CPU {self.cpu_target}%, Mem {self.memory_mb_target:.1f}MB")
        
    def stop(self):
        if not self.running: return
        self.running = False
        self.stop_event.set()
        if self.process:
            self.process.join(timeout=1)
            if self.process.is_alive(): self.process.terminate()

    def is_running(self):
        return self.running and self.process and self.process.is_alive()
        
    def get_stats(self):
        try:
            if not self.pid or not self.is_running(): 
                return {"name": self.name, "cpu_actual": 0.0, "memory_actual_mb": 0.0, "status": "Stopped"}
            proc = psutil.Process(self.pid)
            cpu_percent = proc.cpu_percent() / psutil.cpu_count()
            memory_mb = proc.memory_info().rss / (1024 * 1024)
            return {
                "name": self.name,
                "pid": self.pid,
                "cpu_target": self.cpu_target,
                "cpu_actual": cpu_percent,
                "memory_actual_mb": memory_mb,
                "status": "Running"
            }
        except psutil.NoSuchProcess:
            return {"name": self.name, "cpu_actual": 0.0, "memory_actual_mb": 0.0, "status": "Terminated"}
        except Exception as e:
            return {"name": self.name, "cpu_actual": 0.0, "memory_actual_mb": 0.0, "status": f"Error: {e}"}

class SystemLoadSimulator:
    """Simulates background system load."""
    
    def __init__(self, level):
        self.level = level
        # Uses globally defined SYSTEM_LOADS
        self.cpu_workers, self.memory_mb = SYSTEM_LOADS.get(level) 
        self.processes = []
        self.running = False
        
    def start(self):
        self.running = True
        
        # CPU workers
        max_workers = min(self.cpu_workers, os.cpu_count() or 4)
        cpu_per_worker = 80 / max_workers
        for i in range(max_workers):
            name = f"sysload-cpu-{i}"
            # Convert fixed MB to percentage for ProcessSimulator
            mem_pct = (10 / TOTAL_MEM_MB) * 100 
            proc = ProcessSimulator(name, cpu_per_worker, mem_pct) 
            proc.start()
            self.processes.append(proc)
            
        # Memory consumer process
        mem_pct = (self.memory_mb / TOTAL_MEM_MB) * 100
        mem_proc = ProcessSimulator("sysload-memory", 5, mem_pct) 
        mem_proc.start()
        self.processes.append(mem_proc)
            
        logger.info(f"System Load: {self.level.upper()} (CPU workers: {self.cpu_workers}, Memory: {self.memory_mb:.1f}MB)")
        
    def stop(self):
        self.running = False
        for proc in self.processes:
            proc.stop()
        self.processes.clear()
        logger.info("System load simulation stopped")

class ScenarioRunner:
    """Manages the full test sequence to trigger all stages."""
    
    def __init__(self, duration=180, interval=1, system_load="low"):
        self.duration = duration
        self.interval = interval
        self.system_load = system_load
        self.processes = []
        self.system_simulator = None
        self.start_time = None
        self.lock = threading.Lock()
        self.running = False
        
    def start(self):
        global running
        running = True
        self.running = True
        self.start_time = time.time()
        
        # Corrected usage: SystemLoadSimulator is now defined and accessible
        if self.system_load:
            self.system_simulator = SystemLoadSimulator(self.system_load) 
            self.system_simulator.start()
        
        threading.Thread(target=self._run_all_scenarios_sequence, daemon=True).start()
        
        try:
            while running and (time.time() - self.start_time) < self.duration:
                self._print_status()
                time.sleep(self.interval)
        except KeyboardInterrupt:
            logger.info("Test interrupted by user")
        finally:
            self.stop()
    
    def _run_all_scenarios_sequence(self):
        all_scenarios = []
        for scenario_type in ["static", "dynamic", "multi", "anomaly", "retention"]:
            for process_def in SCENARIOS.get(scenario_type, []):
                all_scenarios.append((process_def, scenario_type))
        
        for process_def, scenario_type in all_scenarios:
            if not self.running: break
            threading.Thread(target=self._run_single_process, args=(process_def, scenario_type), daemon=True).start()
            time.sleep(0.5) 
        
        time.sleep(max(0, self.duration - (time.time() - self.start_time)))
            
        logger.info("All test scenarios completed.")
    
    def _run_single_process(self, process_def, scenario_type):
        
        initial_cpu, peak_cpu, initial_mem_pct, peak_mem_pct, name = process_def
        process_name = f"{scenario_type}-{name}"
        
        proc = ProcessSimulator(process_name, initial_cpu, initial_mem_pct)
        proc.start()
        
        with self.lock:
            self.processes.append(proc)
        
        # --- Stage-Specific Timing Logic ---
        if scenario_type == "anomaly":
            time.sleep(3) 
            if self.running:
                proc.update(peak_cpu, peak_mem_pct)
                time.sleep(3) 
                if self.running:
                    proc.update(initial_cpu, initial_mem_pct)
                    time.sleep(max(0, self.duration - (time.time() - self.start_time)))
        
        elif scenario_type == "retention":
            time.sleep(5) 
            if self.running:
                # Drop to idle after initial spike
                idle_cpu, _, idle_mem_pct, _, _ = SCENARIOS['retention'][-1]
                proc.update(idle_cpu, idle_mem_pct)
                time.sleep(70) # Wait past the 60s retention window
                
                proc.stop()
        
        else: # Static, Dynamic, Multi-Metric
            time.sleep(3)
            if self.running and (initial_cpu != peak_cpu or initial_mem_pct != peak_mem_pct):
                 proc.update(peak_cpu, peak_mem_pct)
            
            time.sleep(max(0, self.duration - (time.time() - self.start_time)))
    
    def _print_status(self):
        current_time = time.time() - self.start_time
        print("\n" + "="*95)
        print(f" ADAPTIVE FILTER TEST RUNNER - ELAPSED: {int(current_time)}s / {self.duration}s | Load: {self.system_load.upper()}")
        print("="*95)
        
        print(f"{'NAME':<30} {'PID':<6} {'CPU%':>6} {'MEM MB':>10} {'STAGE TARGET':<20} {'STATUS':<15} {'NOTE':<5}")
        print("-" * 95)
        
        with self.lock:
            current_processes = list(self.processes)
        
        for proc in current_processes:
            stats = proc.get_stats()
            target_name = proc.name.split('-')[0].upper()
            
            note = ""
            if target_name == "RETENTION" and current_time > 65 and stats.get("status") == "Running":
                 note = "EXCLUDED?"
            elif target_name == "RETENTION" and current_time < 65 and stats.get("status") == "Running":
                 note = "INCLUDED (RETENTION)"
            
            print(f"{stats.get('name', 'N/A'):<30} {stats.get('pid', 'N/A'):<6} {stats.get('cpu_actual', 0.0):>6.1f} {stats.get('memory_actual_mb', 0.0):>10.1f} "
                  f"{target_name:<20} {stats.get('status', 'N/A'):<15} {note:<5}")
        
        print("="*95)
    
    def stop(self):
        global running
        running = False
        self.running = False
        if self.system_simulator: self.system_simulator.stop()
        with self.lock:
            for proc in self.processes: proc.stop()
            self.processes.clear()
        logger.info("Scenario runner cleanup complete.")


def parse_arguments():
    parser = argparse.ArgumentParser(description="Test script for adaptive process filter")
    parser.add_argument("--duration", type=int, default=180,
                        help="Total duration to run the test in seconds (default: 180)")
    parser.add_argument("--system-load", choices=["low", "medium", "high"],
                        default="low", help="Simulate system load for dynamic threshold testing (default: low)")
    parser.add_argument("--interval", type=int, default=1,
                        help="Interval between measurements in seconds (default: 1)")
    return parser.parse_args()


if __name__ == "__main__":
    multiprocessing.set_start_method('spawn', force=True)
    signal.signal(signal.SIGINT, lambda sig, frame: sys.exit(0))
    
    args = parse_arguments()
    
    runner = ScenarioRunner(
        duration=args.duration,
        interval=args.interval,
        system_load=args.system_load,
    )
    
    try:
        runner.start()
        
    except KeyboardInterrupt:
        pass
    finally:
        runner.stop()
        print("\nTest run finished.")