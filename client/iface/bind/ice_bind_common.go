//go:build js

package bind

import (
	"net"
	"net/netip"
	"sync"

	"github.com/netbirdio/netbird/client/iface/wgaddr"
	"github.com/pion/transport/v3"
)

// RecvMessage represents a received message
type RecvMessage struct {
	Endpoint *Endpoint
	Buffer   []byte
}

// ICEBind is a bind implementation that uses ICE candidates for connectivity
type ICEBind struct {
	address          wgaddr.Address
	filterFn         FilterFn
	endpoints        map[netip.Addr]net.Conn
	endpointsMu      sync.Mutex
	udpMux           *UniversalUDPMuxDefault
	muUDPMux         sync.Mutex
	transportNet     transport.Net
	receiverCreated  bool
	activityRecorder *ActivityRecorder
	RecvChan         chan RecvMessage
	closed           bool // Flag to signal that bind is closed
	closedMu         sync.Mutex
}

// NewICEBind creates a new ICEBind instance
func NewICEBind(transportNet transport.Net, filterFn FilterFn, address wgaddr.Address) *ICEBind {
	return &ICEBind{
		address:          address,
		transportNet:     transportNet,
		filterFn:         filterFn,
		endpoints:        make(map[netip.Addr]net.Conn),
		RecvChan:         make(chan RecvMessage, 100),
		activityRecorder: NewActivityRecorder(),
	}
}

// SetFilter updates the filter function
func (s *ICEBind) SetFilter(filter FilterFn) {
	s.filterFn = filter
}

// GetAddress returns the bind address
func (s *ICEBind) GetAddress() wgaddr.Address {
	return s.address
}

// ActivityRecorder returns the activity recorder
func (s *ICEBind) ActivityRecorder() *ActivityRecorder {
	return s.activityRecorder
}
