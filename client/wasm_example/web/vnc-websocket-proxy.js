/**
 * VNC WebSocket Proxy for NetBird
 * This creates a WebSocket that proxies VNC traffic through NetBird
 */

class VNCWebSocketProxy {
    // WebSocket readyState constants
    static CONNECTING = 0;
    static OPEN = 1;
    static CLOSING = 2;
    static CLOSED = 3;
    
    constructor(host, port, protocols) {
        // Instance constants for compatibility
        this.CONNECTING = 0;
        this.OPEN = 1;
        this.CLOSING = 2;
        this.CLOSED = 3;
        
        this.vncHost = host;
        this.vncPort = port;
        this.isNetBirdConnected = false;
        this.readyState = WebSocket.CONNECTING;
        this.binaryType = 'arraybuffer';
        this.protocol = '';  // Will be set when connection opens
        this.url = `ws://vnc-proxy.local/${host}:${port}`;
        this.extensions = '';
        this.bufferedAmount = 0;
        
        // Event target for dispatching events
        this.eventTarget = document.createElement('div');
        
        // Setup NetBird proxy connection
        this.setupNetBirdProxy();
    }
    
    setupNetBirdProxy() {
        // Store reference to this for use in callbacks
        const self = this;
        
        // Wait a moment then establish NetBird connection
        setTimeout(() => {
            if (window.handleVNCWebSocketMessage) {
                // Register this WebSocket with our Go proxy
                window.handleVNCWebSocketMessage(this, this.vncHost, this.vncPort);
                this.isNetBirdConnected = true;
                
                // Give Go time to set up handlers
                setTimeout(() => {
                    // Simulate WebSocket open event
                    this.readyState = WebSocket.OPEN;
                    const openEvent = new Event('open');
                    this.dispatchEvent(openEvent);
                    
                    console.log(`VNC WebSocket proxy established for ${this.vncHost}:${this.vncPort}`);
                }, 200);
            } else {
                console.error('NetBird VNC proxy not available');
                const errorEvent = new Event('error');
                this.dispatchEvent(errorEvent);
                
                const closeEvent = new CloseEvent('close', {
                    code: 1002,
                    reason: 'NetBird VNC proxy not available'
                });
                this.dispatchEvent(closeEvent);
            }
        }, 100);
    }
    
    // Implement WebSocket interface methods
    send(data) {
        if (this.readyState !== WebSocket.OPEN) {
            throw new Error('WebSocket is not open');
        }
        // Forward data to the Go proxy's onmessage handler
        // The Go proxy sets up an onmessage handler to receive data from JavaScript
        if (this.onmessage && typeof this.onmessage === 'function') {
            // Create event object that Go expects
            const event = { data: data };
            // Call the Go-provided onmessage handler
            this.onmessage.call(this, event);
        } else {
            console.error('No onmessage handler set by Go proxy');
        }
    }
    
    close(code, reason) {
        this.readyState = WebSocket.CLOSING;
        // Notify Go proxy to close connection if needed
        setTimeout(() => {
            this.readyState = WebSocket.CLOSED;
            const closeEvent = new CloseEvent('close', { code: code || 1000, reason: reason || '' });
            this.dispatchEvent(closeEvent);
        }, 0);
    }
    
    // Event handling
    addEventListener(type, listener) {
        this.eventTarget.addEventListener(type, listener);
    }
    
    removeEventListener(type, listener) {
        this.eventTarget.removeEventListener(type, listener);
    }
    
    dispatchEvent(event) {
        // Also trigger on* properties
        if (typeof this['on' + event.type] === 'function') {
            this['on' + event.type](event);
        }
        return this.eventTarget.dispatchEvent(event);
    }
    
    // WebSocket event properties
    set onopen(handler) { this._onopen = handler; }
    get onopen() { return this._onopen; }
    
    set onclose(handler) { this._onclose = handler; }
    get onclose() { return this._onclose; }
    
    set onerror(handler) { this._onerror = handler; }
    get onerror() { return this._onerror; }
    
    set onmessage(handler) { this._onmessage = handler; }
    get onmessage() { return this._onmessage; }
}

// Factory function to create VNC WebSocket connections
window.createVNCWebSocket = function(host, port, protocols) {
    return new VNCWebSocketProxy(host, port, protocols);
};