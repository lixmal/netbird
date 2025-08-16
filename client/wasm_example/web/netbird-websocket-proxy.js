/**
 * NetBird WebSocket Proxy
 * 
 * Creates a fake WebSocket interface that actually uses NetBird TCP connections
 * This allows IronRDP WASM to work through NetBird network
 */

class NetBirdWebSocket extends EventTarget {
    constructor(url) {
        super();
        this.url = url;
        this.readyState = 0; // CONNECTING
        this.binaryType = 'arraybuffer';
        this.bufferedAmount = 0;
        
        // Parse the WebSocket URL to get host and port
        const match = url.match(/wss?:\/\/([^:\/]+):?(\d+)?/);
        if (!match) {
            throw new Error(`Invalid WebSocket URL: ${url}`);
        }
        
        this.hostname = match[1];
        this.port = parseInt(match[2] || '3389');
        
        // Queue for messages before connection is established
        this.messageQueue = [];
        this.tcpConnection = null;
        
        // Start connection
        this._connect();
    }
    
    async _connect() {
        try {
            console.log(`NetBirdWebSocket: Connecting to ${this.hostname}:${this.port} via NetBird`);
            
            // Get NetBird TCP connection
            if (!window.createNetBirdTCPConnection) {
                throw new Error('NetBird TCP connection creator not available');
            }
            
            // Create the TCP connection through NetBird
            this.tcpConnection = await window.createNetBirdTCPConnection(this.hostname, this.port);
            
            // Set up event handlers for the TCP connection
            this.tcpConnection.ondata = (data) => {
                // Convert data to ArrayBuffer if needed
                const event = new MessageEvent('message', {
                    data: data instanceof ArrayBuffer ? data : new Uint8Array(data).buffer
                });
                this.dispatchEvent(event);
                if (this.onmessage) this.onmessage(event);
            };
            
            this.tcpConnection.onclose = () => {
                this.readyState = 3; // CLOSED
                const event = new CloseEvent('close');
                this.dispatchEvent(event);
                if (this.onclose) this.onclose(event);
            };
            
            this.tcpConnection.onerror = (error) => {
                const event = new Event('error');
                event.error = error;
                this.dispatchEvent(event);
                if (this.onerror) this.onerror(event);
            };
            
            // Connection successful
            this.readyState = 1; // OPEN
            const openEvent = new Event('open');
            this.dispatchEvent(openEvent);
            if (this.onopen) this.onopen(openEvent);
            
            // Send any queued messages
            while (this.messageQueue.length > 0) {
                const data = this.messageQueue.shift();
                await this.tcpConnection.send(data);
            }
            
        } catch (error) {
            console.error('NetBirdWebSocket connection failed:', error);
            this.readyState = 3; // CLOSED
            
            const errorEvent = new Event('error');
            errorEvent.error = error;
            this.dispatchEvent(errorEvent);
            if (this.onerror) this.onerror(errorEvent);
            
            const closeEvent = new CloseEvent('close', {
                code: 1006,
                reason: error.message
            });
            this.dispatchEvent(closeEvent);
            if (this.onclose) this.onclose(closeEvent);
        }
    }
    
    send(data) {
        if (this.readyState === 0) {
            // Still connecting, queue the message
            this.messageQueue.push(data);
            return;
        }
        
        if (this.readyState !== 1) {
            throw new Error('WebSocket is not open');
        }
        
        // Send through NetBird TCP connection
        if (this.tcpConnection) {
            // Convert data to proper format if needed
            let sendData = data;
            if (data instanceof Blob) {
                // Convert Blob to ArrayBuffer
                const reader = new FileReader();
                reader.onload = () => {
                    this.tcpConnection.send(new Uint8Array(reader.result));
                };
                reader.readAsArrayBuffer(data);
            } else if (typeof data === 'string') {
                // Convert string to Uint8Array
                const encoder = new TextEncoder();
                this.tcpConnection.send(encoder.encode(data));
            } else if (data instanceof ArrayBuffer) {
                this.tcpConnection.send(new Uint8Array(data));
            } else if (data.buffer instanceof ArrayBuffer) {
                // TypedArray
                this.tcpConnection.send(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
            } else {
                this.tcpConnection.send(data);
            }
        }
    }
    
    close(code = 1000, reason = '') {
        if (this.readyState === 2 || this.readyState === 3) {
            return; // Already closing or closed
        }
        
        this.readyState = 2; // CLOSING
        
        if (this.tcpConnection) {
            this.tcpConnection.close();
        }
        
        // Emit close event
        setTimeout(() => {
            this.readyState = 3; // CLOSED
            const event = new CloseEvent('close', { code, reason });
            this.dispatchEvent(event);
            if (this.onclose) this.onclose(event);
        }, 0);
    }
    
    // WebSocket properties
    get CONNECTING() { return 0; }
    get OPEN() { return 1; }
    get CLOSING() { return 2; }
    get CLOSED() { return 3; }
}

// Override the global WebSocket for RDP connections
const OriginalWebSocket = window.WebSocket;

window.WebSocket = new Proxy(OriginalWebSocket, {
    construct(target, args) {
        const url = args[0];
        if (url && url.includes('netbird-direct')) {
            console.log('Intercepting WebSocket for direct NetBird connection');
            // Extract hostname and port from URL like ws://netbird-direct/hostname:port or ws://netbird-direct/hostname.domain:port
            const match = url.match(/ws:\/\/netbird-direct\/(.+)/);
            if (match) {
                const destination = match[1];
                // Check if port is included or use default RDP port
                let hostname, port;
                const lastColon = destination.lastIndexOf(':');
                if (lastColon !== -1 && /^\d+$/.test(destination.substring(lastColon + 1))) {
                    hostname = destination.substring(0, lastColon);
                    port = destination.substring(lastColon + 1);
                } else {
                    hostname = destination;
                    port = '3389';
                }
                console.log(`Creating NetBird connection to ${hostname}:${port}`);
                return new NetBirdWebSocket(`ws://${hostname}:${port}`);
            }
        }
        
        // Check if this is an RDCleanPath proxy connection
        if (url && url.includes('rdcleanpath.proxy.local')) {
            console.log('Intercepting WebSocket for RDCleanPath proxy');
            if (window.RDCleanPathProxyWebSocket) {
                return new window.RDCleanPathProxyWebSocket(url);
            } else {
                console.error('RDCleanPathProxyWebSocket class not loaded');
                return new target(...args);
            }
        }
        
        // Check if this is a direct RDP connection attempt
        if (url && (url.includes(':3389') || url.includes('rdp') || 
                   (window.ironRDPMode && (url.startsWith('ws://') || url.startsWith('wss://'))))) {
            console.log('Intercepting WebSocket for RDP, using NetBird connection');
            return new NetBirdWebSocket(url);
        }
        
        return new target(...args);
    }
});

console.log('NetBird WebSocket Proxy installed');
