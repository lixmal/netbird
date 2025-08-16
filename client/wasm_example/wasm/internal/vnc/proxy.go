package vnc

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall/js"
	"time"

	log "github.com/sirupsen/logrus"
)

// WebSocketProxy handles VNC connections through WebSocket
type WebSocketProxy struct {
	nbClient interface {
		Dial(ctx context.Context, network, address string) (net.Conn, error)
	}
	mu sync.Mutex
}

// NewWebSocketProxy creates a new VNC WebSocket proxy
func NewWebSocketProxy(nbClient interface{}) *WebSocketProxy {
	client, ok := nbClient.(interface {
		Dial(ctx context.Context, network, address string) (net.Conn, error)
	})
	if !ok {
		log.Error("nbClient does not implement required Dial method")
		return nil
	}

	return &WebSocketProxy{
		nbClient: client,
	}
}

// RegisterJSHandlers registers JavaScript handlers for VNC WebSocket proxy
func (p *WebSocketProxy) RegisterJSHandlers() {
	log.Info("Registering VNC WebSocket proxy handlers")

	js.Global().Set("handleVNCWebSocketMessage", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 3 {
			log.Error("handleVNCWebSocketMessage requires ws, host, port")
			return nil
		}

		ws := args[0]
		host := args[1].String()
		port := args[2].Int()

		go p.handleVNCConnection(ws, host, port)
		return nil
	}))
}

func (p *WebSocketProxy) handleVNCConnection(ws js.Value, host string, port int) {
	address := fmt.Sprintf("%s:%d", host, port)
	log.Infof("Creating VNC connection to %s via WebSocket proxy", address)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := p.nbClient.Dial(ctx, "tcp", address)
	if err != nil {
		log.Errorf("Failed to connect to VNC server at %s: %v", address, err)
		ws.Call("close", 1002, fmt.Sprintf("Failed to connect: %v", err))
		return
	}
	defer conn.Close()

	log.Infof("Connected to VNC server at %s", address)

	ws.Set("binaryType", "arraybuffer")
	ws.Set("readyState", 1)

	done := make(chan struct{})

	p.setupWebSocketHandlers(ws, conn, done)

	go p.forwardVNCToWebSocket(conn, ws, done)

	<-done
	ws.Set("readyState", 3)
	log.Infof("VNC connection to %s closed", address)
}

func (p *WebSocketProxy) setupWebSocketHandlers(ws js.Value, conn net.Conn, done chan struct{}) {
	ws.Set("onmessage", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		event := args[0]
		data := event.Get("data")

		if data.Type() == js.TypeObject {
			uint8Array := js.Global().Get("Uint8Array").New(data)
			length := uint8Array.Get("length").Int()
			bytes := make([]byte, length)
			js.CopyBytesToGo(bytes, uint8Array)

			log.Debugf("Forwarding %d bytes from WebSocket to VNC server", len(bytes))
			if _, err := conn.Write(bytes); err != nil {
				log.Errorf("Failed to write to VNC server: %v", err)
				close(done)
			}
		}
		return nil
	}))

	ws.Set("onclose", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Info("VNC WebSocket closed")
		close(done)
		return nil
	}))

	ws.Set("onerror", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		log.Error("VNC WebSocket error")
		close(done)
		return nil
	}))
}

func (p *WebSocketProxy) forwardVNCToWebSocket(conn net.Conn, ws js.Value, done chan struct{}) {
	buffer := make([]byte, 64*1024)

	for {
		select {
		case <-done:
			return
		default:
			n, err := conn.Read(buffer)
			if err != nil {
				if err != io.EOF {
					log.Errorf("Error reading from VNC server: %v", err)
				}
				close(done)
				return
			}

			if n > 0 {
				p.sendToWebSocket(ws, buffer[:n], done)
			}
		}
	}
}

func (p *WebSocketProxy) sendToWebSocket(ws js.Value, data []byte, done chan struct{}) {
	uint8Array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(uint8Array, data)

	readyStateValue := ws.Get("readyState")
	if readyStateValue.IsUndefined() || readyStateValue.IsNull() {
		log.Warn("WebSocket readyState is undefined, assuming closed")
		close(done)
		return
	}

	readyState := readyStateValue.Int()
	if readyState == 1 {
		messageEvent := js.Global().Get("MessageEvent").New("message", js.ValueOf(map[string]interface{}{
			"data":        uint8Array.Get("buffer"),
			"origin":      "ws://vnc-proxy.local",
			"lastEventId": "",
			"source":      js.Null(),
			"ports":       js.Global().Get("Array").New(),
		}))
		ws.Call("dispatchEvent", messageEvent)
		log.Debugf("Forwarded %d bytes from VNC server to WebSocket", len(data))
	} else {
		log.Warnf("WebSocket not open (state: %d), closing connection", readyState)
		close(done)
	}
}

// RegisterProxy initializes and registers the VNC WebSocket proxy
func RegisterProxy(nbClient interface{}) *WebSocketProxy {
	proxy := NewWebSocketProxy(nbClient)
	if proxy != nil {
		proxy.RegisterJSHandlers()
	}
	return proxy
}
