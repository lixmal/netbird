/**
 * IronRDP Input Handler for NetBird WASM Client
 * 
 * This module handles mouse and keyboard input for IronRDP sessions
 */

class IronRDPInputHandler {
    constructor(ironrdp, session, canvas) {
        this.ironrdp = ironrdp;
        this.session = session;
        this.canvas = canvas;
        this.isActive = false;
        
        // Track button states to avoid duplicate events
        this.mouseButtonStates = {
            0: false, // Left button
            1: false, // Middle button
            2: false  // Right button
        };
        
        // Track key states to avoid duplicate events
        this.keyStates = new Map();
        
        // Keyboard scancode mappings (common keys)
        this.keycodeToScancode = {
            // Letters A-Z
            65: 0x1E, 66: 0x30, 67: 0x2E, 68: 0x20, 69: 0x12, 70: 0x21, 71: 0x22, 72: 0x23,
            73: 0x17, 74: 0x24, 75: 0x25, 76: 0x26, 77: 0x32, 78: 0x31, 79: 0x18, 80: 0x19,
            81: 0x10, 82: 0x13, 83: 0x1F, 84: 0x14, 85: 0x16, 86: 0x2F, 87: 0x11, 88: 0x2D,
            89: 0x15, 90: 0x2C,
            
            // Numbers 0-9
            48: 0x0B, 49: 0x02, 50: 0x03, 51: 0x04, 52: 0x05, 53: 0x06, 54: 0x07, 55: 0x08,
            56: 0x09, 57: 0x0A,
            
            // Function keys F1-F12
            112: 0x3B, 113: 0x3C, 114: 0x3D, 115: 0x3E, 116: 0x3F, 117: 0x40,
            118: 0x41, 119: 0x42, 120: 0x43, 121: 0x44, 122: 0x57, 123: 0x58,
            
            // Special keys
            8: 0x0E,   // Backspace
            9: 0x0F,   // Tab
            13: 0x1C,  // Enter
            16: 0x2A,  // Shift (left)
            17: 0x1D,  // Ctrl (left)
            18: 0x38,  // Alt (left)
            20: 0x3A,  // Caps Lock
            27: 0x01,  // Escape
            32: 0x39,  // Space
            33: 0x49,  // Page Up
            34: 0x51,  // Page Down
            35: 0x4F,  // End
            36: 0x47,  // Home
            37: 0x4B,  // Left Arrow
            38: 0x48,  // Up Arrow
            39: 0x4D,  // Right Arrow
            40: 0x50,  // Down Arrow
            45: 0x52,  // Insert
            46: 0x53,  // Delete
            91: 0x5B,  // Windows key (left)
            93: 0x5C,  // Windows key (right)
            
            // Punctuation
            186: 0x27, // Semicolon
            187: 0x0D, // Equals
            188: 0x33, // Comma
            189: 0x0C, // Minus
            190: 0x34, // Period
            191: 0x35, // Forward Slash
            192: 0x29, // Backtick
            219: 0x1A, // Left Bracket
            220: 0x2B, // Backslash
            221: 0x1B, // Right Bracket
            222: 0x28  // Quote
        };
        
        // Mouse button mappings - use direct event.button values like IronRDP does
        // No remapping needed - IronRDP expects the standard JavaScript button codes
        this.mouseButtonMap = {
            0: 0, // Left button
            1: 1, // Middle button
            2: 2  // Right button
        };
        
        this.setupEventListeners();
    }
    
    // Use IronRDP's official coordinate mapping approach with letterbox correction
    getCanvasCoordinates(clientX, clientY) {
        const rect = this.canvas.getBoundingClientRect();
        
        // Calculate the actual rendered size of the canvas content
        const canvasAspectRatio = this.canvas.width / this.canvas.height;
        const containerAspectRatio = rect.width / rect.height;
        
        let renderWidth, renderHeight, offsetX, offsetY;
        
        // Check if we're using object-fit: contain (letterboxing)
        const isFullscreen = document.fullscreenElement === this.canvas || 
                           document.fullscreenElement === this.canvas.parentElement;
        const hasLetterbox = isFullscreen && this.canvas.style.objectFit !== 'fill';
        
        if (hasLetterbox && canvasAspectRatio !== containerAspectRatio) {
            // Calculate actual rendered dimensions with letterboxing
            if (canvasAspectRatio > containerAspectRatio) {
                // Canvas is wider - letterbox on top/bottom
                renderWidth = rect.width;
                renderHeight = rect.width / canvasAspectRatio;
                offsetX = 0;
                offsetY = (rect.height - renderHeight) / 2;
            } else {
                // Canvas is taller - letterbox on left/right
                renderWidth = rect.height * canvasAspectRatio;
                renderHeight = rect.height;
                offsetX = (rect.width - renderWidth) / 2;
                offsetY = 0;
            }
        } else {
            // No letterboxing - canvas fills the entire rect
            renderWidth = rect.width;
            renderHeight = rect.height;
            offsetX = 0;
            offsetY = 0;
        }
        
        // Calculate scale factors based on actual render size
        const scaleX = this.canvas.width / renderWidth;
        const scaleY = this.canvas.height / renderHeight;
        
        // Adjust coordinates for letterbox offset
        const relativeX = clientX - rect.left - offsetX;
        const relativeY = clientY - rect.top - offsetY;
        
        // Clamp to valid canvas area
        const x = Math.max(0, Math.min(this.canvas.width - 1, 
                          Math.round(relativeX * scaleX)));
        const y = Math.max(0, Math.min(this.canvas.height - 1, 
                          Math.round(relativeY * scaleY)));
        
        // Debug logging (commented out for production)
        // console.log(`Coordinate mapping: client(${clientX},${clientY}) -> canvas(${x},${y})`);
        // console.log(`  Letterbox offset: (${offsetX.toFixed(1)},${offsetY.toFixed(1)})`);
        
        return { x, y };
    }
    
    setupEventListeners() {
        // Make canvas focusable
        this.canvas.tabIndex = 1;
        this.canvas.style.outline = 'none';
        
        // Mouse events
        this.canvas.addEventListener('mousedown', this.handleMouseDown.bind(this));
        this.canvas.addEventListener('mouseup', this.handleMouseUp.bind(this));
        this.canvas.addEventListener('mousemove', this.handleMouseMove.bind(this));
        this.canvas.addEventListener('wheel', this.handleWheel.bind(this));
        this.canvas.addEventListener('contextmenu', (e) => e.preventDefault());
        
        // Touch events for mobile support
        this.canvas.addEventListener('touchstart', this.handleTouchStart.bind(this));
        this.canvas.addEventListener('touchmove', this.handleTouchMove.bind(this));
        this.canvas.addEventListener('touchend', this.handleTouchEnd.bind(this));
        
        // Keyboard events
        this.canvas.addEventListener('keydown', this.handleKeyDown.bind(this));
        this.canvas.addEventListener('keyup', this.handleKeyUp.bind(this));
        
        // Focus events
        this.canvas.addEventListener('focus', () => {
            this.isActive = true;
            // Input handler activated
            // Update visual indicator
            if (this.canvas.parentElement) {
                const controls = this.canvas.parentElement.querySelector('#rdpControls');
                if (controls) {
                    controls.style.borderBottom = '2px solid #4CAF50';
                }
            }
        });
        
        this.canvas.addEventListener('blur', () => {
            this.isActive = false;
            this.releaseAllKeys();
            // Input handler deactivated
            // Update visual indicator
            if (this.canvas.parentElement) {
                const controls = this.canvas.parentElement.querySelector('#rdpControls');
                if (controls) {
                    controls.style.borderBottom = 'none';
                }
            }
        });
        
        // Auto-focus canvas when clicked
        this.canvas.addEventListener('click', () => {
            this.canvas.focus();
        });
        
        // Global keyboard shortcuts (when canvas is focused)
        document.addEventListener('keydown', (e) => {
            if (this.isActive) {
                // F11 for fullscreen toggle
                if (e.key === 'F11') {
                    e.preventDefault();
                    if (window.toggleFullscreen) {
                        window.toggleFullscreen();
                    }
                }
                // Ctrl+Alt+Enter for fullscreen toggle
                else if (e.ctrlKey && e.altKey && e.key === 'Enter') {
                    e.preventDefault();
                    if (window.toggleFullscreen) {
                        window.toggleFullscreen();
                    }
                }
            }
        });
    }
    
    handleMouseDown(event) {
        event.preventDefault();
        this.canvas.focus();
        
        // Ensure input handler is active
        if (!this.isActive) {
            this.isActive = true;
            console.log('Input handler activated by mouse down');
        }
        
        console.log('Mouse down event:', event.button, 'isActive:', this.isActive);
        
        const button = this.mouseButtonMap[event.button];
        if (button === undefined) {
            console.log('Unknown mouse button:', event.button);
            return;
        }
        
        console.log('Mouse button mapped:', event.button, '->', button);
        
        if (!this.mouseButtonStates[event.button]) {
            this.mouseButtonStates[event.button] = true;
            console.log('Setting mouse button state to true for button:', event.button);
            
            try {
                if (!this.session || !this.ironrdp) {
                    console.error('Session or IronRDP not available');
                    return;
                }
                
                const deviceEvent = this.ironrdp.DeviceEvent.mouseButtonPressed(button);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                console.log('Successfully sent mouse down for button:', button);
                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending mouse down:', err);
            }
        } else {
            console.log('Mouse button already pressed:', event.button);
        }
    }
    
    handleMouseUp(event) {
        event.preventDefault();
        
        console.log('Mouse up event:', event.button, 'isActive:', this.isActive);
        
        const button = this.mouseButtonMap[event.button];
        if (button === undefined) {
            console.log('Unknown mouse button in up event:', event.button);
            return;
        }
        
        console.log('Mouse up button mapped:', event.button, '->', button);
        
        if (this.mouseButtonStates[event.button]) {
            this.mouseButtonStates[event.button] = false;
            console.log('Setting mouse button state to false for button:', event.button);
            
            try {
                if (!this.session || !this.ironrdp) {
                    console.error('Session or IronRDP not available');
                    return;
                }
                
                const deviceEvent = this.ironrdp.DeviceEvent.mouseButtonReleased(button);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                console.log('Successfully sent mouse up for button:', button);
                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending mouse up:', err);
            }
        } else {
            console.log('Mouse button was not pressed:', event.button);
        }
    }
    
    handleMouseMove(event) {
        if (!this.isActive) return;
        
        const coords = this.getCanvasCoordinates(event.clientX, event.clientY);
        const { x, y } = coords;
        
        try {
            if (!this.session || !this.ironrdp) {
                return;
            }
            
            const deviceEvent = this.ironrdp.DeviceEvent.mouseMove(x, y);
            const transaction = new this.ironrdp.InputTransaction();
            transaction.addEvent(deviceEvent);
            this.session.applyInputs(transaction);
            
            // Don't free objects immediately - let them be garbage collected
            // deviceEvent.free();
            // transaction.free();
        } catch (err) {
            console.error('Error sending mouse move:', err);
        }
    }
    
    handleWheel(event) {
        event.preventDefault();
        if (!this.isActive) return;
        
        // Calculate rotation units (120 units = 1 notch)
        const delta = event.deltaY > 0 ? -1 : 1;
        const rotationUnits = delta * 120;
        
        try {
            if (!this.session || !this.ironrdp) {
                return;
            }
            
            const deviceEvent = this.ironrdp.DeviceEvent.wheelRotations(true, rotationUnits);
            const transaction = new this.ironrdp.InputTransaction();
            transaction.addEvent(deviceEvent);
            this.session.applyInputs(transaction);
            
            // Don't free objects immediately - let them be garbage collected
            // deviceEvent.free();
            // transaction.free();
        } catch (err) {
            console.error('Error sending wheel event:', err);
        }
    }
    
    handleKeyDown(event) {
        if (!this.isActive) return;
        
        // Prevent default for all keys when canvas is focused to avoid browser shortcuts
        event.preventDefault();
        

        
        // Skip if key is already pressed (avoid key repeat)
        if (this.keyStates.get(event.keyCode)) return;
        
        this.keyStates.set(event.keyCode, true);
        
        // Get scancode for the key
        const scancode = this.keycodeToScancode[event.keyCode];
        
        if (scancode !== undefined) {
            try {
                if (!this.session || !this.ironrdp) {
                    console.error('Session or IronRDP not available for scancode:', scancode);
                    return;
                }
                

                
                const deviceEvent = this.ironrdp.DeviceEvent.keyPressed(scancode);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                

                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending key down:', err, 'scancode:', scancode, 'key:', event.key);
            }
        } else if (event.key.length === 1) {
            // For printable characters not in our scancode map, use unicode
            try {
                if (!this.session || !this.ironrdp) {
                    return;
                }
                
                const deviceEvent = this.ironrdp.DeviceEvent.unicodePressed(event.key);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending unicode key:', err);
            }
        }
    }
    
    handleKeyUp(event) {
        if (!this.isActive) return;
        
        // Prevent default for all keys when canvas is focused
        event.preventDefault();
        

        
        // Clear key state
        this.keyStates.delete(event.keyCode);
        
        // Get scancode for the key
        const scancode = this.keycodeToScancode[event.keyCode];
        
        if (scancode !== undefined) {
            try {
                if (!this.session || !this.ironrdp) {
                    console.error('Session or IronRDP not available for key release');
                    return;
                }
                

                
                const deviceEvent = this.ironrdp.DeviceEvent.keyReleased(scancode);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                

                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending key up:', err, 'scancode:', scancode, 'key:', event.key);
            }
        } else if (event.key.length === 1) {
            // For printable characters not in our scancode map, use unicode
            try {
                if (!this.session || !this.ironrdp) {
                    return;
                }
                
                const deviceEvent = this.ironrdp.DeviceEvent.unicodeReleased(event.key);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                
                // Don't free objects immediately - let them be garbage collected
                // deviceEvent.free();
                // transaction.free();
            } catch (err) {
                console.error('Error sending unicode key release:', err);
            }
        }
    }
    
    // Touch support for mobile devices
    handleTouchStart(event) {
        event.preventDefault();
        this.canvas.focus();
        
        if (event.touches.length === 1) {
            const touch = event.touches[0];
            const coords = this.getCanvasCoordinates(touch.clientX, touch.clientY);
            const { x, y } = coords;
            
            // Simulate mouse down
            try {
                const moveEvent = this.ironrdp.DeviceEvent.mouseMove(x, y);
                const clickEvent = this.ironrdp.DeviceEvent.mouseButtonPressed(1);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(moveEvent);
                transaction.addEvent(clickEvent);
                this.session.applyInputs(transaction);
                moveEvent.free();
                clickEvent.free();
                transaction.free();
            } catch (err) {
                console.error('Error handling touch start:', err);
            }
        }
    }
    
    handleTouchMove(event) {
        event.preventDefault();
        
        if (event.touches.length === 1) {
            const touch = event.touches[0];
            const coords = this.getCanvasCoordinates(touch.clientX, touch.clientY);
            const { x, y } = coords;
            
            try {
                const deviceEvent = this.ironrdp.DeviceEvent.mouseMove(x, y);
                const transaction = new this.ironrdp.InputTransaction();
                transaction.addEvent(deviceEvent);
                this.session.applyInputs(transaction);
                deviceEvent.free();
                transaction.free();
            } catch (err) {
                console.error('Error handling touch move:', err);
            }
        }
    }
    
    handleTouchEnd(event) {
        event.preventDefault();
        
        // Simulate mouse up
        try {
            const deviceEvent = this.ironrdp.DeviceEvent.mouseButtonReleased(1);
            const transaction = new this.ironrdp.InputTransaction();
            transaction.addEvent(deviceEvent);
            this.session.applyInputs(transaction);
            deviceEvent.free();
            transaction.free();
        } catch (err) {
            console.error('Error handling touch end:', err);
        }
    }
    
    // Release all pressed keys (useful when losing focus)
    releaseAllKeys() {
        for (const [keyCode, pressed] of this.keyStates.entries()) {
            if (pressed) {
                const scancode = this.keycodeToScancode[keyCode];
                if (scancode !== undefined) {
                    try {
                        const deviceEvent = this.ironrdp.DeviceEvent.keyReleased(scancode);
                        const transaction = new this.ironrdp.InputTransaction();
                        transaction.addEvent(deviceEvent);
                        this.session.applyInputs(transaction);
                        deviceEvent.free();
                        transaction.free();
                    } catch (err) {
                        console.error('Error releasing key:', err);
                    }
                }
            }
        }
        this.keyStates.clear();
        
        // Also release all mouse buttons
        for (let button = 0; button < 3; button++) {
            if (this.mouseButtonStates[button]) {
                const rdpButton = this.mouseButtonMap[button];
                try {
                    const deviceEvent = this.ironrdp.DeviceEvent.mouseButtonReleased(rdpButton);
                    const transaction = new this.ironrdp.InputTransaction();
                    transaction.addEvent(deviceEvent);
                    this.session.applyInputs(transaction);
                    deviceEvent.free();
                    transaction.free();
                } catch (err) {
                    console.error('Error releasing mouse button:', err);
                }
                this.mouseButtonStates[button] = false;
            }
        }
    }
    
    // Clean up event listeners
    destroy() {
        this.releaseAllKeys();
        
        // Remove all event listeners
        this.canvas.removeEventListener('mousedown', this.handleMouseDown);
        this.canvas.removeEventListener('mouseup', this.handleMouseUp);
        this.canvas.removeEventListener('mousemove', this.handleMouseMove);
        this.canvas.removeEventListener('wheel', this.handleWheel);
        this.canvas.removeEventListener('keydown', this.handleKeyDown);
        this.canvas.removeEventListener('keyup', this.handleKeyUp);
        this.canvas.removeEventListener('touchstart', this.handleTouchStart);
        this.canvas.removeEventListener('touchmove', this.handleTouchMove);
        this.canvas.removeEventListener('touchend', this.handleTouchEnd);
        

        
        this.isActive = false;
    }
}

// Export for use in other modules
window.IronRDPInputHandler = IronRDPInputHandler;

// IronRDP Input Handler loaded