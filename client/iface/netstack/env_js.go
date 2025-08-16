package netstack

import "fmt"

const EnvUseNetstackMode = "NB_USE_NETSTACK_MODE"

// IsEnabled always returns true for WASM since it's the only mode available
func IsEnabled() bool {
	return true
}

func ListenAddr() string {
	// Default SOCKS5 port for WASM
	return listenAddr(DefaultSocks5Port)
}

func listenAddr(port int) string {
	return fmt.Sprintf("0.0.0.0:%d", port)
}
