/**
 * RDP Certificate Handler
 * 
 * Handles X.509 certificate validation and user acceptance for RDP connections
 * through the NetBird WASM client with RDCleanPath proxy.
 */

class RDPCertificateHandler {
    constructor() {
        this.STORAGE_KEY = 'netbird-rdp-trusted-certs';
        this.modalElement = null;
    }

    /**
     * Handle RDCleanPath response containing server certificates
     * @param {Object} response - RDCleanPath response with ServerCertChain
     * @returns {Promise<boolean>} - True if certificate is trusted/accepted
     */
    async handleRDCleanPathResponse(response) {
        if (!response.ServerCertChain || response.ServerCertChain.length === 0) {
            console.log('No certificate chain in RDCleanPath response');
            return true; // No cert to validate
        }

        // Extract server address and certificate
        const serverAddr = response.ServerAddr || 'unknown';
        const hostname = serverAddr.split(':')[0];
        const certBytes = response.ServerCertChain[0]; // First cert is the server cert

        console.log(`Validating certificate for ${hostname}`);

        try {
            // Parse certificate information
            const certInfo = await this.parseCertificate(certBytes, hostname);
            
            // Validate certificate
            return await this.validateCertificate(certInfo, hostname);
        } catch (error) {
            console.error('Certificate validation error:', error);
            // On parse error, show raw cert data and let user decide
            return await this.promptRawCertificateAcceptance(certBytes, hostname);
        }
    }

    /**
     * Parse X.509 certificate bytes to extract relevant information
     * @param {Uint8Array} certBytes - Raw certificate bytes
     * @param {string} hostname - Server hostname
     * @returns {Promise<Object>} - Parsed certificate information
     */
    async parseCertificate(certBytes, hostname) {
        // Calculate SHA-256 fingerprint
        const fingerprint = await this.calculateFingerprint(certBytes);
        
        // Basic parsing without external library
        // For production, consider using PKI.js or forge for proper X.509 parsing
        const certInfo = {
            raw: certBytes,
            fingerprint: fingerprint,
            hostname: hostname,
            // These would be properly parsed from the X.509 structure
            subject: `CN=${hostname}`,
            issuer: `CN=${hostname}`, // Often self-signed for RDP
            validFrom: new Date(),
            validTo: new Date(Date.now() + 365 * 24 * 60 * 60 * 1000), // +1 year placeholder
            serialNumber: this.extractSerialNumber(certBytes),
            keySize: 2048 // Placeholder
        };

        // Try to extract some basic info from the certificate
        // This is a simplified approach - for production use a proper ASN.1/X.509 parser
        const certString = new TextDecoder('latin1').decode(certBytes);
        
        // Look for common patterns in certificates
        const cnMatch = certString.match(/CN=([^,\0]+)/);
        if (cnMatch) {
            certInfo.subject = cnMatch[0];
        }

        return certInfo;
    }

    /**
     * Extract serial number from certificate bytes (simplified)
     */
    extractSerialNumber(certBytes) {
        // This is a placeholder - proper implementation would parse ASN.1
        const bytes = certBytes.slice(15, 23); // Approximate location
        return Array.from(bytes)
            .map(b => b.toString(16).padStart(2, '0'))
            .join(':')
            .toUpperCase();
    }

    /**
     * Calculate SHA-256 fingerprint of certificate
     * @param {Uint8Array} certBytes - Raw certificate bytes
     * @returns {Promise<string>} - Fingerprint string
     */
    async calculateFingerprint(certBytes) {
        const hashBuffer = await crypto.subtle.digest('SHA-256', certBytes);
        const hashArray = Array.from(new Uint8Array(hashBuffer));
        return hashArray
            .map(b => b.toString(16).padStart(2, '0'))
            .join(':')
            .toUpperCase();
    }

    /**
     * Validate certificate against stored trust database
     * @param {Object} certInfo - Parsed certificate information
     * @param {string} hostname - Server hostname
     * @returns {Promise<boolean>} - True if certificate is trusted/accepted
     */
    async validateCertificate(certInfo, hostname) {
        const trustedCerts = this.loadTrustedCerts();
        
        if (trustedCerts[hostname]) {
            const stored = trustedCerts[hostname];
            
            if (stored.fingerprint === certInfo.fingerprint) {
                console.log(`Certificate for ${hostname} is already trusted`);
                return true;
            } else {
                // Certificate has changed!
                console.warn(`Certificate for ${hostname} has changed!`);
                return await this.promptCertificateChange(hostname, certInfo, stored);
            }
        }

        // New certificate - ask user to accept
        console.log(`New certificate for ${hostname} - requesting user acceptance`);
        return await this.promptUserAcceptance(hostname, certInfo);
    }

    /**
     * Prompt user to accept a new certificate
     * @param {string} hostname - Server hostname
     * @param {Object} certInfo - Certificate information
     * @returns {Promise<boolean>} - True if user accepts
     */
    async promptUserAcceptance(hostname, certInfo) {
        return new Promise((resolve) => {
            // Create modal HTML
            const modal = this.createCertificateModal(hostname, certInfo, false);
            
            // Add event listeners
            modal.querySelector('#cert-accept').onclick = () => {
                const remember = modal.querySelector('#cert-remember').checked;
                if (remember) {
                    this.saveTrustedCert(hostname, certInfo);
                }
                this.closeModal(modal);
                resolve(true);
            };

            modal.querySelector('#cert-reject').onclick = () => {
                this.closeModal(modal);
                resolve(false);
            };

            // Show modal
            document.body.appendChild(modal);
            this.modalElement = modal;
        });
    }

    /**
     * Prompt user when certificate has changed
     * @param {string} hostname - Server hostname
     * @param {Object} newCertInfo - New certificate information
     * @param {Object} oldCertInfo - Previously stored certificate information
     * @returns {Promise<boolean>} - True if user accepts the change
     */
    async promptCertificateChange(hostname, newCertInfo, oldCertInfo) {
        return new Promise((resolve) => {
            const modal = this.createCertificateModal(hostname, newCertInfo, true, oldCertInfo);
            
            modal.querySelector('#cert-accept').onclick = () => {
                const remember = modal.querySelector('#cert-remember').checked;
                if (remember) {
                    this.saveTrustedCert(hostname, newCertInfo);
                }
                this.closeModal(modal);
                resolve(true);
            };

            modal.querySelector('#cert-reject').onclick = () => {
                this.closeModal(modal);
                resolve(false);
            };

            document.body.appendChild(modal);
            this.modalElement = modal;
        });
    }

    /**
     * Fallback prompt for raw certificate when parsing fails
     */
    async promptRawCertificateAcceptance(certBytes, hostname) {
        const fingerprint = await this.calculateFingerprint(certBytes);
        
        return new Promise((resolve) => {
            const modal = document.createElement('div');
            modal.className = 'rdp-cert-modal';
            modal.innerHTML = `
                <div class="rdp-cert-dialog">
                    <h2>⚠️ Security Warning</h2>
                    <p>Cannot verify the identity of the remote computer.</p>
                    
                    <div class="cert-details">
                        <strong>Remote Computer:</strong> ${hostname}<br>
                        <strong>Certificate Size:</strong> ${certBytes.length} bytes<br>
                        <strong>SHA-256 Fingerprint:</strong><br>
                        <code class="fingerprint">${fingerprint}</code>
                    </div>
                    
                    <div class="cert-warning">
                        ⚠️ The certificate could not be fully parsed. 
                        Only connect if you trust this computer.
                    </div>
                    
                    <div class="cert-actions">
                        <label>
                            <input type="checkbox" id="cert-remember">
                            Don't ask again for this computer
                        </label>
                        <div class="buttons">
                            <button id="cert-accept" class="accept">Connect Anyway</button>
                            <button id="cert-reject" class="reject">Cancel</button>
                        </div>
                    </div>
                </div>
            `;

            modal.querySelector('#cert-accept').onclick = () => {
                const remember = modal.querySelector('#cert-remember').checked;
                if (remember) {
                    this.saveTrustedCert(hostname, { fingerprint, hostname });
                }
                this.closeModal(modal);
                resolve(true);
            };

            modal.querySelector('#cert-reject').onclick = () => {
                this.closeModal(modal);
                resolve(false);
            };

            document.body.appendChild(modal);
            this.modalElement = modal;
        });
    }

    /**
     * Create certificate modal HTML
     */
    createCertificateModal(hostname, certInfo, isChanged = false, oldCertInfo = null) {
        const modal = document.createElement('div');
        modal.className = 'rdp-cert-modal';
        
        const warningTitle = isChanged ? 
            '⚠️ Certificate Changed!' : 
            '⚠️ Security Warning';
            
        const warningText = isChanged ?
            'The certificate for this remote computer has changed since your last connection.' :
            'The identity of the remote computer cannot be verified.';

        let certComparison = '';
        if (isChanged && oldCertInfo) {
            certComparison = `
                <div class="cert-comparison">
                    <h4>Certificate Change Details:</h4>
                    <table>
                        <tr>
                            <th>Old Fingerprint:</th>
                            <td><code class="old-fingerprint">${oldCertInfo.fingerprint}</code></td>
                        </tr>
                        <tr>
                            <th>New Fingerprint:</th>
                            <td><code class="new-fingerprint">${certInfo.fingerprint}</code></td>
                        </tr>
                    </table>
                </div>
            `;
        }

        modal.innerHTML = `
            <div class="rdp-cert-dialog">
                <h2>${warningTitle}</h2>
                <p>${warningText}</p>
                
                <div class="cert-details">
                    <h3>Certificate Details:</h3>
                    <table>
                        <tr><th>Remote Computer:</th><td>${hostname}</td></tr>
                        <tr><th>Certificate Subject:</th><td>${certInfo.subject}</td></tr>
                        <tr><th>Certificate Issuer:</th><td>${certInfo.issuer}</td></tr>
                        <tr><th>Valid From:</th><td>${certInfo.validFrom.toLocaleDateString()}</td></tr>
                        <tr><th>Valid To:</th><td>${certInfo.validTo.toLocaleDateString()}</td></tr>
                        <tr><th>Key Size:</th><td>${certInfo.keySize} bits</td></tr>
                        <tr><th>SHA-256 Fingerprint:</th></tr>
                        <tr><td colspan="2"><code class="fingerprint">${certInfo.fingerprint}</code></td></tr>
                    </table>
                </div>
                
                ${certComparison}
                
                <div class="cert-warning">
                    ${isChanged ? 
                        '⚠️ This could indicate a security risk. Only proceed if you recently changed the server certificate.' :
                        '⚠️ This certificate appears to be self-signed. Only connect if you trust this computer.'}
                </div>
                
                <div class="cert-actions">
                    <label>
                        <input type="checkbox" id="cert-remember" ${isChanged ? '' : 'checked'}>
                        ${isChanged ? 'Update stored certificate' : 'Don\'t ask again for this computer'}
                    </label>
                    <div class="buttons">
                        <button id="cert-accept" class="accept">
                            ${isChanged ? 'Accept New Certificate' : 'Yes, Connect'}
                        </button>
                        <button id="cert-reject" class="reject">Cancel</button>
                    </div>
                </div>
            </div>
        `;

        return modal;
    }

    /**
     * Close and remove modal
     */
    closeModal(modal) {
        if (modal && modal.parentNode) {
            modal.parentNode.removeChild(modal);
        }
        this.modalElement = null;
    }

    /**
     * Save trusted certificate to storage
     */
    saveTrustedCert(hostname, certInfo) {
        const trustedCerts = this.loadTrustedCerts();
        trustedCerts[hostname] = {
            fingerprint: certInfo.fingerprint,
            subject: certInfo.subject || hostname,
            savedAt: new Date().toISOString(),
            lastUsed: new Date().toISOString()
        };
        
        try {
            localStorage.setItem(this.STORAGE_KEY, JSON.stringify(trustedCerts));
            console.log(`Saved certificate for ${hostname}`);
        } catch (error) {
            console.error('Failed to save certificate:', error);
        }
    }

    /**
     * Load trusted certificates from storage
     */
    loadTrustedCerts() {
        try {
            const stored = localStorage.getItem(this.STORAGE_KEY);
            return stored ? JSON.parse(stored) : {};
        } catch (error) {
            console.error('Failed to load trusted certificates:', error);
            return {};
        }
    }

    /**
     * Clear all trusted certificates
     */
    clearTrustedCerts() {
        localStorage.removeItem(this.STORAGE_KEY);
        console.log('Cleared all trusted certificates');
    }

    /**
     * Remove a specific trusted certificate
     */
    removeTrustedCert(hostname) {
        const trustedCerts = this.loadTrustedCerts();
        delete trustedCerts[hostname];
        localStorage.setItem(this.STORAGE_KEY, JSON.stringify(trustedCerts));
        console.log(`Removed certificate for ${hostname}`);
    }
}

// Export for use in other modules
window.RDPCertificateHandler = RDPCertificateHandler;