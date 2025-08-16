#!/bin/bash

echo "Building NetBird WASM client..."

# Copy the wasm_exec.js support file from Go installation
cp "$(go env GOROOT)/lib/wasm/wasm_exec.js" web/

# Build the WASM module
echo "Building WASM module..."
GOOS=js GOARCH=wasm go build -tags=devcert -ldflags="-s -w" -o web/netbird.wasm ./wasm/cmd/netbird-wasm

BUILD_SUCCESS=$?

# Check if IronRDP sources have changed or if the output doesn't exist
IRONRDP_OUTPUT="web/ironrdp-pkg/ironrdp_web_bg.wasm"
IRONRDP_SOURCES="IronRDP/crates/ironrdp-web/src IronRDP/crates/ironrdp-web/Cargo.toml"
REBUILD_IRONRDP=false

if [ ! -f "$IRONRDP_OUTPUT" ]; then
    echo "IronRDP output not found, building..."
    REBUILD_IRONRDP=true
else
    # Check if any source file is newer than the output
    for src in $IRONRDP_SOURCES; do
        if [ -e "$src" ]; then
            if find "$src" -newer "$IRONRDP_OUTPUT" -print -quit | grep -q .; then
                echo "IronRDP sources changed, rebuilding..."
                REBUILD_IRONRDP=true
                break
            fi
        fi
    done
fi

if [ "$REBUILD_IRONRDP" = true ]; then
    echo "Applying IronRDP patches for NetBird build..."
    ./ironrdp-build-patch.sh > /dev/null 2>&1
    
    echo "Building IronRDP WASM module..."
    cd IronRDP/crates/ironrdp-web
    wasm-pack build --release --target web --out-dir ../../../web/ironrdp-pkg 2>&1 | tail -5
    IRONRDP_BUILD=$?
    cd ../../..
else
    echo "IronRDP unchanged, skipping build..."
    IRONRDP_BUILD=0
fi
