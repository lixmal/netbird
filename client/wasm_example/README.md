# NetBird WASM Client Example

This directory contains a WebAssembly (WASM) implementation of the NetBird client that runs directly in web browsers.

## Features

- **Browser-based NetBird client**: Connect to your NetBird network from any modern web browser
- **HTTP proxy**: Access HTTP services on NetBird peers
- **SSH client**: Connect to SSH servers on NetBird peers  
- **RDP client**: Connect to Windows Remote Desktop servers via IronRDP WASM
- **VNC client**: Connect to VNC servers using noVNC JavaScript library
- **TCP proxy**: Built-in TCP-to-WebSocket proxy for accessing TCP services
- **Test server**: Included HTTP server for testing browser functionality

## Prerequisites

- Go 1.19 or higher
- Python 3 (for the development web server)
- A NetBird setup key and management URL

## Quick Start

1. **Run everything with a single command:**
   ```bash
   ./run.sh
   ```
   This will:
   - Build all required components (grpc-proxy, test server, WASM module)
   - Start the gRPC-WebSocket proxy on port 8081
   - Start the web server on port 10001

2. **Open your browser:**
   Navigate to http://localhost:10001

3. **Connect to NetBird:**
   - Enter your setup key
   - Enter your management URL
   - Click "Connect"

## Configuration

### gRPC Proxy Configuration

The gRPC proxy bridges WebSocket connections from the browser to the NetBird management gRPC API. 

**Important:** You need to adjust the backend address in `run.sh` to match your NetBird management server:

```bash
# Edit run.sh and change this line:
./grpc-proxy-bin -listen :8081 -backend 192.168.100.1:8080 &

# To your management server address, for example:
# For local development:
./grpc-proxy-bin -listen :8081 -backend localhost:8080 &

# For remote server:
./grpc-proxy-bin -listen :8081 -backend your-management-server.com:443 &
```

### Management URL Configuration

In the browser interface, you'll need to enter your management URL. Common examples:

- **Local development**: `http://localhost:8081` (points to the grpc-proxy)
- **Self-hosted**: `https://your-management-server.com`
- **NetBird Cloud**: `https://api.netbird.io`

## Project Structure

```
wasm_example/
├── README.md         # This file
├── build.sh          # Build script for all components
├── run.sh            # Run script to start everything
├── wasm/             # WASM client source code
│   ├── main.go       # Main WASM entry point
│   └── ws_tcp_proxy.go # TCP proxy implementation
├── grpc-proxy/       # gRPC-to-WebSocket proxy
│   └── main.go       # Proxy server implementation
├── web/              # Web interface files
│   ├── index.html    # Web UI
│   ├── netbird.wasm  # Compiled WASM (generated)
│   └── wasm_exec.js  # Go WASM runtime support
├── test_srv/         # Test HTTP server
│   ├── test_server.go
│   └── run.sh
└── .gitignore        # Git ignore file
```

## TCP Proxy Architecture

The NetBird WASM client includes built-in TCP proxy functionality for SSH and other TCP connections directly from the browser.

### How It Works

1. **JavaScript calls Go function**: The browser calls `netbirdConnectTCP(host, port, protocol)`
2. **Go establishes TCP connection**: Uses NetBird's netstack to connect through the WireGuard tunnel
3. **WebSocket-like interface**: Returns an interface similar to WebSocket for bidirectional communication
4. **Binary data transfer**: Supports both text and binary data transfer

### Implementation Details

- **wasm/ws_tcp_proxy.go**: Core TCP proxy implementation that registers JavaScript functions, handles TCP connections through NetBird, and provides WebSocket-like interface
- **wasm/main.go**: Calls `StartWebSocketTCPProxy()` after NetBird connection is established
- **index.html**: Contains the UI and JavaScript code to call Go functions

## Using the Features

### HTTP Browser
1. After connecting to NetBird, switch to the "Browser" tab
2. Enter a URL like `http://peer-hostname:8080`
3. Click "Go" to fetch and display the content

### SSH Client
1. Switch to the "SSH" tab
2. Enter the peer hostname (e.g., `peer-hostname`)
3. Enter the SSH port (default: 44338 for NetBird SSH)
4. Click "Connect SSH"

### Testing with the Test Server
A test server is included for development:

```bash
# Run the test server on a NetBird peer
cd test_srv
./test_server

# Then access it from the WASM client browser:
http://peer-hostname:8080
```

## Development

### Building Individual Components

If you want to build components separately:

```bash
# Build only the WASM module
GOOS=js GOARCH=wasm go build -tags=devcert -o web/netbird.wasm ./wasm

# Build only the grpc-proxy
go build -o grpc-proxy-bin ./grpc-proxy

# Build only the test server
(cd test_srv && go build -o test_server test_server.go)
```

### Manual Setup

If you prefer to run components manually:

1. **Build everything:**
   ```bash
   ./build.sh
   ```

2. **Start the gRPC proxy:**
   ```bash
   ./grpc-proxy-bin -listen :8081 -backend YOUR_MANAGEMENT_SERVER:PORT &
   ```

3. **Start the web server:**
   ```bash
   cd web && python3 -m http.server 10001
   ```

## Troubleshooting

### Connection Issues
- Verify your setup key is valid
- Check that the management URL is correct
- Ensure the gRPC proxy backend address is configured correctly
- Check browser console for error messages

### Build Issues
- Ensure Go 1.19+ is installed
- Verify you're in the correct directory
- Check that grpc-proxy directory exists with main.go

### SSH Connection Issues
- NetBird SSH uses port 44338 by default (not 22)
- Ensure the NetBird SSH server is running on the target peer
- Check that the peer is connected to the NetBird network
