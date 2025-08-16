package rdp

import (
	"context"
	"crypto/tls"
	"encoding/asn1"
	"fmt"
	"net"
	"sync"
	"syscall/js"

	log "github.com/sirupsen/logrus"
)

const (
	RDCleanPathVersion = 3390
)

type RDCleanPathPDU struct {
	Version           int64    `asn1:"tag:0,explicit"`
	Error             []byte   `asn1:"tag:1,explicit,optional"`
	Destination       string   `asn1:"utf8,tag:2,explicit,optional"`
	ProxyAuth         string   `asn1:"utf8,tag:3,explicit,optional"`
	ServerAuth        string   `asn1:"utf8,tag:4,explicit,optional"`
	PreconnectionBlob string   `asn1:"utf8,tag:5,explicit,optional"`
	X224ConnectionPDU []byte   `asn1:"tag:6,explicit,optional"`
	ServerCertChain   [][]byte `asn1:"tag:7,explicit,optional"`
	ServerAddr        string   `asn1:"utf8,tag:9,explicit,optional"`
}

type RDCleanPathProxy struct {
	nbClient interface {
		Dial(ctx context.Context, network, address string) (net.Conn, error)
	}
	activeConnections map[string]*proxyConnection
	destinations      map[string]string
	mu                sync.Mutex
}

type proxyConnection struct {
	id          string
	destination string
	rdpConn     net.Conn
	tlsConn     *tls.Conn
	wsHandlers  js.Value
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewRDCleanPathProxy creates a new RDCleanPath proxy
func NewRDCleanPathProxy(client interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
}) *RDCleanPathProxy {
	return &RDCleanPathProxy{
		nbClient:          client,
		activeConnections: make(map[string]*proxyConnection),
	}
}

// Register registers the JavaScript handlers
func (p *RDCleanPathProxy) Register() {
	js.Global().Set("createRDCleanPathProxy", js.FuncOf(p.createProxy))
	js.Global().Set("handleRDCleanPathWebSocket", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 2 {
			log.Error("handleRDCleanPathWebSocket requires WebSocket and proxyID")
			return nil
		}

		ws := args[0]
		proxyID := args[1].String()

		p.HandleWebSocketConnection(ws, proxyID)
		return nil
	}))

	log.Error("RDCleanPath Proxy registered")
}

func (p *RDCleanPathProxy) createProxy(this js.Value, args []js.Value) interface{} {
	var destination string
	if len(args) >= 2 && !args[0].IsNull() && !args[1].IsNull() {
		hostname := args[0].String()
		port := args[1].String()
		destination = fmt.Sprintf("%s:%s", hostname, port)
	} else {
		destination = "win2k19-c2.nb.internal:3389"
		log.Error("createRDCleanPathProxy called without hostname/port, using default destination")
	}

	promise := js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]

		go func() {
			proxyID := fmt.Sprintf("proxy_%d", len(p.activeConnections))

			p.mu.Lock()
			if p.destinations == nil {
				p.destinations = make(map[string]string)
			}
			p.destinations[proxyID] = destination
			p.mu.Unlock()

			proxyURL := fmt.Sprintf("ws://rdcleanpath.proxy.local/%s", proxyID)

			log.Errorf("Created RDCleanPath proxy endpoint: %s for destination: %s", proxyURL, destination)
			resolve.Invoke(proxyURL)
		}()

		return nil
	}))

	return promise
}

// HandleWebSocketConnection handles incoming WebSocket connections from IronRDP
func (p *RDCleanPathProxy) HandleWebSocketConnection(ws js.Value, proxyID string) {
	ctx, cancel := context.WithCancel(context.Background())

	p.mu.Lock()
	destination := p.destinations[proxyID]
	if destination == "" {
		destination = "win2k19-c2.nb.internal:3389"
	}
	p.mu.Unlock()

	conn := &proxyConnection{
		id:          proxyID,
		destination: destination,
		wsHandlers:  ws,
		ctx:         ctx,
		cancel:      cancel,
	}

	p.mu.Lock()
	p.activeConnections[proxyID] = conn
	p.mu.Unlock()

	p.setupWebSocketHandlers(ws, conn)

	log.Errorf("RDCleanPath proxy WebSocket connection established for %s", proxyID)
}

func (p *RDCleanPathProxy) setupWebSocketHandlers(ws js.Value, conn *proxyConnection) {
	ws.Set("onGoMessage", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			return nil
		}

		data := args[0]
		go p.handleWebSocketMessage(conn, data)
		return nil
	}))

	ws.Set("onGoClose", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Error("WebSocket closed by JavaScript")
		conn.cancel()
		return nil
	}))
}

func (p *RDCleanPathProxy) handleWebSocketMessage(conn *proxyConnection, data js.Value) {
	if !data.InstanceOf(js.Global().Get("Uint8Array")) {
		return
	}

	length := data.Get("length").Int()
	bytes := make([]byte, length)
	js.CopyBytesToGo(bytes, data)

	if conn.rdpConn != nil || conn.tlsConn != nil {
		p.forwardToRDP(conn, bytes)
		return
	}

	var pdu RDCleanPathPDU
	_, err := asn1.Unmarshal(bytes, &pdu)
	if err != nil {
		log.Errorf("Failed to parse RDCleanPath PDU: %v", err)
		log.Errorf("First 20 bytes: %x", bytes[:min(len(bytes), 20)])

		if len(bytes) > 0 && bytes[0] == 0x03 {
			log.Error("Received raw RDP packet instead of RDCleanPath PDU")
			go p.handleDirectRDP(conn, bytes)
			return
		}
		return
	}

	go p.processRDCleanPathPDU(conn, pdu)
}

func (p *RDCleanPathProxy) forwardToRDP(conn *proxyConnection, bytes []byte) {
	if conn.tlsConn != nil {
		_, err := conn.tlsConn.Write(bytes)
		if err != nil {
			log.Errorf("Failed to write to TLS: %v", err)
		}
	} else if conn.rdpConn != nil {
		_, err := conn.rdpConn.Write(bytes)
		if err != nil {
			log.Errorf("Failed to write to TCP: %v", err)
		}
	}
}

func (p *RDCleanPathProxy) handleDirectRDP(conn *proxyConnection, firstPacket []byte) {
	defer p.cleanupConnection(conn)

	destination := conn.destination
	log.Errorf("Direct RDP mode: Connecting to %s via NetBird", destination)

	rdpConn, err := p.nbClient.Dial(conn.ctx, "tcp", destination)
	if err != nil {
		log.Errorf("Failed to connect to %s: %v", destination, err)
		return
	}
	conn.rdpConn = rdpConn

	_, err = rdpConn.Write(firstPacket)
	if err != nil {
		log.Errorf("Failed to write first packet: %v", err)
		return
	}

	response := make([]byte, 1024)
	n, err := rdpConn.Read(response)
	if err != nil {
		log.Errorf("Failed to read X.224 response: %v", err)
		return
	}

	p.sendToWebSocket(conn, response[:n])

	go p.forwardWSToTCP(conn)
	go p.forwardTCPToWS(conn)
}

func (p *RDCleanPathProxy) cleanupConnection(conn *proxyConnection) {
	log.Errorf("Cleaning up connection %s", conn.id)
	conn.cancel()
	if conn.tlsConn != nil {
		log.Error("Closing TLS connection")
		conn.tlsConn.Close()
		conn.tlsConn = nil
	}
	if conn.rdpConn != nil {
		log.Error("Closing TCP connection")
		conn.rdpConn.Close()
		conn.rdpConn = nil
	}
	p.mu.Lock()
	delete(p.activeConnections, conn.id)
	p.mu.Unlock()
}

func (p *RDCleanPathProxy) sendToWebSocket(conn *proxyConnection, data []byte) {
	if conn.wsHandlers.Get("receiveFromGo").Truthy() {
		uint8Array := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(uint8Array, data)
		conn.wsHandlers.Call("receiveFromGo", uint8Array.Get("buffer"))
	} else if conn.wsHandlers.Get("send").Truthy() {
		uint8Array := js.Global().Get("Uint8Array").New(len(data))
		js.CopyBytesToJS(uint8Array, data)
		conn.wsHandlers.Call("send", uint8Array.Get("buffer"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
