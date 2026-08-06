//go:build darwin && !ios

package cmd

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	vncserver "github.com/netbirdio/netbird/client/vnc/server"
)

func newAgentResources() (vncserver.ScreenCapturer, vncserver.InputInjector, error) {
	// Ask for Screen Recording here and nowhere else: this process runs as the
	// console user, which is what TCC requires for a user-scope service, and it
	// is the point where somebody is demonstrably trying to view the screen.
	// Granting it also requires the capturing process to restart, which comes
	// for free since the agent is respawned per session.
	vncserver.PrimeScreenCapturePermission()

	capturer := vncserver.NewMacPoller()
	// A Screen Recording grant only applies to a process started after it, and
	// the prompt is once per process, so an agent that never manages to capture
	// is a dead end. Exit and let the service spawn a fresh one on the next
	// connection, which prompts again and sees any grant made since.
	capturer.OnCaptureUnavailable(func() {
		log.Warn("vnc-agent exiting so the next connection can ask for Screen Recording again")
		vncAgentGiveUp()
	})
	injector, err := vncserver.NewMacInputInjector()
	if err != nil {
		return nil, nil, fmt.Errorf("macOS input injector: %w", err)
	}
	return capturer, injector, nil
}
