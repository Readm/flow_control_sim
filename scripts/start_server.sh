#!/bin/bash

# Kill existing server if running
echo "Checking for running server..."
fuser -k 8081/tcp > /dev/null 2>&1
if [ $? -eq 0 ]; then
    echo "Killed existing server on port 8081"
fi

# Build and run server
echo "Starting Visualization Server..."
# Make sure we are in the project root
cd "$(dirname "$0")/.."

# Check if web/examples exists
if [ ! -d "./web/examples" ]; then
    echo "Error: ./web/examples directory not found!"
    exit 1
fi

echo "Serving static files from ./web/examples"
echo "Listening on http://localhost:8081"

# Run with e2e tag for mock controller
go run -tags e2e cmd/server/main.go --port 8081 --static ./web/examples
