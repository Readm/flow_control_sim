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
# We use vue-cli-service directly or via npm run build
# Note: package.json script 'build' is: vue-cli-service build --target lib ...
# BUT we want a full app build for examples/main.js as per original plan inspection
# Original 'serve' script was: vue-cli-service serve examples/main.js
# So we should build that entry point.

# Ensure output goes to correct place
# vue-cli-service build [entry] --dest [destination]
NODE_OPTIONS=--openssl-legacy-provider npx vue-cli-service build examples/main.js --dest "$STATIC_DIR" --name index

# Post-processing: Vue CLI build might name it index.html
echo "  Build complete. Assets in $STATIC_DIR"
