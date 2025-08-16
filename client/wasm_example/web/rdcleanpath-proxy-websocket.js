/**
 * RDCleanPath Proxy WebSocket Handler
 * 
 * This intercepts WebSocket connections to rdcleanpath.proxy.local
 * and routes them through the Go RDCleanPath proxy
 */

class RDCleanPathProxyWebSocket extends EventTarget {
    constructor(url) {
        super();
        this.url = url;
        this.readyState = 0; // CONNECTING
        this.binaryType = 'arraybuffer';
        this.bufferedAmount = 0;
        
        // Extract proxy ID from URL
        const match = url.match(/ws:\/\/rdcleanpath\.proxy\.local\/(.+)/);
        if (!match) {
            throw new Error(`Invalid RDCleanPath proxy URL: ${url}`);
        }
        
        this.proxyID = match[1];
        
        // Message queue before connection is ready
        this.messageQueue = [];
        
        // Certificate validation - always enable if available
        this.certificateHandler = window.RDPCertificateHandler ? new window.RDPCertificateHandler() : null;
        this.certificateValidated = false;
        
        if (this.certificateHandler) {
            console.log('Certificate handler enabled for RDCleanPath proxy');
        } else {
            console.warn('RDPCertificateHandler not available - certificate validation disabled');
        }
        
        // Start connection process
        this._connect();
    }
    
    async _connect() {
        try {
            console.log(`RDCleanPath Proxy: Connecting via proxy ${this.proxyID}`);
            
            // Check if RDCleanPath proxy handler is available
            if (!window.createRDCleanPathProxy) {
                throw new Error('RDCleanPath proxy handler not available');
            }
            
            // Register this WebSocket with the Go proxy
            // The Go proxy will handle the RDCleanPath protocol
            // We need to pass this WebSocket object to Go's HandleWebSocketConnection
            if (window.handleRDCleanPathWebSocket) {
                window.handleRDCleanPathWebSocket(this, this.proxyID);
            } else {
                // Fallback - just mark as connected
                console.warn('Go RDCleanPath handler not available, using fallback');
            }
            
            // Connection successful
            this.readyState = 1; // OPEN
            const openEvent = new Event('open');
            this.dispatchEvent(openEvent);
            if (this.onopen) this.onopen(openEvent);
            
            // Send any queued messages
            while (this.messageQueue.length > 0) {
                const data = this.messageQueue.shift();
                this._sendInternal(data);
            }
            
        } catch (error) {
            console.error('RDCleanPath Proxy connection failed:', error);
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
        
        this._sendInternal(data);
    }
    
    _sendInternal(data) {
        // This will be called by the Go proxy to receive data
        // The Go proxy will handle the actual send
        if (this.onGoMessage) {
            let sendData;
            if (data instanceof Blob) {
                // Convert Blob to ArrayBuffer
                const reader = new FileReader();
                reader.onload = () => {
                    this.onGoMessage(new Uint8Array(reader.result));
                };
                reader.readAsArrayBuffer(data);
            } else if (typeof data === 'string') {
                // Convert string to Uint8Array
                const encoder = new TextEncoder();
                this.onGoMessage(encoder.encode(data));
            } else if (data instanceof ArrayBuffer) {
                this.onGoMessage(new Uint8Array(data));
            } else if (data.buffer instanceof ArrayBuffer) {
                // TypedArray
                this.onGoMessage(new Uint8Array(data.buffer, data.byteOffset, data.byteLength));
            } else {
                this.onGoMessage(data);
            }
        }
    }
    
    // Called by Go proxy to send data to IronRDP
    async receiveFromGo(data) {
        // Make sure we have proper ArrayBuffer
        const buffer = data instanceof ArrayBuffer ? data : new Uint8Array(data).buffer;
        
        // Try to parse as RDCleanPath PDU to check for certificates
        // Only check once per connection
        if (this.certificateHandler && !this.certificateValidated) {
            console.log('Checking for certificate in RDCleanPath response...');
            try {
                const response = await this.parseRDCleanPathResponse(buffer);
                if (response && response.ServerCertChain && response.ServerCertChain.length > 0) {
                    console.log('RDCleanPath response contains certificate chain:', response);
                    
                    // Validate certificate
                    const trusted = await this.certificateHandler.handleRDCleanPathResponse(response);
                    
                    if (!trusted) {
                        console.error('Certificate not trusted by user - closing connection');
                        this.close(1000, 'Certificate not trusted');
                        return;
                    }
                    
                    console.log('Certificate validated/accepted successfully');
                    this.certificateValidated = true;
                } else {
                    console.log('No certificate chain found in this response');
                }
            } catch (err) {
                // Not a RDCleanPath response or parsing failed - continue normally
                console.debug('RDCleanPath parsing for certificates failed:', err);
            }
        }
        
        const event = new MessageEvent('message', {
            data: buffer
        });
        this.dispatchEvent(event);
        if (this.onmessage) this.onmessage(event);
    }
    
    // Parse RDCleanPath response (simplified ASN.1 DER parsing)
    async parseRDCleanPathResponse(buffer) {
        const bytes = new Uint8Array(buffer);
        
        // Basic ASN.1 DER check - SEQUENCE tag (0x30)
        if (bytes.length < 2 || bytes[0] !== 0x30) {
            return null;
        }
        
        // Parse length (simplified - doesn't handle long form)
        let totalLength = bytes[1];
        let offset = 2;
        
        // Handle long form length if needed
        if (totalLength > 127) {
            const numLengthBytes = totalLength & 0x7F;
            if (bytes.length < 2 + numLengthBytes) return null;
            totalLength = 0;
            for (let i = 0; i < numLengthBytes; i++) {
                totalLength = (totalLength << 8) | bytes[2 + i];
            }
            offset = 2 + numLengthBytes;
        }
        
        // Look for certificate chain field (tag 7)
        // This is a simplified parser - production should use proper ASN.1 library
        let certChain = null;
        let serverAddr = null;
        
        while (offset < bytes.length - 1) {
            const tag = bytes[offset];
            
            // Check for context-specific tags
            if ((tag & 0x80) === 0x80) {  // Context-specific
                const tagNum = tag & 0x1F;
                offset++;
                
                // Parse length
                let fieldLength = bytes[offset];
                offset++;
                
                if (fieldLength > 127) {
                    // Long form - skip for now
                    const numBytes = fieldLength & 0x7F;
                    fieldLength = 0;
                    for (let i = 0; i < numBytes; i++) {
                        fieldLength = (fieldLength << 8) | bytes[offset++];
                    }
                }
                
                // Check tag number
                if (tagNum === 7 && !certChain) {  // ServerCertChain
                    console.log('Found ServerCertChain field at offset', offset - 2);
                    const certChainData = bytes.slice(offset, offset + fieldLength);
                    certChain = this.extractCertificates(certChainData);
                } else if (tagNum === 9 && !serverAddr) {  // ServerAddr
                    console.log('Found ServerAddr field at offset', offset - 2);
                    const addrData = bytes.slice(offset, offset + fieldLength);
                    // Skip UTF8String tag if present (0x0C)
                    const start = addrData[0] === 0x0C ? 2 : 0;
                    serverAddr = new TextDecoder().decode(addrData.slice(start));
                }
                
                offset += fieldLength;
            } else {
                // Not a context tag, skip
                offset++;
            }
            
            // Avoid infinite loops
            if (offset > bytes.length + 100) break;
        }
        
        if (certChain && certChain.length > 0) {
            console.log(`Parsed RDCleanPath with ${certChain.length} certificate(s)`);
            return {
                ServerCertChain: certChain,
                ServerAddr: serverAddr
            };
        }
        
        return null;
    }
    
    // Extract certificates from ASN.1 SEQUENCE
    extractCertificates(data) {
        const certs = [];
        let offset = 0;
        
        // The certificate chain is a SEQUENCE of OCTET STRINGs
        // First check if we have a SEQUENCE
        if (data[0] === 0x30) {
            // Skip SEQUENCE tag and length
            let seqLength = data[1];
            offset = 2;
            if (seqLength > 127) {
                const numBytes = seqLength & 0x7F;
                offset = 2 + numBytes;
            }
        }
        
        while (offset < data.length - 1) {
            const tag = data[offset];
            
            // Look for OCTET STRING tag (0x04)
            if (tag === 0x04) {
                offset++;
                
                // Parse length
                let length = data[offset];
                offset++;
                
                // Handle long form length
                if (length > 127) {
                    const numBytes = length & 0x7F;
                    length = 0;
                    for (let i = 0; i < numBytes && offset < data.length; i++) {
                        length = (length << 8) | data[offset++];
                    }
                }
                
                if (length > 0 && offset + length <= data.length) {
                    const cert = data.slice(offset, offset + length);
                    certs.push(cert);
                    console.log(`Extracted certificate ${certs.length}: ${length} bytes`);
                }
                offset += length;
            } else {
                offset++;
            }
            
            // Safety check
            if (offset > data.length + 100) break;
        }
        
        console.log(`Extracted ${certs.length} certificate(s) from chain`);
        return certs;
    }
    
    // Extract server address from RDCleanPath PDU
    extractServerAddr(bytes) {
        // Look for context-specific tag 9 (0xa9) - ServerAddr
        let offset = 2;
        while (offset < bytes.length - 2) {
            if (bytes[offset] === 0xa9) {
                const length = bytes[offset + 1];
                const addrBytes = bytes.slice(offset + 2, offset + 2 + length);
                // Skip inner tag if present
                const start = addrBytes[0] === 0x0c ? 2 : 0;
                return new TextDecoder().decode(addrBytes.slice(start));
            }
            offset++;
        }
        return null;
    }
    
    close(code = 1000, reason = '') {
        if (this.readyState === 2 || this.readyState === 3) {
            return; // Already closing or closed
        }
        
        this.readyState = 2; // CLOSING
        
        // Notify Go proxy to close
        if (this.onGoClose) {
            this.onGoClose();
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

// Export for use by netbird-websocket-proxy.js
window.RDCleanPathProxyWebSocket = RDCleanPathProxyWebSocket;

console.log('RDCleanPath Proxy WebSocket class available');
