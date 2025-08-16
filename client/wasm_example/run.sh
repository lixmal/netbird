#!/bin/bash

# Kill any existing processes
pkill -f "proxy.*backend"
pkill -f "python3.*http.server"

echo "Starting NetBird WASM example..."

# Always run build to ensure everything is up to date
echo "Building components..."
./build.sh
if [ $? -ne 0 ]; then
    echo "Build failed!"
    exit 1
fi


# Start web server
echo "Starting web server (port 10001)..."
(cd web && python3 -m http.server 10001) &
WEB_PID=$!

echo ""
echo "==================================="
echo "NetBird WASM Example Running!"
echo "==================================="
echo "gRPC proxy running on: ws://localhost:8081"
echo "Web UI at: http://localhost:10001"
echo ""
echo "Features:"
echo "  - HTTP client to NetBird peers"
echo "  - SSH connections (built-in TCP proxy)"
echo "  - RDP connections via IronRDP WASM through NetBird network"
echo "  - VNC connections via noVNC JavaScript library through NetBird network"
echo ""
echo "Press Ctrl+C to stop all services"
echo "==================================="

# Cleanup function
cleanup() {
    echo ""
    echo "Stopping services..."
    kill $WEB_PID 2>/dev/null
    exit 0
}

# Set trap for cleanup
trap cleanup INT TERM

# Wait for processes
wait
