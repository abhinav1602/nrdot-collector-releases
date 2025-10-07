import time
import argparse
import sys
from multiprocessing import Process, Event

def cpu_hog(duration_seconds, usage_percent, stop_event):
    """
    A function to consume a specific percentage of CPU on a single core for a given duration.

    Args:
        duration_seconds (int): The total time in seconds to run the simulation.
        usage_percent (int): The target CPU usage percentage (0-100).
        stop_event (multiprocessing.Event): An event to signal the process to stop.
    """
    start_time = time.time()

    # We run the loop for a short interval and then sleep, to control CPU usage.
    interval = 0.1  # seconds
    work_time = interval * (usage_percent / 100.0)
    sleep_time = interval - work_time

    if usage_percent <= 0:
        time.sleep(duration_seconds)
        return

    if usage_percent >= 100:
        work_time = interval
        sleep_time = 0

    print(f"  -> Hogging {usage_percent}% CPU for {duration_seconds} seconds...")
    while time.time() - start_time < duration_seconds:
        if stop_event.is_set():
            break

        # This is the busy-loop that consumes CPU
        cycle_start = time.time()
        while time.time() - cycle_start < work_time:
            _ = 1 * 1 # a dummy operation

        # Sleep for the remainder of the interval to yield CPU
        if sleep_time > 0:
            time.sleep(sleep_time)

    print(f"  -> Finished hogging CPU.")


def run_scenario(target_function, *args):
    """
    Runs a scenario in a separate process to ensure clean termination and PID.
    """
    stop_event = Event()
    p = Process(target=target_function, args=(stop_event, *args))
    try:
        p.start()
        p.join()
    except KeyboardInterrupt:
        print("\n[INFO] Keyboard interrupt received. Stopping simulator.")
        stop_event.set()
        p.join()
    finally:
        if p.is_alive():
            p.terminate()


# --- Test Scenario Implementations ---

def scenario_under_threshold(stop_event, duration_minutes, cpu_low):
    print(f"[SCENARIO] Running 'Process Under Threshold' for {duration_minutes} minutes.")
    print(f"Behavior: Process will maintain a steady {cpu_low}% CPU usage.")
    cpu_hog(duration_minutes * 60, cpu_low, stop_event)
    print("[SCENARIO] 'Process Under Threshold' finished.")



def scenario_single_spike(stop_event, duration_minutes, cpu_high, cpu_low, spike_duration_sec):
    print(f"[SCENARIO] Running 'Single Threshold Spike' for {duration_minutes} minutes.")
    print(f"Behavior: Spike to {cpu_high}% for {spike_duration_sec}s, then drop to {cpu_low}%.")

    # 1. Spike
    cpu_hog(spike_duration_sec, cpu_high, stop_event)

    # 2. Drop to low usage for the remaining time
    remaining_time_sec = (duration_minutes * 60) - spike_duration_sec
    if remaining_time_sec > 0:
        cpu_hog(remaining_time_sec, cpu_low, stop_event)

    print("[SCENARIO] 'Single Threshold Spike' finished.")



def scenario_multiple_spikes(stop_event, duration_minutes, cpu_high, cpu_low, spike_duration_sec):
    print(f"[SCENARIO] Running 'Multiple Spikes within Retention Period' for {duration_minutes} minutes.")
    print("Behavior: Spike, drop, then spike again at T=30 minutes.")

    # 1. First spike at T=0
    cpu_hog(spike_duration_sec, cpu_high, stop_event)

    # 2. Stay low until T=30 mins (minus the first spike duration)
    # 30 minutes = 1800 seconds
    wait_time_sec = 1800 - spike_duration_sec
    if wait_time_sec > 0:
        cpu_hog(wait_time_sec, cpu_low, stop_event)

    # 3. Second spike at T=30 mins
    cpu_hog(spike_duration_sec, cpu_high, stop_event)

    # 4. Stay low for the remainder of the total duration
    total_time_spent_sec = spike_duration_sec * 2 + wait_time_sec
    remaining_time_sec = (duration_minutes * 60) - total_time_spent_sec
    if remaining_time_sec > 0:
        cpu_hog(remaining_time_sec, cpu_low, stop_event)

    print("[SCENARIO] 'Multiple Spikes' finished.")



def scenario_sustained_high(stop_event, duration_minutes, cpu_high):
    print(f"[SCENARIO] Running 'Sustained High Usage' for {duration_minutes} minutes.")
    print(f"Behavior: Process will maintain a steady {cpu_high}% CPU usage.")
    cpu_hog(duration_minutes * 60, cpu_high, stop_event)
    print("[SCENARIO] 'Sustained High Usage' finished.")



def scenario_terminate_during_retention(stop_event, cpu_high, spike_duration_sec):
    print(f"[SCENARIO] Running 'Process Termination during Retention'.")
    print(f"Behavior: Spike to {cpu_high}% for {spike_duration_sec}s, then terminate.")
    cpu_hog(spike_duration_sec, cpu_high, stop_event)
    print("[SCENARIO] 'Process Termination' finished. Exiting.")
    # The script simply exits here, simulating termination.


if __name__ == "__main__":
    parser = argparse.ArgumentParser(
        description="A script to simulate various process behaviors for testing an OTEL processor.",
        formatter_class=argparse.RawTextHelpFormatter
    )

    parser.add_argument(
        "--scenario",
        type=str,
        required=True,
        choices=[
            "under_threshold",
            "single_spike",
            "multiple_spikes",
            "sustained_high",
            "terminate_during_retention"
        ],
        help="The name of the test scenario to run."
    )
    parser.add_argument("--duration-minutes", type=int, default=1440, help="Total simulation duration in minutes.")
    parser.add_argument("--cpu-high", type=int, default=10, help="Target CPU % for high usage periods.")
    parser.add_argument("--cpu-low", type=int, default=5, help="Target CPU % for low usage periods.")
    parser.add_argument("--spike-duration-sec", type=int, default=300, help="Duration of a CPU spike in seconds.")

    args = parser.parse_args()

    # This is a trick to make the process name more descriptive in tools like `ps` and `top`.
    # The OTEL collector can capture this via `process.command` or `process.command_args`.
    # We change the script's own title to include the scenario name.
    # Note: This has limitations but is a simple way to add identifying info.
    original_argv = ' '.join(sys.argv)

    # We use multiprocessing to ensure each scenario runs in a contained process,
    # making its lifecycle (start/end) clear to the OTEL collector.

    if args.scenario == "under_threshold":
        run_scenario(scenario_under_threshold, args.duration_minutes, args.cpu_low)
    elif args.scenario == "single_spike":
        run_scenario(scenario_single_spike, args.duration_minutes, args.cpu_high, args.cpu_low, args.spike_duration_sec)
    elif args.scenario == "multiple_spikes":
        run_scenario(scenario_multiple_spikes, args.duration_minutes, args.cpu_high, args.cpu_low, args.spike_duration_sec)
    elif args.scenario == "sustained_high":
        run_scenario(scenario_sustained_high, args.duration_minutes, args.cpu_high)
    elif args.scenario == "terminate_during_retention":
        run_scenario(scenario_terminate_during_retention, args.cpu_high, args.spike_duration_sec)
