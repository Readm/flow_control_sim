#!/bin/bash
(
echo "scenario test1"
for i in {1..10}; do
    echo ""
done
echo "quit"
) | go run internal/core/node/cmd/ring_simulator/main.go 2>&1 | tee /tmp/ring_sim_output.txt
