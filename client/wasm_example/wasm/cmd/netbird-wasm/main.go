package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"syscall/js"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	nbclient "github.com/netbirdio/netbird/client/wasm_example/wasm/internal/client"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/http"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/iperf3"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/rdp"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/ssh"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/tcp"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/vnc"
)

var globalClient *netbird.Client

func main() {
	_ = os.Setenv("NB_FORCE_RELAY", "true")

	js.Global().Set("createNetBirdClient", js.FuncOf(createNetBirdClient))
	js.Global().Set("connectNetBird", js.FuncOf(connectNetBird))
	js.Global().Set("disconnectNetBird", js.FuncOf(disconnectNetBird))
	js.Global().Set("logoutNetBird", js.FuncOf(logoutNetBird))
	js.Global().Set("getNetBirdStatus", js.FuncOf(getNetBirdStatus))

	select {}
}

func startClient(nbClient *netbird.Client) error {
	startErr := make(chan error, 1)
	go func() {
		log.Println("Starting NetBird client...")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := nbClient.Start(ctx); err != nil {
			startErr <- err
			return
		}
		log.Println("NetBird client started successfully in WASM")
		startErr <- nil
	}()

	select {
	case err := <-startErr:
		if err != nil {
			return err
		}
		log.Println("NetBird client started successfully")
		nbclient.SetConnected(true)
		return nil
	case <-time.After(35 * time.Second):
		return fmt.Errorf("timeout after 35 seconds")
	}
}

func registerHandlers(nbClient *netbird.Client) {
	nbclient.SetClient(nbClient)

	http.RegisterHandlers()
	nbclient.RegisterControlHandlers()
	tcp.RegisterProxy(nbClient)
	ssh.RegisterHandlers(nbClient)

	tcpBridge := rdp.NewTCPConnectionBridge(nbClient)
	tcpBridge.Register()

	rdCleanPathProxy := rdp.NewRDCleanPathProxy(nbClient)
	rdCleanPathProxy.Register()

	vnc.RegisterProxy(nbClient)
	iperf3.RegisterHandlers()
}

// JavaScript handler functions for dashboard integration

func createNetBirdClient(this js.Value, args []js.Value) interface{} {
	return js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		if len(args) < 1 {
			reject.Invoke(js.ValueOf("Options object required"))
			return nil
		}

		go func() {
			jsOptions := args[0]

			options := netbird.Options{
				DeviceName: "dashboard-client",
				LogLevel:   "warn",
			}

			if jwtToken := jsOptions.Get("jwtToken"); !jwtToken.IsNull() && !jwtToken.IsUndefined() {
				options.JWTToken = jwtToken.String()
			}

			if setupKey := jsOptions.Get("setupKey"); !setupKey.IsNull() && !setupKey.IsUndefined() {
				options.SetupKey = setupKey.String()
			}

			if mgmtURL := jsOptions.Get("managementURL"); !mgmtURL.IsNull() && !mgmtURL.IsUndefined() {
				mgmtURLStr := mgmtURL.String()
				if mgmtURLStr != "" {
					options.ManagementURL = mgmtURLStr
				}
			}

			if logLevel := jsOptions.Get("logLevel"); !logLevel.IsNull() && !logLevel.IsUndefined() {
				options.LogLevel = logLevel.String()
			}

			if deviceName := jsOptions.Get("deviceName"); !deviceName.IsNull() && !deviceName.IsUndefined() {
				options.DeviceName = deviceName.String()
			}

			if options.JWTToken == "" && options.SetupKey == "" {
				reject.Invoke(js.ValueOf("Either jwtToken or setupKey must be provided"))
				return
			}

			log.Printf("Creating NetBird client with options: deviceName=%s, hasJWT=%v, hasSetupKey=%v, mgmtURL=%s",
				options.DeviceName, options.JWTToken != "", options.SetupKey != "", options.ManagementURL)

			client, err := netbird.New(options)
			if err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}

			if err := startClient(client); err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}

			globalClient = client
			registerHandlers(client)

			log.Println("NetBird client created and connected successfully")
			resolve.Invoke(js.ValueOf(true))
		}()

		return nil
	}))
}

func connectNetBird(this js.Value, args []js.Value) interface{} {
	return js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		resolve := args[0]
		reject := args[1]

		if globalClient == nil {
			reject.Invoke(js.ValueOf("No client created"))
			return nil
		}

		go func() {
			if err := startClient(globalClient); err != nil {
				reject.Invoke(js.ValueOf(err.Error()))
				return
			}

			registerHandlers(globalClient)
			resolve.Invoke(js.ValueOf(true))
		}()

		return nil
	}))
}

func disconnectNetBird(this js.Value, args []js.Value) interface{} {
	if globalClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := globalClient.Stop(ctx); err != nil {
			log.Printf("Error stopping client: %v", err)
		}

		globalClient = nil
		nbclient.SetConnected(false)
		log.Println("NetBird client disconnected")
	}
	return js.ValueOf(true)
}

func logoutNetBird(this js.Value, args []js.Value) interface{} {
	return js.Global().Get("Promise").New(js.FuncOf(func(this js.Value, promiseArgs []js.Value) interface{} {
		resolve := promiseArgs[0]
		reject := promiseArgs[1]

		go func() {
			if globalClient != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				log.Println("Logging out NetBird client...")
				if err := globalClient.Logout(ctx); err != nil {
					log.Printf("Error during logout: %v", err)
					reject.Invoke(js.ValueOf(err.Error()))
					return
				}

				globalClient = nil
				nbclient.SetConnected(false)
				log.Println("NetBird client logged out successfully")
				resolve.Invoke(js.ValueOf(true))
			} else {
				log.Println("No NetBird client to logout")
				resolve.Invoke(js.ValueOf(true))
			}
		}()

		return nil
	}))
}

func getNetBirdStatus(this js.Value, args []js.Value) interface{} {
	status := map[string]interface{}{
		"isConnected": globalClient != nil && nbclient.IsConnected(),
		"hasClient":   globalClient != nil,
	}
	return js.ValueOf(status)
}
