#!/usr/bin/env python3
"""Run all examples and collect results."""
import os
import subprocess
import sys
import time

EXAMPLES = [
    ("example1", "data_analyst_agent.py", "Data Analyst Agent"),
    ("example2", "iterate_fix_failing_tests.py", "Iterate Fix Failing Tests"),
    ("example3", "gate_human_in_the_loop.py", "Gate Human in the Loop"),
    ("example4", "outcome_grader.py", "Outcome Grader"),
    ("example5", "coordinate_team.py", "Coordinate Specialist Team"),
    ("example6", "remember_preferences.py", "Remember User Preferences"),
    ("example7", "explore_unfamiliar_codebase.py", "Explore Unfamiliar Codebase"),
    ("example8", "orchestrate_issue_to_pr.py", "Orchestrate Issue to PR"),
    ("example9", "sre_incident_responder.py", "SRE Incident Responder"),
]

def run_example(example_dir, script_name, description):
    """Run a single example and return result."""
    script_path = os.path.join(example_dir, script_name)
    if not os.path.isfile(script_path):
        return {
            "description": description,
            "status": "SKIP",
            "exit_code": None,
            "duration": 0,
            "error": "Script not found",
        }

    print(f"\n{'='*60}")
    print(f"Running: {description}")
    print(f"Script: {script_path}")
    print(f"{'='*60}")

    start_time = time.time()
    try:
        result = subprocess.run(
            [sys.executable, script_path],
            capture_output=True,
            text=True,
            timeout=300,  # 5 minutes timeout
            cwd=os.path.dirname(os.path.abspath(__file__))
        )
        duration = time.time() - start_time

        # Print output
        if result.stdout:
            print("STDOUT:")
            print(result.stdout)
        if result.stderr:
            print("STDERR:")
            print(result.stderr)

        return {
            "description": description,
            "status": "PASS" if result.returncode == 0 else "FAIL",
            "exit_code": result.returncode,
            "duration": duration,
            "stdout": result.stdout,
            "stderr": result.stderr,
        }
    except subprocess.TimeoutExpired:
        duration = time.time() - start_time
        return {
            "description": description,
            "status": "TIMEOUT",
            "exit_code": None,
            "duration": duration,
            "error": "Timeout after 300s",
        }
    except Exception as e:
        duration = time.time() - start_time
        return {
            "description": description,
            "status": "ERROR",
            "exit_code": None,
            "duration": duration,
            "error": str(e),
        }

def main():
    results = []

    for example_dir, script_name, description in EXAMPLES:
        result = run_example(example_dir, script_name, description)
        results.append(result)

    # Print summary table
    print("\n" + "="*80)
    print("SUMMARY TABLE")
    print("="*80)
    print(f"{'Example':<35} {'Status':<10} {'Exit Code':<10} {'Duration':<10}")
    print("-"*80)

    for result in results:
        duration_str = f"{result['duration']:.2f}s"
        exit_code_str = str(result['exit_code']) if result['exit_code'] is not None else "N/A"
        print(f"{result['description']:<35} {result['status']:<10} {exit_code_str:<10} {duration_str:<10}")

    print("="*80)

    # Count results
    pass_count = sum(1 for r in results if r['status'] == 'PASS')
    fail_count = sum(1 for r in results if r['status'] == 'FAIL')
    skip_count = sum(1 for r in results if r['status'] == 'SKIP')
    timeout_count = sum(1 for r in results if r['status'] == 'TIMEOUT')
    error_count = sum(1 for r in results if r['status'] == 'ERROR')

    print(f"\nTotal: {len(results)} | Pass: {pass_count} | Fail: {fail_count} | Skip: {skip_count} | Timeout: {timeout_count} | Error: {error_count}")

if __name__ == "__main__":
    main()
