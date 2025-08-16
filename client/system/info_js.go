package system

import (
	"context"
	"runtime"

	"github.com/netbirdio/netbird/version"
)

// GetInfo retrieves system information for WASM environment
func GetInfo(ctx context.Context) *Info {
	return &Info{
		GoOS:           runtime.GOOS,
		Kernel:         runtime.GOARCH,
		Platform:       "browser",
		OS:             "browser",
		OSVersion:      "",
		Hostname:       "wasm-client",
		CPUs:           runtime.NumCPU(),
		NetbirdVersion: version.NetbirdVersion(),
		UIVersion:      "",
	}
}

func checkFileAndProcess(paths []string) ([]File, error) {
	return []File{}, nil
}

func updateStaticInfo() *StaticInfo {
	return &StaticInfo{}
}
