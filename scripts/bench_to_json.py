#!/usr/bin/env python3
import sys
import re
import json

def parse_benchmark_output(input_lines):
    results = []
    # Regex to capture Benchmark Name and sim_Hz
    # Example: Benchmark_ChampSim_64CPU/Cores_1-16 ... 2765 sim_Hz ...
    # We look for "Benchmark" at start, and then a number followed by "sim_Hz"
    regex = re.compile(r'^(Benchmark\S+).*?(\d+(?:\.\d+)?)\s+sim_Hz')

    for line in input_lines:
        line = line.strip()
        match = regex.search(line)
        if match:
            name = match.group(1)
            # Remove CPU suffix like "-16" from name if desired for cleaner charts
            # usually github-action-benchmark with 'go' keeps it, but we can keep it too.
            value = float(match.group(2))
            
            results.append({
                "name": name,
                "unit": "sim_Hz",
                "value": value
            })
    return results

if __name__ == "__main__":
    lines = sys.stdin.readlines()
    data = parse_benchmark_output(lines)
    print(json.dumps(data, indent=2))
