package forwarder

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tcpProxyTestHelper wraps the proxy logic for testing without gVisor dependencies
type tcpProxyTestHelper struct {
	errChan chan error
}

// proxyConnections proxies data between two connections using the current implementation
func (h *tcpProxyTestHelper) proxyConnections(ctx context.Context, client, server net.Conn) (clientToServer, serverToClient int64, errs []error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	var (
		bytesClientToServer int64
		bytesServerToClient int64
		errClientToServer   error
		errServerToClient   error
	)

	// client -> server
	go func() {
		defer wg.Done()
		bytesClientToServer, errClientToServer = io.Copy(server, client)
		cancel()
	}()

	// server -> client
	go func() {
		defer wg.Done()
		bytesServerToClient, errServerToClient = io.Copy(client, server)
		cancel()
	}()

	// Cleanup goroutine
	go func() {
		<-ctx.Done()
		client.Close()
		server.Close()
	}()

	wg.Wait()

	if errClientToServer != nil {
		errs = append(errs, errClientToServer)
	}
	if errServerToClient != nil {
		errs = append(errs, errServerToClient)
	}

	return bytesClientToServer, bytesServerToClient, errs
}

// proxyConnectionsHalfClose proxies using half-close semantics (Tailscale style)
func (h *tcpProxyTestHelper) proxyConnectionsHalfClose(ctx context.Context, client, server net.Conn) (clientToServer, serverToClient int64, errs []error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(2)

	var (
		bytesClientToServer int64
		bytesServerToClient int64
		errClientToServer   error
		errServerToClient   error
	)

	clientTCP, clientIsTCP := client.(*net.TCPConn)
	serverTCP, serverIsTCP := server.(*net.TCPConn)

	// client -> server
	go func() {
		defer wg.Done()
		bytesClientToServer, errClientToServer = io.Copy(server, client)

		// Half-close: signal EOF to server, stop reading from client
		if serverIsTCP {
			serverTCP.CloseWrite()
		}
		if clientIsTCP {
			clientTCP.CloseRead()
		}
	}()

	// server -> client
	go func() {
		defer wg.Done()
		bytesServerToClient, errServerToClient = io.Copy(client, server)

		// Half-close: signal EOF to client, stop reading from server
		if clientIsTCP {
			clientTCP.CloseWrite()
		}
		if serverIsTCP {
			serverTCP.CloseRead()
		}
	}()

	wg.Wait()
	cancel()

	if errClientToServer != nil {
		errs = append(errs, errClientToServer)
	}
	if errServerToClient != nil {
		errs = append(errs, errServerToClient)
	}

	return bytesClientToServer, bytesServerToClient, errs
}

func TestTCPProxy_BidirectionalTransfer(t *testing.T) {
	// Create a simple echo server that also sends initial data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	serverData := []byte("hello from server")
	clientData := []byte("hello from client")

	// Server goroutine
	serverDone := make(chan struct{})
	var serverReceived []byte
	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Send data to client
		conn.Write(serverData)

		// Read data from client
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		serverReceived = buf[:n]

		// Close write side to signal EOF
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.CloseWrite()
		}
	}()

	// Connect as client
	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)

	// Send data to server
	_, err = clientConn.Write(clientData)
	require.NoError(t, err)

	// Close write side
	clientConn.(*net.TCPConn).CloseWrite()

	// Read data from server
	received, err := io.ReadAll(clientConn)
	require.NoError(t, err)
	clientConn.Close()

	<-serverDone

	assert.Equal(t, serverData, received, "client should receive server data")
	assert.Equal(t, clientData, serverReceived, "server should receive client data")
}

func TestTCPProxy_HalfClose_ServerClosesFirst(t *testing.T) {
	// Test that when server closes its write side, client can still send data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	serverDone := make(chan struct{})
	var serverReceived []byte

	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		tc := conn.(*net.TCPConn)

		// Server immediately closes write (sends FIN)
		tc.CloseWrite()

		// But continues reading
		buf := make([]byte, 1024)
		n, _ := conn.Read(buf)
		serverReceived = buf[:n]
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	tc := clientConn.(*net.TCPConn)

	// Client should be able to read EOF from server
	buf := make([]byte, 1024)
	n, err := tc.Read(buf)
	assert.Equal(t, 0, n)
	assert.Equal(t, io.EOF, err)

	// Client should still be able to send data
	clientData := []byte("data after server close")
	_, err = tc.Write(clientData)
	require.NoError(t, err)
	tc.CloseWrite()

	<-serverDone
	assert.Equal(t, clientData, serverReceived)
}

func TestTCPProxy_HalfClose_ClientClosesFirst(t *testing.T) {
	// Test that when client closes its write side, server can still send data
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	serverDone := make(chan struct{})
	serverData := []byte("data after client close")

	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		tc := conn.(*net.TCPConn)

		// Read until EOF (client closed write)
		buf := make([]byte, 1024)
		_, err = tc.Read(buf)
		if err == io.EOF {
			// Now send data back
			tc.Write(serverData)
		}
		tc.CloseWrite()
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	tc := clientConn.(*net.TCPConn)

	// Client closes write immediately
	tc.CloseWrite()

	// Client should still receive data from server
	received, err := io.ReadAll(tc)
	require.NoError(t, err)

	<-serverDone
	assert.Equal(t, serverData, received)
}

func TestTCPProxy_ServerReset(t *testing.T) {
	// Test behavior when server sends RST
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()

	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := listener.Accept()
		if err != nil {
			return
		}

		// Set linger to 0 to force RST on close
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0)
		}

		// Read some data first
		buf := make([]byte, 1024)
		conn.Read(buf)

		// Close immediately - this sends RST due to linger=0
		conn.Close()
	}()

	clientConn, err := net.Dial("tcp", listener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	// Send some data
	clientConn.Write([]byte("hello"))

	// Give server time to RST
	time.Sleep(50 * time.Millisecond)

	// Try to read - should get connection reset error
	buf := make([]byte, 1024)
	_, err = clientConn.Read(buf)

	<-serverDone

	// Should get a connection reset error (exact error varies by OS)
	// On Linux/Unix: connection reset by peer
	// On Windows: wsarecv: An existing connection was forcibly closed
	if err != nil {
		isReset := errors.Is(err, syscall.ECONNRESET) || errors.Is(err, io.EOF)
		assert.True(t, isReset, "expected connection reset or EOF, got: %v", err)
	}
}

func TestTCPProxy_ProxyWithCurrentImpl(t *testing.T) {
	// Test the current proxy implementation behavior
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backendListener.Close()

	// Backend echo server
	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo all data back
		io.Copy(conn, conn)
	}()

	// Create proxy connections
	clientConn, proxyClientSide := net.Pipe()
	proxyServerSide, err := net.Dial("tcp", backendListener.Addr().String())
	require.NoError(t, err)

	helper := &tcpProxyTestHelper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start proxy
	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		helper.proxyConnections(ctx, proxyClientSide, proxyServerSide)
	}()

	// Client sends data
	testData := []byte("test data for proxy")
	_, err = clientConn.Write(testData)
	require.NoError(t, err)

	// Read echoed data
	buf := make([]byte, len(testData))
	_, err = io.ReadFull(clientConn, buf)
	require.NoError(t, err)
	assert.Equal(t, testData, buf)

	clientConn.Close()
	<-proxyDone
	<-backendDone
}

func TestTCPProxy_ProxyWithHalfClose(t *testing.T) {
	// Test the half-close proxy implementation
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backendListener.Close()

	// Backend server that sends response then closes
	backendData := []byte("response from backend")
	backendDone := make(chan struct{})
	var backendReceived []byte

	go func() {
		defer close(backendDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		tc := conn.(*net.TCPConn)

		// Read client data
		buf := make([]byte, 1024)
		n, _ := tc.Read(buf)
		backendReceived = buf[:n]

		// Send response
		tc.Write(backendData)

		// Close write side
		tc.CloseWrite()
	}()

	// Simulate proxy setup with real TCP connections
	proxyToBackend, err := net.Dial("tcp", backendListener.Addr().String())
	require.NoError(t, err)

	// Create a listener for the "client" side of proxy
	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer proxyListener.Close()

	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		proxyToClient, err := proxyListener.Accept()
		if err != nil {
			return
		}
		defer proxyToClient.Close()

		helper := &tcpProxyTestHelper{}
		ctx := context.Background()
		helper.proxyConnectionsHalfClose(ctx, proxyToClient, proxyToBackend)
	}()

	// Connect as client
	clientConn, err := net.Dial("tcp", proxyListener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	tc := clientConn.(*net.TCPConn)

	// Send data to backend via proxy
	clientData := []byte("request to backend")
	_, err = tc.Write(clientData)
	require.NoError(t, err)
	tc.CloseWrite()

	// Read response from backend via proxy
	received, err := io.ReadAll(tc)
	require.NoError(t, err)

	<-backendDone
	<-proxyDone

	assert.Equal(t, clientData, backendReceived, "backend should receive client data")
	assert.Equal(t, backendData, received, "client should receive backend response")
}

func TestTCPProxy_ProxyServerReset_CurrentImpl(t *testing.T) {
	// Test current implementation when backend sends RST
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backendListener.Close()

	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}

		// Set linger to 0 to send RST
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetLinger(0)
		}

		// Read something then RST
		buf := make([]byte, 1024)
		conn.Read(buf)
		conn.Close() // Sends RST due to linger=0
	}()

	proxyToBackend, err := net.Dial("tcp", backendListener.Addr().String())
	require.NoError(t, err)

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer proxyListener.Close()

	var proxyErrors []error
	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		proxyToClient, err := proxyListener.Accept()
		if err != nil {
			return
		}
		defer proxyToClient.Close()

		helper := &tcpProxyTestHelper{}
		ctx := context.Background()
		_, _, proxyErrors = helper.proxyConnections(ctx, proxyToClient, proxyToBackend)
	}()

	clientConn, err := net.Dial("tcp", proxyListener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	// Send data
	clientConn.Write([]byte("trigger backend"))

	// Wait for connections to close
	time.Sleep(100 * time.Millisecond)

	// Try to read - connection should be closed
	buf := make([]byte, 1024)
	_, readErr := clientConn.Read(buf)

	<-backendDone
	<-proxyDone

	// Current implementation: client gets EOF or error because proxy closed the connection
	// The RST from backend is not propagated - client sees graceful close or broken pipe
	t.Logf("Proxy errors: %v", proxyErrors)
	t.Logf("Client read error: %v", readErr)

	// Document current behavior - client doesn't see RST, sees EOF or error
	assert.True(t, readErr == io.EOF || readErr != nil,
		"client should see connection closed (got: %v)", readErr)
}

func TestTCPProxy_LongLivedConnection(t *testing.T) {
	// Test a long-lived connection with multiple exchanges
	backendListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer backendListener.Close()

	backendDone := make(chan struct{})
	go func() {
		defer close(backendDone)
		conn, err := backendListener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		// Echo server
		buf := make([]byte, 1024)
		for {
			n, err := conn.Read(buf)
			if err != nil {
				return
			}
			conn.Write(buf[:n])
		}
	}()

	proxyToBackend, err := net.Dial("tcp", backendListener.Addr().String())
	require.NoError(t, err)

	proxyListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer proxyListener.Close()

	proxyDone := make(chan struct{})
	go func() {
		defer close(proxyDone)
		proxyToClient, err := proxyListener.Accept()
		if err != nil {
			return
		}
		defer proxyToClient.Close()

		helper := &tcpProxyTestHelper{}
		ctx := context.Background()
		helper.proxyConnectionsHalfClose(ctx, proxyToClient, proxyToBackend)
	}()

	clientConn, err := net.Dial("tcp", proxyListener.Addr().String())
	require.NoError(t, err)
	defer clientConn.Close()

	// Multiple request/response exchanges
	for i := 0; i < 5; i++ {
		data := []byte("request " + string(rune('0'+i)))
		_, err := clientConn.Write(data)
		require.NoError(t, err)

		buf := make([]byte, len(data))
		_, err = io.ReadFull(clientConn, buf)
		require.NoError(t, err)
		assert.Equal(t, data, buf)
	}

	clientConn.(*net.TCPConn).CloseWrite()

	<-proxyDone
	<-backendDone
}
