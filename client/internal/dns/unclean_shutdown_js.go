package dns

import (
	"context"
)

type ShutdownState struct{}

func CheckUncleanShutdown(ctx context.Context) (*ShutdownState, error) {
	return &ShutdownState{}, nil
}

func (s *ShutdownState) Name() string {
	return "wasm"
}

func (s *ShutdownState) Cleanup() error {
	return nil
}

func (s *ShutdownState) RestoreUncleanShutdownConfigs(context.Context) error {
	return nil
}
