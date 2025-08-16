#!/bin/bash

# Patch IronRDP to build properly for NetBird WASM client
# This fixes build issues in the clean IronRDP submodule

echo "Applying IronRDP build patches..."

cd IronRDP || exit 1

# Fix the diplomat dependency issue by commenting out the patch
echo "Fixing diplomat dependency..."
if grep -q "\[patch.crates-io\]" Cargo.toml; then
    # Comment out the patch section
    sed -i '/\[patch.crates-io\]/,/diplomat = { git/c\
# [patch.crates-io]\
# FIXME: We need to catch up with Diplomat upstream again, but this is a significant amount of work.\
# In the meantime, we use this forked version which fixes an undefined behavior in the code expanded by the bridge macro.\
# diplomat = { git = "https://github.com/CBenoit/diplomat", rev = "6dc806e80162b6b39509a04a2835744236cd2396" }' Cargo.toml
    echo "✓ Fixed diplomat dependency in root Cargo.toml"
fi

# Fix the ironrdp-client build dependencies
echo "Fixing ironrdp-client dependencies..."
CLIENT_CARGO="crates/ironrdp-client/Cargo.toml"
if [ -f "$CLIENT_CARGO" ]; then
    # Comment out transport dependency that causes issues
    if grep -q "transport = { git" "$CLIENT_CARGO"; then
        sed -i 's/transport = { git.*$/# transport = { git = "https:\/\/github.com\/Devolutions\/devolutions-gateway", rev = "06e91dfe82751a6502eaf74b6a99663f06f0236d" }/' "$CLIENT_CARGO"
        echo "✓ Fixed transport dependency in ironrdp-client"
    fi
fi

echo "IronRDP patches applied successfully!"
echo "You can now build with: wasm-pack build --dev --target web"