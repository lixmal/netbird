/**
 * VNC Connection Handler using noVNC
 */

let vncClient = null;
let vncConnectionId = null;
let vncWebSocket = null;

// Load noVNC library dynamically
function loadNoVNCLibrary() {
    return new Promise((resolve, reject) => {
        if (window.RFB) {
            resolve();
            return;
        }
        
        // Check if script already exists
        if (document.querySelector('script[src*="noVNC"]')) {
            // Wait for it to load
            setTimeout(() => {
                if (window.RFB) {
                    resolve();
                } else {
                    reject(new Error('noVNC library failed to load'));
                }
            }, 1000);
            return;
        }
        
        // Load noVNC from CDN
        const script = document.createElement('script');
        script.src = 'https://cdn.jsdelivr.net/npm/@novnc/novnc@latest/core/rfb.js';
        script.type = 'module';
        script.onload = () => {
            // Import RFB class
            import('https://cdn.jsdelivr.net/npm/@novnc/novnc@latest/core/rfb.js')
                .then(module => {
                    window.RFB = module.default;
                    resolve();
                })
                .catch(reject);
        };
        script.onerror = () => reject(new Error('Failed to load noVNC library'));
        document.head.appendChild(script);
    });
}

async function connectVNC() {
    console.log('Attempting VNC connection...');
    
    // Check if NetBird is connected
    if (!window.handleVNCWebSocketMessage) {
        alert('NetBird is not connected. Please connect to NetBird first.');
        console.error('VNC WebSocket proxy function not available');
        return;
    }
    
    const host = document.getElementById('rdpHost').value;
    const port = document.getElementById('rdpPort').value || 5900;
    const password = document.getElementById('rdpPassword').value || '';
    const protocol = document.getElementById('rdpProtocol').value;
    
    if (protocol !== 'vnc') {
        console.log('Not VNC protocol, skipping VNC connection');
        return;
    }
    
    if (!host) {
        alert('Please enter a hostname');
        return;
    }
    
    try {
        // Load noVNC library if not already loaded
        await loadNoVNCLibrary();
        
        // Update UI
        updateVNCStatus('Connecting to VNC server...');
        document.getElementById('rdpConnectBtn').disabled = true;
        
        // Get the canvas element
        const canvas = document.getElementById('rdpCanvas');
        const container = document.getElementById('rdpCanvasContainer');
        
        // Show the canvas container
        container.style.display = 'block';
        document.getElementById('rdpControls').style.display = 'flex';
        
        // Create a unique WebSocket URL for this VNC connection
        // noVNC requires a URL, not a WebSocket object
        const wsProtocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = `${wsProtocol}//vnc-proxy.local/${host}:${port}`;
        
        // Override WebSocket constructor to intercept our special URL
        const originalWebSocket = window.WebSocket;
        window.WebSocket = function(url, protocols) {
            if (url.includes('vnc-proxy.local')) {
                // Return our custom NetBird proxy WebSocket
                return window.createVNCWebSocket(host, parseInt(port), protocols);
            }
            return new originalWebSocket(url, protocols);
        };
        
        // Create noVNC RFB client with the URL
        vncClient = new window.RFB(canvas, wsUrl, {
            credentials: {
                password: password
            }
        });
        
        // Restore original WebSocket constructor
        window.WebSocket = originalWebSocket;
        
        // Configure VNC client settings
        vncClient.scaleViewport = true;
        vncClient.resizeSession = false;
        vncClient.showDotCursor = true;
        
        // Set up VNC event handlers
        vncClient.addEventListener('connect', handleVNCConnect);
        vncClient.addEventListener('disconnect', handleVNCDisconnect);
        vncClient.addEventListener('credentialsrequired', handleVNCCredentials);
        vncClient.addEventListener('securityfailure', handleVNCSecurityFailure);
        vncClient.addEventListener('clipboard', handleVNCClipboard);
        
        // Apply resolution settings
        applyVNCResolution();
        
        console.log('VNC client initialized and connecting...');
        
    } catch (error) {
        console.error('Failed to connect VNC:', error);
        alert('Failed to connect to VNC server: ' + error.message);
        disconnectVNC();
    }
}

function createVNCWebSocketURL(connectionId) {
    // Create a WebSocket proxy URL that will be handled by our Go code
    // Using a custom protocol handler through NetBird
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = protocol + '//vnc-proxy-' + connectionId + '.netbird.local/';
    
    // Create a wrapper WebSocket that will be intercepted by our Go proxy
    return wsUrl;
}

function handleVNCConnect(event) {
    console.log('VNC connected successfully');
    updateVNCStatus('Connected to VNC server');
    
    // Update UI
    document.getElementById('rdpConnectBtn').style.display = 'none';
    document.getElementById('rdpDisconnectBtn').style.display = 'inline-block';
    document.getElementById('rdpResizeBtn').style.display = 'inline-block';
}

function handleVNCDisconnect(event) {
    console.log('VNC disconnected:', event.detail);
    
    if (event.detail.clean) {
        updateVNCStatus('Disconnected from VNC server');
    } else {
        updateVNCStatus('VNC connection lost unexpectedly');
    }
    
    disconnectVNC();
}

function handleVNCCredentials(event) {
    const password = prompt('VNC Password required:');
    if (password) {
        vncClient.sendCredentials({ password: password });
    } else {
        disconnectVNC();
    }
}

function handleVNCSecurityFailure(event) {
    console.error('VNC security failure:', event.detail);
    alert('VNC authentication failed: ' + (event.detail.reason || 'Unknown error'));
    disconnectVNC();
}

function handleVNCClipboard(event) {
    console.log('VNC clipboard event:', event.detail.text);
    // Could implement clipboard synchronization here
}

function updateVNCStatus(message) {
    const statusEl = document.getElementById('rdpStatus');
    const statusText = document.getElementById('rdpStatusText');
    
    if (statusEl && statusText) {
        statusEl.style.display = 'block';
        statusText.textContent = message;
    }
    
    console.log('VNC Status:', message);
}

function applyVNCResolution() {
    if (!vncClient) return;
    
    const resolution = document.getElementById('rdpResolution').value;
    const canvas = document.getElementById('rdpCanvas');
    
    if (resolution === 'auto') {
        // Fit to window
        vncClient.scaleViewport = true;
        vncClient.resizeSession = false;
    } else if (resolution === 'custom') {
        const width = parseInt(document.getElementById('rdpCustomWidth').value) || 1024;
        const height = parseInt(document.getElementById('rdpCustomHeight').value) || 768;
        
        canvas.width = width;
        canvas.height = height;
        
        if (vncClient.resizeSession) {
            vncClient.requestDesktopSize(width, height);
        }
    } else {
        // Predefined resolution
        const [width, height] = resolution.split('x').map(n => parseInt(n));
        
        canvas.width = width;
        canvas.height = height;
        
        if (vncClient.resizeSession) {
            vncClient.requestDesktopSize(width, height);
        }
    }
    
    // Update resolution display
    const resDisplay = document.getElementById('rdpResolutionDisplay');
    if (resDisplay) {
        resDisplay.textContent = canvas.width + 'x' + canvas.height;
    }
}

function disconnectVNC() {
    console.log('Disconnecting VNC...');
    
    // Close VNC client
    if (vncClient) {
        try {
            vncClient.disconnect();
        } catch (e) {
            console.error('Error disconnecting VNC client:', e);
        }
        vncClient = null;
    }
    
    // Close WebSocket
    if (vncWebSocket) {
        try {
            vncWebSocket.close();
        } catch (e) {
            console.error('Error closing WebSocket:', e);
        }
        vncWebSocket = null;
    }
    
    // Close NetBird connection
    if (vncConnectionId && window.closeVNCConnection) {
        window.closeVNCConnection(vncConnectionId);
        vncConnectionId = null;
    }
    
    // Update UI
    document.getElementById('rdpCanvasContainer').style.display = 'none';
    document.getElementById('rdpControls').style.display = 'none';
    document.getElementById('rdpConnectBtn').style.display = 'inline-block';
    document.getElementById('rdpConnectBtn').disabled = false;
    document.getElementById('rdpDisconnectBtn').style.display = 'none';
    document.getElementById('rdpResizeBtn').style.display = 'none';
    
    updateVNCStatus('Disconnected');
}

// VNC-specific keyboard and mouse handling
function sendVNCKey(keysym, down) {
    if (vncClient) {
        vncClient.sendKey(keysym, null, down);
    }
}

function sendVNCCtrlAltDel() {
    if (vncClient) {
        vncClient.sendCtrlAltDel();
    }
}

// Export functions for global use
window.connectVNC = connectVNC;
window.disconnectVNC = disconnectVNC;
window.applyVNCResolution = applyVNCResolution;
window.sendVNCCtrlAltDel = sendVNCCtrlAltDel;