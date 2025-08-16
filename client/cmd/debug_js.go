//go:build js

package cmd

import "context"

// SetupDebugHandler is a no-op for WASM
func SetupDebugHandler(ctx context.Context, config interface{}, r interface{}, connectClient interface{}, filePath string) {
	// Debug handler not needed for WASM
}
