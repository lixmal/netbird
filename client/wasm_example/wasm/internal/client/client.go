package client

import (
	"sync"
	"syscall/js"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
)

var (
	nbClient    *netbird.Client
	isConnected bool
	mu          sync.RWMutex
)

// SetClient sets the global NetBird client instance
func SetClient(client *netbird.Client) {
	mu.Lock()
	defer mu.Unlock()
	nbClient = client
}

// GetClient returns the global NetBird client instance
func GetClient() *netbird.Client {
	mu.RLock()
	defer mu.RUnlock()
	return nbClient
}

// SetConnected sets the connection status
func SetConnected(connected bool) {
	mu.Lock()
	defer mu.Unlock()
	isConnected = connected
}

// IsConnected returns the connection status
func IsConnected() bool {
	mu.RLock()
	defer mu.RUnlock()
	return isConnected
}

// RegisterControlHandlers registers JavaScript functions for NetBird status and peer management
func RegisterControlHandlers() {
	js.Global().Set("getPeers", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		client := GetClient()
		if client == nil {
			return js.ValueOf("Client not initialized")
		}

		status, err := client.GetStatus()
		if err != nil {
			return js.ValueOf("Failed to get status")
		}

		peerList := make([]interface{}, 0, len(status.Peers))
		for _, peer := range status.Peers {
			peerList = append(peerList, map[string]interface{}{
				"id":        peer.FQDN,
				"ip":        peer.IP,
				"connected": peer.ConnStatus.String() == "Connected",
			})
		}
		return js.ValueOf(peerList)
	}))

	js.Global().Set("getStatus", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		return js.ValueOf(map[string]interface{}{
			"connected": IsConnected(),
		})
	}))
	
	js.Global().Set("netbirdGetStatus", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		promise := js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
			resolve := promiseArgs[0]
			reject := promiseArgs[1]

			go func() {
				client := GetClient()
				if client == nil {
					reject.Invoke("Client not initialized")
					return
				}

				connect := client.GetConnect()
				if connect != nil {
					engine := connect.Engine()
					if engine != nil {
						engine.RunHealthProbes()
					}
				}

				status, err := client.GetStatus()
				if err != nil {
					reject.Invoke(err.Error())
					return
				}

				jsStatus := js.Global().Get("Object").New()
				jsStatus.Set("connected", IsConnected())
				jsStatus.Set("deviceName", "wasm-client")
				jsStatus.Set("managementURL", status.ManagementState.URL)
				jsStatus.Set("netbirdIp", status.LocalPeerState.IP)

				connectedPeers := 0
				totalPeers := len(status.Peers)
				
				jsPeers := js.Global().Get("Array").New()
				peerIndex := 0
				
				for _, peerState := range status.Peers {
					if peerState.ConnStatus.String() == "Connected" {
						connectedPeers++
					}
					
					jsPeer := js.Global().Get("Object").New()
					jsPeer.Set("fqdn", peerState.FQDN)
					jsPeer.Set("ip", peerState.IP)
					jsPeer.Set("connected", peerState.ConnStatus.String() == "Connected")
					jsPeer.Set("connStatus", peerState.ConnStatus.String())
					jsPeer.Set("latency", int64(peerState.Latency/time.Millisecond))
					
					handshakeAge := int64(0)
					if !peerState.LastWireguardHandshake.IsZero() {
						handshakeAge = int64(time.Since(peerState.LastWireguardHandshake).Seconds())
					}
					jsPeer.Set("handshakeAge", handshakeAge)
					
					jsPeer.Set("relayed", peerState.Relayed)
					jsPeer.Set("relayServer", peerState.RelayServerAddress)
					jsPeer.Set("bytesTx", peerState.BytesTx)
					jsPeer.Set("bytesRx", peerState.BytesRx)
					
					connectionUpdateAge := int64(0)
					if !peerState.ConnStatusUpdate.IsZero() {
						connectionUpdateAge = int64(time.Since(peerState.ConnStatusUpdate).Seconds())
					}
					jsPeer.Set("connectionUpdateAge", connectionUpdateAge)
					
					jsPeers.SetIndex(peerIndex, jsPeer)
					peerIndex++
				}
				
				jsStatus.Set("peers", jsPeers)
				jsStatus.Set("connectedPeers", connectedPeers)
				jsStatus.Set("totalPeers", totalPeers)
				jsStatus.Set("status", "Connected")
				
				resolve.Invoke(jsStatus)
			}()

			return nil
		}))
		
		return promise
	}))
}
