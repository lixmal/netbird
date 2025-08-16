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
    async connect(hostname, port, username, password, canvas) {
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