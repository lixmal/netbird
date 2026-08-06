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
	// the prompt is once per process, so an agent that cannot capture is a dead
	// end whether the user has just granted the permission or just taken it away.
	// Exit and let the service spawn a fresh one on the next connection, which
	// starts with the grants as they are now and can ask again.
	capturer.OnCaptureUnavailable(func() {
		log.Warn("vnc-agent exiting so the next connection sees the current Screen Recording state")
		vncAgentGiveUp()
	})
	injector, err := vncserver.NewMacInputInjector()
	if err != nil {
		return nil, nil, fmt.Errorf("macOS input injector: %w", err)
	}
	return capturer, injector, nil
}
