package tcp

import (
	"context"
	"fmt"
	"io"
	"syscall/js"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	log "github.com/sirupsen/logrus"
)

// WebSocketTCPProxy handles WebSocket to TCP proxying within WASM
type WebSocketTCPProxy struct {
	nbClient *netbird.Client
}

// NewWebSocketTCPProxy creates a new proxy instance
func NewWebSocketTCPProxy(client *netbird.Client) *WebSocketTCPProxy {
	return &WebSocketTCPProxy{
		nbClient: client,
	}
}

// RegisterJSHandlers registers JavaScript functions for WebSocket proxy
func (p *WebSocketTCPProxy) RegisterJSHandlers() {
	js.Global().Set("netbirdConnectTCP", js.FuncOf(p.handleTCPConnection))
	log.Info("WebSocket-to-TCP proxy handlers registered")
}

func (p *WebSocketTCPProxy) handleTCPConnection(this js.Value, args []js.Value) interface{} {
	if len(args) < 3 {
		return js.ValueOf("error: requires host, port, and protocol arguments")
	}

	host := args[0].String()
	port := args[1].Int()
	protocol := args[2].String()

	promiseConstructor := js.Global().Get("Promise")
	return promiseConstructor.New(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]
		reject := args[1]

		go func() {
			err := p.proxyConnection(host, port, protocol, resolve, reject)
			if err != nil {
				reject.Invoke(err.Error())
			}
		}()

		return nil
	}))
}

func (p *WebSocketTCPProxy) proxyConnection(host string, port int, protocol string, resolve, reject js.Value) error {
	targetAddr := fmt.Sprintf("%s:%d", host, port)
	log.Infof("WASM TCP proxy: connecting to %s for %s", targetAddr, protocol)

	timeout := 30 * time.Second
	if protocol == "ssh" {
		log.Infof("SSH connection: using 30s timeout for %s", targetAddr)
	} else if protocol == "rdp" {
		log.Infof("RDP connection: using 30s timeout for %s", targetAddr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	log.Infof("Dialing %s via NetBird network...", targetAddr)
	conn, err := p.nbClient.Dial(ctx, "tcp", targetAddr)
	if err != nil {
		log.Errorf("Failed to connect to %s: %v", targetAddr, err)
		return fmt.Errorf("connection to %s failed: %w", targetAddr, err)
	}
	log.Infof("Successfully connected to %s", targetAddr)

	wsInterface := p.createWebSocketInterface(conn, targetAddr)

	readyChannel := make(chan bool)
	go p.readLoop(conn, wsInterface, targetAddr, readyChannel)

	<-readyChannel

	log.Infof("WebSocket interface ready for %s", targetAddr)
	resolve.Invoke(wsInterface)
	return nil
}

func (p *WebSocketTCPProxy) createWebSocketInterface(conn io.ReadWriteCloser, targetAddr string) js.Value {
	wsInterface := js.Global().Get("Object").Call("create", js.Null())

	wsInterface.Set("send", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if len(args) < 1 {
			return js.ValueOf(false)
		}

		data := args[0]
		var bytes []byte

		if data.Type() == js.TypeString {
			bytes = []byte(data.String())
		} else {
			uint8Array := js.Global().Get("Uint8Array").New(data)
			length := uint8Array.Get("length").Int()
			bytes = make([]byte, length)
			js.CopyBytesToGo(bytes, uint8Array)
		}

		_, err := conn.Write(bytes)
		if err != nil {
			log.Errorf("TCP write error: %v", err)
			return js.ValueOf(false)
		}

		return js.ValueOf(true)
	}))

	wsInterface.Set("close", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		conn.Close()
		return js.Undefined()
	}))

	return wsInterface
}

func (p *WebSocketTCPProxy) readLoop(conn io.ReadWriteCloser, wsInterface js.Value, targetAddr string, readyChannel chan bool) {
	defer conn.Close()
	readyChannel <- true
	buffer := make([]byte, 4096)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Debugf("TCP read error from %s: %v", targetAddr, err)
			}
			if onclose := wsInterface.Get("onclose"); !onclose.IsUndefined() && !onclose.IsNull() {
				onclose.Invoke()
			}
			return
		}

		if onmessage := wsInterface.Get("onmessage"); !onmessage.IsUndefined() && !onmessage.IsNull() {
			uint8Array := js.Global().Get("Uint8Array").New(n)
			js.CopyBytesToJS(uint8Array, buffer[:n])

			event := js.Global().Get("Object").Call("create", js.Null())
			event.Set("data", uint8Array.Get("buffer"))

			onmessage.Invoke(event)
		} else {
			log.Warnf("No onmessage handler set, dropping %d bytes", n)
		}
	}
}

// RegisterProxy initializes the proxy when NetBird is connected
func RegisterProxy(client *netbird.Client) {
	proxy := NewWebSocketTCPProxy(client)
	proxy.RegisterJSHandlers()

	if callback := js.Global().Get("onNetbirdProxyReady"); !callback.IsUndefined() && !callback.IsNull() {
		callback.Invoke()
	}
}
