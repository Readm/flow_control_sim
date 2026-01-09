#!/bin/bash
# 
# Build the frontend from the web_dev submodule and deploy to web/static
#
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WEB_DEV_DIR="$PROJECT_ROOT/web_dev"
STATIC_DIR="$PROJECT_ROOT/web/static"

echo "==> Building Frontend from $WEB_DEV_DIR"

if [ ! -d "$WEB_DEV_DIR" ]; then
    echo "Error: web_dev submodule not found."
    echo "Please initialize submodules: git submodule update --init --recursive"
    exit 1
fi

cd "$WEB_DEV_DIR"

# Install dependencies if node_modules missing
if [ ! -d "node_modules" ]; then
    echo "  Installing dependencies..."
    npm install
fi

# Build
echo "  Running build..."
# Use the new build:app script which is configured to output to ../web/static
npm run build:app

echo "  Build complete. Assets in $STATIC_DIR"
