package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	netbird "github.com/netbirdio/netbird/client/embed"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/client"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/http"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/iperf3"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/rdp"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/ssh"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/tcp"
	"github.com/netbirdio/netbird/client/wasm_example/wasm/internal/vnc"
)

func main() {
	os.Setenv("NB_FORCE_RELAY", "true")

	logLevel := os.Getenv("NB_LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	nbClient, err := netbird.New(netbird.Options{
		DeviceName:    "wasm-client",
		SetupKey:      os.Getenv("NB_SETUP_KEY"),
		ManagementURL: os.Getenv("NB_MANAGEMENT_URL"),
		LogLevel:      logLevel,
	})
	if err != nil {
		log.Fatal(err)
	}

	if err := startClient(nbClient); err != nil {
		log.Fatalf("Failed to start client: %v", err)
	}

	registerHandlers(nbClient)

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
		client.SetConnected(true)
		return nil
	case <-time.After(35 * time.Second):
		return fmt.Errorf("timeout after 35 seconds")
	}
}

func registerHandlers(nbClient *netbird.Client) {
	client.SetClient(nbClient)

	http.RegisterHandlers()
	client.RegisterControlHandlers()
	tcp.RegisterProxy(nbClient)
	ssh.RegisterHandlers(nbClient)

	tcpBridge := rdp.NewTCPConnectionBridge(nbClient)
	tcpBridge.Register()

	rdCleanPathProxy := rdp.NewRDCleanPathProxy(nbClient)
	rdCleanPathProxy.Register()

	vnc.RegisterProxy(nbClient)
	iperf3.RegisterHandlers()
}
