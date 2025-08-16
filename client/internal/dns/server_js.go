package dns

func (s *DefaultServer) initialize() (hostManager, error) {
	// Return noop host manager for WASM
	return &noopHostConfigurator{}, nil
}
