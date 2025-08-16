package rdp

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall/js"

	log "github.com/sirupsen/logrus"
)

// TCPConnectionBridge provides TCP connections to JavaScript through NetBird
type TCPConnectionBridge struct {
	client interface {
		Dial(ctx context.Context, network, address string) (net.Conn, error)
	}
	connections map[string]*tcpConnection
	mu          sync.Mutex
	nextID      int
}

type tcpConnection struct {
	id       string
	conn     io.ReadWriteCloser
	ctx      context.Context
	cancel   context.CancelFunc
	jsObject js.Value
}

// NewTCPConnectionBridge creates a new TCP connection bridge
func NewTCPConnectionBridge(client interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
}) *TCPConnectionBridge {
	return &TCPConnectionBridge{
		client:      client,
		connections: make(map[string]*tcpConnection),
	}
}

// Register registers the JavaScript handlers
func (b *TCPConnectionBridge) Register() {
	js.Global().Set("createNetBirdTCPConnection", js.FuncOf(b.createConnection))
	log.Error("NetBird TCP Connection Bridge registered")
}

func (b *TCPConnectionBridge) createConnection(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		log.Error("createNetBirdTCPConnection requires hostname and port arguments")
		return nil
	}

	hostname := args[0].String()
	port := args[1].Int()
	addr := fmt.Sprintf("%s:%d", hostname, port)

	promise := js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]
		reject := args[1]

		go func() {
			log.Errorf("Creating NetBird TCP connection to %s", addr)

			ctx := context.Background()
			conn, err := b.client.Dial(ctx, "tcp", addr)
			if err != nil {
				log.Errorf("Failed to dial %s: %v", addr, err)
				reject.Invoke(js.Global().Get("Error").New(fmt.Sprintf("Failed to connect: %v", err)))
				return
			}

			b.mu.Lock()
			b.nextID++
			connID := fmt.Sprintf("conn_%d", b.nextID)
			b.mu.Unlock()

			ctx, cancel := context.WithCancel(context.Background())
			tc := &tcpConnection{
				id:     connID,
				conn:   conn,
				ctx:    ctx,
				cancel: cancel,
			}

			jsConn := b.createJSConnection(tc, connID)
			tc.jsObject = jsConn

			b.mu.Lock()
			b.connections[connID] = tc
			b.mu.Unlock()

			go b.readLoop(tc)

			log.Errorf("NetBird TCP connection established to %s", addr)
			resolve.Invoke(jsConn)
		}()

		return nil
	}))

	return promise
}

func (b *TCPConnectionBridge) createJSConnection(tc *tcpConnection, connID string) js.Value {
	jsConn := js.Global().Get("Object").New()
	jsConn.Set("id", connID)
	jsConn.Set("readyState", 1)

	jsConn.Set("send", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			log.Error("send requires data argument")
			return nil
		}

		go b.handleSend(tc, args[0])
		return nil
	}))

	jsConn.Set("close", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		go b.handleClose(tc, connID)
		return nil
	}))

	return jsConn
}

func (b *TCPConnectionBridge) handleSend(tc *tcpConnection, data js.Value) {
	var bytes []byte

	if data.Type() == js.TypeString {
		bytes = []byte(data.String())
	} else if data.InstanceOf(js.Global().Get("Uint8Array")) {
		length := data.Get("length").Int()
		bytes = make([]byte, length)
		js.CopyBytesToGo(bytes, data)
	} else if data.InstanceOf(js.Global().Get("ArrayBuffer")) {
		uint8Array := js.Global().Get("Uint8Array").New(data)
		length := uint8Array.Get("length").Int()
		bytes = make([]byte, length)
		js.CopyBytesToGo(bytes, uint8Array)
	} else {
		log.Errorf("Unsupported data type for send: %v", data.Type())
		return
	}

	log.Errorf("Sending %d bytes through NetBird TCP", len(bytes))
	n, err := tc.conn.Write(bytes)
	if err != nil {
		log.Errorf("Failed to write to connection: %v", err)
		if tc.jsObject.Get("onerror").Truthy() {
			tc.jsObject.Get("onerror").Invoke(err.Error())
		}
	} else if n != len(bytes) {
		log.Errorf("Partial write: only sent %d of %d bytes", n, len(bytes))
	}
}

func (b *TCPConnectionBridge) handleClose(tc *tcpConnection, connID string) {
	log.Error("Closing NetBird TCP connection")
	tc.cancel()
	tc.conn.Close()

	b.mu.Lock()
	delete(b.connections, connID)
	b.mu.Unlock()

	if tc.jsObject.Get("onclose").Truthy() {
		tc.jsObject.Get("onclose").Invoke()
	}
}

func (b *TCPConnectionBridge) readLoop(tc *tcpConnection) {
	buffer := make([]byte, 32*1024)

	for {
		select {
		case <-tc.ctx.Done():
			return
		default:
			n, err := tc.conn.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Errorf("Read error: %v", err)
				}

				if tc.jsObject.Get("onclose").Truthy() {
					tc.jsObject.Get("onclose").Invoke()
				}

				tc.cancel()
				tc.conn.Close()

				b.mu.Lock()
				delete(b.connections, tc.id)
				b.mu.Unlock()
				return
			}

			if n > 0 {
				uint8Array := js.Global().Get("Uint8Array").New(n)
				js.CopyBytesToJS(uint8Array, buffer[:n])

				if tc.jsObject.Get("ondata").Truthy() {
					tc.jsObject.Get("ondata").Invoke(uint8Array.Get("buffer"))
				}
			}
		}
	}
}

// readLengthPrefixed reads length-prefixed messages (if needed for RDP)
func readLengthPrefixed(conn io.Reader) ([]byte, error) {
	var length uint32
	if err := binary.Read(conn, binary.BigEndian, &length); err != nil {
		return nil, err
	}

	if length > 1024*1024 {
		return nil, fmt.Errorf("message too large: %d bytes", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(conn, data); err != nil {
		return nil, err
	}

	return data, nil
}
