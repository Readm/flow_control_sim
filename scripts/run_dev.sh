#!/bin/bash

# Define colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

echo -e "${BLUE}=== FlowSim Integrated Development Environment ===${NC}"

# Function to cleanup background processes on exit
cleanup() {
    echo -e "\n${BLUE}Shutting down services...${NC}"
    # Kill the backend server process if we recorded its PID
    if [ ! -z "$BACKEND_PID" ]; then
        kill $BACKEND_PID 2>/dev/null
    fi
    # Also ensure ports are freed
    fuser -k 8081/tcp > /dev/null 2>&1
    fuser -k 8080/tcp > /dev/null 2>&1
    echo -e "${GREEN}All services stopped.${NC}"
    exit
}

# Trap SIGINT (Ctrl+C)
trap cleanup SIGINT

# 1. Cleanup Ports
echo -e "${BLUE}[1/3] Cleaning up ports 8080 (Web) and 8081 (Server)...${NC}"
fuser -k 8081/tcp > /dev/null 2>&1
fuser -k 8080/tcp > /dev/null 2>&1

# 2. Start Backend Server
echo -e "${BLUE}[2/3] Starting Backend Server (Port 8081)...${NC}"
cd "$(dirname "$0")/.."
# Check if web_dev is initialized
if [ ! -d "web_dev" ] || [ -z "$(ls -A web_dev)" ]; then
    echo -e "${BLUE}Initializing web_dev submodule...${NC}"
    git submodule update --init --recursive
fi

# Run in background, redirect output to a log file or let it print to stdout? 
# Let's print to stdout but prefixed, or just let it mix for now (simplest for dev).
# In Dev mode, backend static files path is less critical because we use frontend dev server,
# but we point it to source examples just in case.
go run -tags e2e cmd/server/main.go --port 8081 --static ./web_dev/examples &
BACKEND_PID=$!
echo -e "${GREEN}Backend Server started with PID $BACKEND_PID${NC}"

# Wait a moment for backend to initialize
sleep 2

# 3. Start Frontend Dev Server
echo -e "${BLUE}[3/3] Starting Frontend Dev Server (Port 8080)...${NC}"
cd web_dev

# Check for node_modules
if [ ! -d "node_modules" ]; then
    echo -e "${BLUE}Installing frontend dependencies...${NC}"
    npm install
fi

# npm run serve blocks, so we run it in foreground. 
# Ctrl+C will trigger the trap to kill backend.
NODE_OPTIONS=--openssl-legacy-provider npm run serve
