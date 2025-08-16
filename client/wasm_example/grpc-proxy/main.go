package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow all origins for development
		return true
	},
}

type Proxy struct {
	backendAddr string
}

func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if websocket.IsWebSocketUpgrade(r) {
		p.handleWebSocket(w, r)
	} else {
		p.handleHTTP(w, r)
	}
}

func (p *Proxy) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	defer wsConn.Close()

	// Read the first message to get target address
	messageType, data, err := wsConn.ReadMessage()
	if err != nil {
		log.Printf("Failed to read target address: %v", err)
		return
	}
	
	targetAddr := string(data)
	if targetAddr == "" || messageType != websocket.BinaryMessage {
		targetAddr = p.backendAddr
	}
	
	// Map proxy addresses to actual backend
	if targetAddr == "localhost:8081" {
		targetAddr = "192.168.100.1:8080" // Management service
	} else if targetAddr == "192.168.100.1:8082" {
		targetAddr = "192.168.100.1:8082" // Signal service (already correct)
	}
	
	log.Printf("Received target address: %s, routing to: %s", string(data), targetAddr)

	// Connect to backend TCP server
	tcpConn, err := net.Dial("tcp", targetAddr)
	if err != nil {
		log.Printf("Failed to connect to backend %s: %v", targetAddr, err)
		wsConn.WriteMessage(websocket.CloseMessage, []byte{})
		return
	}
	defer tcpConn.Close()

	log.Printf("Proxying WebSocket to %s", targetAddr)

	var wg sync.WaitGroup
	wg.Add(2)

	// WebSocket -> TCP
	go func() {
		defer wg.Done()
		for {
			messageType, data, err := wsConn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					log.Printf("WebSocket read error: %v", err)
				}
				tcpConn.Close()
				return
			}
			if messageType == websocket.BinaryMessage {
				if _, err := tcpConn.Write(data); err != nil {
					log.Printf("TCP write error: %v", err)
					return
				}
			}
		}
	}()

	// TCP -> WebSocket
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := tcpConn.Read(buf)
			if err != nil {
				if err != io.EOF {
					log.Printf("TCP read error: %v", err)
				}
				wsConn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := wsConn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				log.Printf("WebSocket write error: %v", err)
				return
			}
		}
	}()

	wg.Wait()
}

func (p *Proxy) handleHTTP(w http.ResponseWriter, r *http.Request) {
	// Forward HTTP requests directly
	client := &http.Client{}
	
	targetURL := fmt.Sprintf("http://%s%s", p.backendAddr, r.URL.Path)
	if r.URL.RawQuery != "" {
		targetURL += "?" + r.URL.RawQuery
	}

	proxyReq, err := http.NewRequest(r.Method, targetURL, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Copy headers
	for name, values := range r.Header {
		for _, value := range values {
			proxyReq.Header.Add(name, value)
		}
	}

	resp, err := client.Do(proxyReq)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	for name, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(name, value)
		}
	}

	// Add CORS headers for browser access
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "*")

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func main() {
	var (
		listenAddr  = flag.String("listen", ":8081", "Address to listen on")
		backendAddr = flag.String("backend", "192.168.100.1:8080", "Backend server address")
	)
	flag.Parse()

	proxy := &Proxy{backendAddr: *backendAddr}

	log.Printf("Starting WebSocket-to-TCP proxy")
	log.Printf("  Listening on: %s", *listenAddr)
	log.Printf("  Backend: %s", *backendAddr)
	log.Printf("  WebSocket endpoint: ws://localhost%s", *listenAddr)
	log.Printf("  HTTP endpoint: http://localhost%s", *listenAddr)

	if err := http.ListenAndServe(*listenAddr, proxy); err != nil {
		log.Fatal(err)
	}
}