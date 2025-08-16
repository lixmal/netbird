/**
 * Simple Remote Desktop connection handler
 */

async function connectRemoteDesktop() {
    const protocol = document.getElementById('rdpProtocol').value;
    
    // Route to appropriate handler based on protocol
    if (protocol === 'vnc') {
        if (window.connectVNC) {
            return window.connectVNC();
        } else {
            console.error('VNC handler not loaded');
            alert('VNC support not available. Please refresh the page and try again.');
            return;
        }
    }
    
    // Continue with RDP connection
    console.log('Attempting Remote Desktop connection...');
    
    // Check if NetBird is connected first
    if (!window.createNetBirdTCPConnection) {
        alert('NetBird is not connected. Please connect to NetBird first.');
        console.error('NetBird TCP connection function not available');
        return;
    }
    
    // Enable IronRDP mode for WebSocket proxy
    window.ironRDPMode = true;
    
    const host = document.getElementById('rdpHost').value;
    const port = document.getElementById('rdpPort').value || 3389;
    const username = document.getElementById('rdpUsername').value || 'Administrator';
    const password = document.getElementById('rdpPassword').value || '';
    
    if (!host) {
        alert('Please enter a hostname');
        return;
    }
    
    const canvas = document.getElementById('rdpCanvas');
    const statusText = document.getElementById('rdpStatusText');
    
    // Show status and update text
    document.getElementById('rdpStatus').style.display = 'block';
    statusText.textContent = `Connecting to ${host}:${port} via Remote Desktop...`;
    statusText.style.color = '#f68330';
    
    try {
        // Initialize IronRDP if not already done
        if (!window.IronRDPBridge || !window.IronRDPBridge.initialized) {
            console.log('Initializing IronRDP...');
            const success = await window.initializeIronRDP();
            if (!success) {
                throw new Error('Failed to initialize IronRDP');
            }
        }
        
        // Show canvas and controls
        document.getElementById('rdpCanvasContainer').style.display = 'block';
        document.getElementById('rdpControls').style.display = 'flex';
        
        // Get resolution setting
        const resolutionSelect = document.getElementById('rdpResolution');
        const resolutionValue = resolutionSelect ? resolutionSelect.value : 'auto';
        
        let width, height;
        
        if (resolutionValue === 'auto') {
            // Auto - fit to container size
            const container = document.getElementById('rdpCanvasContainer');
            width = Math.min(1920, container.clientWidth || 1024);
            height = Math.min(1080, Math.floor(width * 0.75)); // 4:3 aspect ratio by default
        } else if (resolutionValue === 'custom') {
            // Custom resolution
            const customWidth = document.getElementById('rdpCustomWidth');
            const customHeight = document.getElementById('rdpCustomHeight');
            width = parseInt(customWidth?.value) || 1024;
            height = parseInt(customHeight?.value) || 768;
        } else {
            // Preset resolution (e.g., "1920x1080")
            const [w, h] = resolutionValue.split('x').map(v => parseInt(v));
            width = w || 1024;
            height = h || 768;
        }
        
        // Validate resolution
        width = Math.max(640, Math.min(3840, width));
        height = Math.max(480, Math.min(2160, height));
        
        canvas.width = width;
        canvas.height = height;
        
        console.log(`Setting RDP resolution to ${width}x${height}`);
        
        // Update resolution display
        if (window.updateResolutionDisplay) {
            window.updateResolutionDisplay(width, height);
        }
        
        // Get clipboard setting from UI
        const enableClipboard = document.getElementById('rdpEnableClipboard')?.checked ?? true;
        console.log('Clipboard setting:', enableClipboard);
        
        // Connect using IronRDP
        const sessionId = await window.IronRDPBridge.connect(
            host,
            parseInt(port),
            username,
            password,
            canvas,
            enableClipboard
        );
        
        statusText.textContent = `Connected to ${host}:${port}`;
        statusText.style.color = '#4CAF50';
        
        // Store session for disconnect
        window.currentIronRDPSession = sessionId;
        
        // Update buttons
        document.getElementById('rdpConnectBtn').style.display = 'none';
        document.getElementById('rdpResizeBtn').style.display = 'inline-block';
        document.getElementById('rdpDisconnectBtn').style.display = 'inline-block';
        
        console.log('Remote Desktop connected successfully');
    } catch (error) {
        console.error('Remote Desktop connection failed:', error);
        statusText.textContent = `Connection failed: ${error.message}`;
        statusText.style.color = '#f44336';
        
        // Hide canvas on error
        setTimeout(() => {
            if (statusText.textContent.includes('failed')) {
                document.getElementById('rdpCanvasContainer').style.display = 'none';
            }
        }, 3000);
    }
}

function disconnectRDP() {
    const protocol = document.getElementById('rdpProtocol').value;
    
    // Route to appropriate handler based on protocol
    if (protocol === 'vnc') {
        if (window.disconnectVNC) {
            return window.disconnectVNC();
        }
    }
    
    // Continue with RDP disconnection
    console.log('Disconnecting RDP...');
    
    // Disable IronRDP mode
    window.ironRDPMode = false;
    
    if (window.IronRDPBridge && window.currentIronRDPSession) {
        window.IronRDPBridge.disconnect(window.currentIronRDPSession);
        window.currentIronRDPSession = null;
    }
    
    // Clear canvas
    const canvas = document.getElementById('rdpCanvas');
    const ctx = canvas.getContext('2d');
    ctx.clearRect(0, 0, canvas.width, canvas.height);
    
    // Update UI
    document.getElementById('rdpStatusText').textContent = 'Disconnected';
    document.getElementById('rdpStatusText').style.color = '#757575';
    
    document.getElementById('rdpConnectBtn').style.display = 'inline-block';
    document.getElementById('rdpResizeBtn').style.display = 'none';
    document.getElementById('rdpDisconnectBtn').style.display = 'none';
    document.getElementById('rdpCanvasContainer').style.display = 'none';
}

console.log('Remote Desktop connection handler loaded');