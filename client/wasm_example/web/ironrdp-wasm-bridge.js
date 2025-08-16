/**
 * IronRDP WASM Bridge for NetBird
 * 
 * This module loads the IronRDP WASM module and bridges it with NetBird's
 * TCP connections. IronRDP handles ALL the RDP protocol complexity.
 */

class IronRDPWASMBridge {
    constructor() {
        this.ironrdp = null;
        this.initialized = false;
        this.sessions = new Map();
    }

    async initialize() {
        if (this.initialized) return;

        try {
            console.log("Loading IronRDP WASM module...");
            
            // Import the IronRDP WASM module
            const ironrdpModule = await import('./ironrdp-pkg/ironrdp_web.js');
            
            // Initialize without auto-loading - let wasm-bindgen handle it
            try {
                await ironrdpModule.default();
            } catch (e) {
                // Try alternative initialization
                if (ironrdpModule.init) {
                    await ironrdpModule.init();
                } else {
                    console.warn("IronRDP init warning:", e);
                    // Module might already be initialized
                }
            }
            
            this.ironrdp = ironrdpModule;
            this.initialized = true;
            
            console.log("IronRDP WASM module loaded successfully");
            console.log("Available IronRDP exports:", Object.keys(ironrdpModule));
            
            // Notify that IronRDP is ready
            if (window.onIronRDPReady) {
                window.onIronRDPReady();
            }
        } catch (error) {
            console.error("Failed to load IronRDP WASM:", error);
            // Don't throw - let it fail gracefully
            this.initialized = false;
        }
    }

    /**
     * Connect to RDP server using IronRDP through NetBird
     */
    async connect(hostname, port, username, password, canvas, enableClipboard = true) {
        if (!this.initialized) {
            await this.initialize();
        }

        const sessionId = `${hostname}:${port}_${Date.now()}`;
        
        try {
            console.log(`IronRDP: Connecting to ${hostname}:${port}...`);
            
            // Check if NetBird is available for direct connection
            // We'll let IronRDP handle the connection through our WebSocket proxy
            if (!window.createNetBirdTCPConnection) {
                console.warn("NetBird TCP connection not available - IronRDP will attempt direct connection");
            }
            
            // Create IronRDP client configuration
            const config = {
                username: username,
                password: password,
                domain: "",
                width: canvas.width || 1024,
                height: canvas.height || 768,
                // Let IronRDP handle all security negotiation
                enable_tls: true,
                enable_credssp: true,
                enable_nla: true
            };
            
            // Create IronRDP SessionBuilder
            const builder = new this.ironrdp.SessionBuilder();
            
            // Configure the session
            builder.username(username)
                   .password(password)
                   .destination(`${hostname}:${port}`);
            
            if (config.domain) {
                builder.serverDomain(config.domain);
            }
            
            // Set desktop size
            const desktopSize = new this.ironrdp.DesktopSize(config.width, config.height);
            builder.desktopSize(desktopSize);
            
            // Set the canvas for rendering
            if (canvas) {
                builder.renderCanvas(canvas);
            }
            
            // Set required callbacks
            builder.setCursorStyleCallback(function(style) {
                console.log('Cursor style changed:', style);
            });
            builder.setCursorStyleCallbackContext(null);
            
            // Set up clipboard callback for RDP → Browser (one-way)
            if (enableClipboard) {
                console.log('Clipboard enabled:', enableClipboard);
                console.log('ClipboardData available:', !!this.ironrdp.ClipboardData);
                
                if (this.ironrdp.ClipboardData) {
                    console.log('Setting up RDP → Browser clipboard support...');
                    
                    // Create callback function references
                    const remoteClipboardCallback = (clipboardData) => {
                        console.log('🔵 RDP clipboard callback triggered!');
                        console.log('Clipboard data object:', clipboardData);
                        this.handleRemoteClipboard(clipboardData);
                    };
                    
                    const forceUpdateCallback = () => {
                        console.log('🔶 RDP server requesting clipboard update (ignored for one-way mode)');
                    };
                    
                    // Handle clipboard data from RDP server
                    const result1 = builder.remoteClipboardChangedCallback(remoteClipboardCallback);
                    console.log('remoteClipboardChangedCallback result:', result1);
                    
                    // Handle forced clipboard update request (we'll just ignore for one-way)  
                    const result2 = builder.forceClipboardUpdateCallback(forceUpdateCallback);
                    console.log('forceClipboardUpdateCallback result:', result2);
                    
                    console.log('✅ Clipboard callbacks registered successfully');
                } else {
                    console.warn('⚠️ ClipboardData class not available in IronRDP module');
                }
            } else {
                console.log('Clipboard disabled by user');
            }
            
            // Always use RDCleanPath proxy - IronRDP requires it
            if (window.createRDCleanPathProxy) {
                console.log("Using RDCleanPath proxy for IronRDP");
                
                // Create a proxy endpoint with hostname and port
                const proxyURL = await window.createRDCleanPathProxy(hostname, port);
                console.log("RDCleanPath proxy URL:", proxyURL);
                
                // Set proxy address for IronRDP (required)
                builder.proxyAddress(proxyURL);
                // Empty auth token for our internal proxy
                builder.authToken("");
            } else {
                throw new Error("RDCleanPath proxy not available - cannot connect");
            }
            
            // Connect
            const session = await builder.connect();
            
            console.log('Session created:', session);
            console.log('Session methods:', Object.getOwnPropertyNames(Object.getPrototypeOf(session)));
            console.log('Session has onClipboardPaste:', typeof session.onClipboardPaste);
            
            this.sessions.set(sessionId, session);
            
            console.log(`IronRDP: Connected to ${hostname}:${port}`);
            
            // Start the session - this begins rendering to the canvas
            console.log('Starting IronRDP session...');
            
            // Set up input handler if canvas is provided
            if (canvas) {
                // Wait for IronRDPInputHandler to be available
                if (window.IronRDPInputHandler) {
                    const inputHandler = new window.IronRDPInputHandler(this.ironrdp, session, canvas);
                    session.inputHandler = inputHandler; // Store reference for cleanup
                    console.log('Input handler attached to RDP session');
                } else {
                    console.warn('IronRDPInputHandler not loaded - input will not work');
                }
            }
            
            session.run().then(termInfo => {
                console.log('IronRDP session terminated:', termInfo.reason());
                // Clean up input handler
                if (session.inputHandler) {
                    session.inputHandler.destroy();
                }
                this.sessions.delete(sessionId);
            }).catch(err => {
                console.error('IronRDP session error:', err);
                // Clean up input handler
                if (session.inputHandler) {
                    session.inputHandler.destroy();
                }
                this.sessions.delete(sessionId);
            });
            
            return sessionId;
            
        } catch (error) {
            console.error(`IronRDP connection failed:`, error);
            
            // If it's an IronError from WASM, try to get more details
            if (error && error.__wbg_ptr) {
                try {
                    // IronError has a backtrace() method
                    if (error.backtrace) {
                        console.error('IronRDP backtrace:', error.backtrace());
                    }
                    // IronError has a kind() method that returns the error type
                    if (error.kind) {
                        const errorKind = error.kind();
                        const errorKindName = ['General', 'WrongPassword', 'LogonFailure', 'AccessDenied', 'RDCleanPath', 'ProxyConnect', 'NegotiationFailure'][errorKind] || 'Unknown';
                        console.error('IronRDP error kind:', errorKindName, `(${errorKind})`);
                    }
                } catch (e) {
                    console.error('Could not extract IronError details:', e);
                }
            }
            
            throw error;
        }
    }

    disconnect(sessionId) {
        const session = this.sessions.get(sessionId);
        if (session) {
            // Clean up input handler first
            if (session.inputHandler) {
                session.inputHandler.destroy();
                session.inputHandler = null;
            }
            
            // Shutdown the session
            if (session.shutdown) {
                session.shutdown();
            }
            
            this.sessions.delete(sessionId);
            console.log(`IronRDP: Disconnected session ${sessionId}`);
        }
    }

    sendInput(sessionId, input) {
        const session = this.sessions.get(sessionId);
        if (session) {
            session.sendInput(input);
        }
    }
    
    /**
     * Handle clipboard data received from RDP server
     * Copies text to browser clipboard (RDP → Browser only)
     */
    handleRemoteClipboard(clipboardData) {
        console.log('📋 handleRemoteClipboard called');
        try {
            // Check if browser supports clipboard API
            if (!navigator.clipboard) {
                console.warn('❌ navigator.clipboard not available');
                return;
            }
            if (!navigator.clipboard.writeText) {
                console.warn('❌ navigator.clipboard.writeText not available');
                return;
            }
            
            console.log('✅ Browser clipboard API available');
            
            // Check if clipboardData has items method
            if (!clipboardData.items) {
                console.error('❌ clipboardData.items() method not found');
                console.log('clipboardData methods:', Object.getOwnPropertyNames(Object.getPrototypeOf(clipboardData)));
                return;
            }
            
            // Get items from clipboard data
            const items = clipboardData.items();
            console.log(`📦 Received ${items.length} clipboard items from RDP`);
            
            if (items.length === 0) {
                console.log('⚠️ No clipboard items received');
                return;
            }
            
            // Look for text/plain data
            for (const item of items) {
                const mimeType = item.mimeType();
                const value = item.value();
                
                console.log(`📄 Clipboard item: ${mimeType}, value type: ${typeof value}`);
                console.log(`📄 Value preview:`, value.substring ? value.substring(0, 100) : value);
                
                if (mimeType === 'text/plain' && typeof value === 'string') {
                    console.log(`📝 Copying text to browser clipboard: "${value.substring(0, 50)}..."`);
                    
                    // Copy text to browser clipboard
                    navigator.clipboard.writeText(value).then(() => {
                        console.log('✅ RDP clipboard successfully copied to browser');
                        
                        // Show visual feedback
                        this.showClipboardNotification('Clipboard updated from RDP');
                    }).catch(err => {
                        console.error('❌ Failed to copy to browser clipboard:', err);
                        // Try fallback method
                        this.fallbackClipboardCopy(value);
                    });
                    
                    return; // Only handle first text item
                } else if (mimeType === 'text/html' && typeof value === 'string') {
                    // Could handle HTML format in future
                    console.log('📄 HTML clipboard data received (not yet supported)');
                }
            }
            
            console.log('⚠️ No text/plain clipboard data found');
        } catch (error) {
            console.error('❌ Error handling RDP clipboard:', error);
            console.error('Stack trace:', error.stack);
        }
    }
    
    /**
     * Fallback clipboard copy method using execCommand
     */
    fallbackClipboardCopy(text) {
        console.log('Trying fallback clipboard method...');
        try {
            const textarea = document.createElement('textarea');
            textarea.value = text;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.select();
            const success = document.execCommand('copy');
            document.body.removeChild(textarea);
            
            if (success) {
                console.log('✅ Fallback clipboard copy succeeded');
                this.showClipboardNotification('Clipboard updated from RDP (fallback)');
            } else {
                console.log('❌ Fallback clipboard copy failed');
            }
        } catch (err) {
            console.error('❌ Fallback clipboard error:', err);
        }
    }
    
    /**
     * Show a temporary notification for clipboard operations
     */
    showClipboardNotification(message) {
        // Check if there's already a notification element
        let notification = document.getElementById('clipboard-notification');
        
        if (!notification) {
            // Create notification element
            notification = document.createElement('div');
            notification.id = 'clipboard-notification';
            notification.style.cssText = `
                position: fixed;
                bottom: 20px;
                right: 20px;
                background: #4CAF50;
                color: white;
                padding: 12px 20px;
                border-radius: 4px;
                box-shadow: 0 2px 5px rgba(0,0,0,0.2);
                z-index: 10000;
                font-family: Arial, sans-serif;
                font-size: 14px;
                opacity: 0;
                transition: opacity 0.3s;
            `;
            document.body.appendChild(notification);
        }
        
        // Update message and show
        notification.textContent = message;
        notification.style.opacity = '1';
        
        // Hide after 2 seconds
        setTimeout(() => {
            notification.style.opacity = '0';
        }, 2000);
    }
}

// Create global instance
window.IronRDPBridge = new IronRDPWASMBridge();

// Don't auto-initialize - let user trigger it when needed
// This avoids conflicts with NetBird WASM module
window.initializeIronRDP = async function() {
    try {
        await window.IronRDPBridge.initialize();
        return true;
    } catch (error) {
        console.error("Failed to initialize IronRDP:", error);
        return false;
    }
};

console.log("IronRDP WASM Bridge loaded");