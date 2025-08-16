#!/bin/bash

# Build and run the NetBird test server
cd "$(dirname "$0")"

echo "Building NetBird test server..."
go build -o test_server test_server.go

if [ $? -eq 0 ]; then
    echo "Build successful!"
    echo ""
    ./test_server
else
    echo "Build failed!"
    exit 1
fi