package grpc

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall/js"
	"time"

	"github.com/cenkalti/backoff/v4"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/netbirdio/netbird/util/embeddedroots"
)

// Backoff returns a backoff for grpc calls in WASM
func Backoff(ctx context.Context) backoff.BackOff {
	b := backoff.NewExponentialBackOff()
	b.MaxElapsedTime = 10 * time.Second
	b.Clock = backoff.SystemClock
	return backoff.WithContext(b, ctx)
}

func WithCustomDialer() grpc.DialOption {
	return grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
		// For WASM/JS, we use WebSocket to connect to the gRPC proxy
		// The proxy converts WebSocket to gRPC
		// Route all gRPC connections through our proxy
		wsURL := "ws://localhost:8081"

		// Store the original address to send to proxy
		originalAddr := addr
		log.Infof("WASM gRPC dialer: connecting to %s via WebSocket proxy at %s", originalAddr, wsURL)

		// Create WebSocket connection using JavaScript API
		ws := js.Global().Get("WebSocket").New(wsURL)

		conn := &grpcWebSocketConn{
			ws:       ws,
			address:  addr,
			messages: make(chan []byte, 100),
			closed:   make(chan struct{}),
		}

		// Setup event handlers
		ws.Set("binaryType", "arraybuffer")

		openCh := make(chan struct{})
		errorCh := make(chan error, 1)

		ws.Set("onopen", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			// Send the target address as the first message
			targetMsg := js.Global().Get("Uint8Array").New(len(originalAddr))
			js.CopyBytesToJS(targetMsg, []byte(originalAddr))
			ws.Call("send", targetMsg)
			close(openCh)
			return nil
		}))

		ws.Set("onerror", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			select {
			case errorCh <- errors.New("WebSocket connection failed"):
			default:
			}
			return nil
		}))

		ws.Set("onmessage", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			event := args[0]
			data := event.Get("data")

			// Convert ArrayBuffer to []byte
			uint8Array := js.Global().Get("Uint8Array").New(data)
			length := uint8Array.Get("length").Int()
			bytes := make([]byte, length)
			js.CopyBytesToGo(bytes, uint8Array)

			select {
			case conn.messages <- bytes:
			default:
				log.Warn("gRPC WebSocket message dropped - buffer full")
			}
			return nil
		}))

		ws.Set("onclose", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			close(conn.closed)
			return nil
		}))

		// Wait for connection
		select {
		case <-openCh:
			return conn, nil
		case err := <-errorCh:
			return nil, err
		case <-ctx.Done():
			ws.Call("close")
			return nil, ctx.Err()
		case <-time.After(10 * time.Second):
			ws.Call("close")
			return nil, errors.New("WebSocket connection timeout")
		}
	})
}

// grpcWebSocketConn wraps a JavaScript WebSocket for gRPC connections
type grpcWebSocketConn struct {
	ws       js.Value
	address  string
	messages chan []byte
	readBuf  []byte
	closed   chan struct{}
	once     sync.Once
	mu       sync.Mutex
}

func (c *grpcWebSocketConn) Read(b []byte) (int, error) {
	// Check buffered data
	c.mu.Lock()
	if len(c.readBuf) > 0 {
		n := copy(b, c.readBuf)
		c.readBuf = c.readBuf[n:]
		c.mu.Unlock()
		return n, nil
	}
	c.mu.Unlock()

	// Wait for new message
	select {
	case data := <-c.messages:
		n := copy(b, data)
		if n < len(data) {
			c.mu.Lock()
			c.readBuf = data[n:]
			c.mu.Unlock()
		}
		return n, nil
	case <-c.closed:
		return 0, errors.New("WebSocket closed")
	}
}

func (c *grpcWebSocketConn) Write(b []byte) (int, error) {
	select {
	case <-c.closed:
		return 0, errors.New("WebSocket closed")
	default:
	}

	// Convert []byte to Uint8Array and send
	uint8Array := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(uint8Array, b)

	c.ws.Call("send", uint8Array)
	return len(b), nil
}

func (c *grpcWebSocketConn) Close() error {
	c.once.Do(func() {
		c.ws.Call("close")
	})
	return nil
}

func (c *grpcWebSocketConn) LocalAddr() net.Addr {
	return &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8081}
}

func (c *grpcWebSocketConn) RemoteAddr() net.Addr {
	// Parse the original address
	host, port := "127.0.0.1", 8080
	if idx := strings.LastIndex(c.address, ":"); idx != -1 {
		host = c.address[:idx]
		fmt.Sscanf(c.address[idx+1:], "%d", &port)
	}
	return &net.TCPAddr{IP: net.ParseIP(host), Port: port}
}

func (c *grpcWebSocketConn) SetDeadline(t time.Time) error {
	return nil // Not implemented
}

func (c *grpcWebSocketConn) SetReadDeadline(t time.Time) error {
	return nil // Not implemented
}

func (c *grpcWebSocketConn) SetWriteDeadline(t time.Time) error {
	return nil // Not implemented
}

func CreateConnection(addr string, tlsEnabled bool) (*grpc.ClientConn, error) {
	// For WASM, we need to use grpc-web or WebSocket transport
	// This is a stub that won't work with standard gRPC servers
	// The server needs to support grpc-web or have a proxy

	log.Warnf("WASM gRPC connection attempt to %s - this requires grpc-web proxy or WebSocket support", addr)

	transportOption := grpc.WithTransportCredentials(insecure.NewCredentials())
	if tlsEnabled {
		certPool := embeddedroots.Get()
		transportOption = grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
			RootCAs: certPool,
		}))
	}

	connCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// This will fail in WASM without a proper transport
	conn, err := grpc.DialContext(
		connCtx,
		addr,
		transportOption,
		WithCustomDialer(),
		grpc.WithBlock(),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	)
	if err != nil {
		log.Printf("DialContext error: %v", err)
		return nil, fmt.Errorf("gRPC connections require grpc-web proxy for WASM: %w", err)
	}

	return conn, nil
}
