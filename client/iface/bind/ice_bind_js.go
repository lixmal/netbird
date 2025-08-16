//go:build js

package bind

import (
	"net"
	"net/netip"
	"runtime"
	"time"

	log "github.com/sirupsen/logrus"
	"golang.zx2c4.com/wireguard/conn"
)

// GetICEMux returns a dummy UDP mux for WASM since browsers don't support UDP.
func (s *ICEBind) GetICEMux() (*UniversalUDPMuxDefault, error) {
	return nil, nil
}

// Open creates a receive function for handling relay packets in WASM.
func (s *ICEBind) Open(uport uint16) ([]conn.ReceiveFunc, uint16, error) {
	log.Debugf("WASM Open: creating receive function for port %d", uport)

	s.closedMu.Lock()
	s.closed = false
	s.closedMu.Unlock()

	if !s.receiverCreated {
		s.receiverCreated = true
		log.Debugf("WASM Open: first call, setting receiverCreated=true")
	}

	receiveFn := func(bufs [][]byte, sizes []int, eps []conn.Endpoint) (int, error) {
		s.closedMu.Lock()
		if s.closed {
			s.closedMu.Unlock()
			return 0, net.ErrClosed
		}
		s.closedMu.Unlock()

		// Use a shorter timeout for WASM to be more responsive
		timer := time.NewTimer(50 * time.Millisecond)
		defer timer.Stop()

		select {
		case msg, ok := <-s.RecvChan:
			if !ok {
				return 0, net.ErrClosed
			}
			copy(bufs[0], msg.Buffer)
			sizes[0] = len(msg.Buffer)
			eps[0] = conn.Endpoint(msg.Endpoint)
			return 1, nil
		case <-timer.C:
			s.closedMu.Lock()
			if s.closed {
				s.closedMu.Unlock()
				return 0, net.ErrClosed
			}
			s.closedMu.Unlock()

			// In WASM, yielding is important for other goroutines
			runtime.Gosched()
			return 0, nil
		}
	}

	log.Debugf("WASM Open: receive function created, returning port %d", uport)
	return []conn.ReceiveFunc{receiveFn}, uport, nil
}

// SetMark is not applicable in WASM/browser environment.
func (s *ICEBind) SetMark(mark uint32) error {
	// SetMark sets the mark for each packet sent through this Bind.
	// This mark is passed to the kernel as the socket option SO_MARK.
	// In WASM/browser environment, this is not applicable.
	return nil
}

// Send forwards packets through the relay connection for WASM.
func (s *ICEBind) Send(bufs [][]byte, ep conn.Endpoint) error {
	if ep == nil {
		return nil
	}

	fakeIP := ep.DstIP()

	s.endpointsMu.Lock()
	relayConn, ok := s.endpoints[fakeIP]
	s.endpointsMu.Unlock()

	if !ok {
		// In WASM with forced relay, not having a connection is expected during setup
		// Don't log errors to avoid spam during connection establishment
		return nil
	}

	for _, buf := range bufs {
		n, err := relayConn.Write(buf)
		if err != nil {
			// Only log actual write errors, not missing connections
			log.Errorf("WASM Send: failed to write to relay: %v", err)
			return err
		}
		_ = n
	}

	return nil
}

// SetEndpoint stores a relay endpoint for a fake IP.
func (s *ICEBind) SetEndpoint(fakeIP netip.Addr, conn net.Conn) {
	s.endpointsMu.Lock()
	defer s.endpointsMu.Unlock()
	
	// Check if we already have the same connection
	if oldConn, exists := s.endpoints[fakeIP]; exists {
		// Only close and replace if it's actually a different connection
		if oldConn != conn {
			log.Debugf("WASM SetEndpoint: replacing existing connection for %s", fakeIP)
			oldConn.Close()
			s.endpoints[fakeIP] = conn
		} else {
			log.Tracef("WASM SetEndpoint: same connection already set for %s, skipping", fakeIP)
		}
	} else {
		log.Debugf("WASM SetEndpoint: setting new relay connection for fake IP %s", fakeIP)
		s.endpoints[fakeIP] = conn
	}
}

// RemoveEndpoint removes a relay endpoint.
func (s *ICEBind) RemoveEndpoint(fakeIP netip.Addr) {
	s.endpointsMu.Lock()
	defer s.endpointsMu.Unlock()
	delete(s.endpoints, fakeIP)
}

// BatchSize returns the batch size for WASM.
func (s *ICEBind) BatchSize() int {
	return 1
}

// ParseEndpoint parses an endpoint string.
func (s *ICEBind) ParseEndpoint(s2 string) (conn.Endpoint, error) {
	addrPort, err := netip.ParseAddrPort(s2)
	if err != nil {
		log.Errorf("WASM ParseEndpoint: failed to parse %s: %v", s2, err)
		return nil, err
	}
	ep := &Endpoint{AddrPort: addrPort}
	return ep, nil
}

// Close closes the ICEBind.
func (s *ICEBind) Close() error {
	log.Debugf("WASM Close: closing ICEBind (receiverCreated=%v)", s.receiverCreated)

	s.closedMu.Lock()
	s.closed = true
	s.closedMu.Unlock()

	s.receiverCreated = false

	log.Debugf("WASM Close: returning from Close")
	return nil
}
