#!/bin/bash
set -e

echo "Running Trace Verification Test..."
# Run the specific test with -tags trace enabled
go test -v -tags trace -run TestDemoTrace ./internal/core/network

if [ $? -eq 0 ]; then
    echo "Trace Verification PASSED"
else
    echo "Trace Verification FAILED"
    exit 1
fi
